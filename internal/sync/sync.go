package sync

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/AdaDigitalAgency/ada-woo-sync/internal/export"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/progress"
)

// Import executes Step 1: drop all staging tables and import the exported SQL.
func Import(stageDB *sql.DB, exp *export.Result, log progress.Logger) error {
	log.Detail("Disabling foreign key checks")
	if _, err := stageDB.Exec("SET FOREIGN_KEY_CHECKS=0"); err != nil {
		return fmt.Errorf("disabling FK checks: %w", err)
	}
	// Raise packet limit so large INSERTs (wp_posts with big post_content) don't kill the connection
	if _, err := stageDB.Exec("SET GLOBAL max_allowed_packet=268435456"); err != nil {
		// Non-fatal: may lack SUPER privilege, proceed with default
		_ = err
	}

	// Drop all existing tables
	tables, err := listTables(stageDB)
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	log.Detail(fmt.Sprintf("Dropping %d existing tables", len(tables)))
	for _, t := range tables {
		if _, err := stageDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", t)); err != nil {
			return fmt.Errorf("dropping %s: %w", t, err)
		}
	}

	// Execute in order: schema-only, base, users, orders
	groups := []struct {
		name  string
		stmts []string
	}{
		{"schema-only", exp.SchemaOnly},
		{"base", exp.Base},
		{"users", exp.Users},
		{"orders", exp.Orders},
	}
	totalStmts := 0
	for _, g := range groups {
		totalStmts += len(g.stmts)
	}
	doneStmts := 0
	for _, g := range groups {
		log.Detail(fmt.Sprintf("Importing %s (%d statements)", g.name, len(g.stmts)))
		for _, stmt := range g.stmts {
			if _, err := stageDB.Exec(stmt); err != nil {
				// Include first 200 chars of statement in error for debugging
				preview := stmt
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				return fmt.Errorf("executing %s SQL: %w\n  stmt: %s", g.name, err, preview)
			}
			doneStmts++
			log.Progress(doneStmts, totalStmts)
		}
	}

	log.Detail("Re-enabling foreign key checks")
	if _, err := stageDB.Exec("SET FOREIGN_KEY_CHECKS=1"); err != nil {
		return fmt.Errorf("re-enabling FK checks: %w", err)
	}
	return nil
}

// FileSync executes Step 2: rsync wp-content and fix ownership.
func FileSync(livePath, stagePath string, log progress.Logger) error {
	src := filepath.Join(livePath, "wp-content") + "/"
	dst := filepath.Join(stagePath, "wp-content") + "/"

	log.Detail(fmt.Sprintf("rsync %s → %s", src, dst))
	cmd := exec.Command("rsync", "-av", "--delete",
		"--exclude=cache",
		"--exclude=ewww",
		"--exclude=critical-css",
		"--exclude=litespeed",
		"--exclude=updraft",
		src, dst,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync: %w", err)
	}

	// Detect owner:group from staging webroot
	uid, gid := detectOwnership(stagePath)
	log.Detail(fmt.Sprintf("Fixing ownership to %d:%d", uid, gid))
	chown := exec.Command("chown", "-R", fmt.Sprintf("%d:%d", uid, gid), dst)
	chown.Stdout = os.Stdout
	chown.Stderr = os.Stderr
	if err := chown.Run(); err != nil {
		return fmt.Errorf("chown: %w", err)
	}

	return nil
}

// PostProcess executes Step 3: WP-CLI search-replace and cache flush.
func PostProcess(stagePath, liveDomain, stageDomain string, log progress.Logger) error {
	hasElementor := dirExists(filepath.Join(stagePath, "wp-content", "plugins", "elementor"))

	commands := []struct {
		label string
		args  []string
		skip  bool
	}{
		{fmt.Sprintf("Search-replace: %s → %s", liveDomain, stageDomain),
			[]string{"wp", "search-replace",
				"https://" + liveDomain, "https://" + stageDomain,
				"--all-tables", "--allow-root", "--path=" + stagePath},
			false},
		{"Elementor URL replace",
			[]string{"wp", "elementor", "replace-urls",
				"https://" + liveDomain, "https://" + stageDomain,
				"--allow-root", "--path=" + stagePath},
			!hasElementor},
		{"Cache flush",
			[]string{"wp", "cache", "flush", "--allow-root", "--path=" + stagePath},
			false},
	}

	for i, c := range commands {
		if c.skip {
			log.Detail(fmt.Sprintf("Skipping: %s (plugin not installed)", c.label))
			continue
		}
		log.Detail(c.label)
		log.Progress(i, len(commands))
		cmd := exec.Command(c.args[0], c.args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("running %s: %w", strings.Join(c.args, " "), err)
		}
	}
	log.Progress(len(commands), len(commands))
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func detectOwnership(path string) (uint32, uint32) {
	info, err := os.Stat(path)
	if err != nil {
		// Fallback: www-data is typically uid/gid 33
		return 33, 33
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 33, 33
	}
	return stat.Uid, stat.Gid
}

func listTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SHOW TABLES")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}
