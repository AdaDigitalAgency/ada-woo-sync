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
)

// Import executes Step 1: drop all staging tables and import the exported SQL.
func Import(stageDB *sql.DB, exp *export.Result) error {
	if _, err := stageDB.Exec("SET FOREIGN_KEY_CHECKS=0"); err != nil {
		return fmt.Errorf("disabling FK checks: %w", err)
	}

	// Drop all existing tables
	tables, err := listTables(stageDB)
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	for _, t := range tables {
		if _, err := stageDB.Exec(fmt.Sprintf("DROP TABLE IF EXISTS `%s`", t)); err != nil {
			return fmt.Errorf("dropping %s: %w", t, err)
		}
	}

	// Execute in order: schema-only, base, users, orders
	groups := []struct {
		name string
		stmts []string
	}{
		{"schema-only", exp.SchemaOnly},
		{"base", exp.Base},
		{"users", exp.Users},
		{"orders", exp.Orders},
	}
	for _, g := range groups {
		for _, stmt := range g.stmts {
			if _, err := stageDB.Exec(stmt); err != nil {
				// Include first 200 chars of statement in error for debugging
				preview := stmt
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				return fmt.Errorf("executing %s SQL: %w\n  stmt: %s", g.name, err, preview)
			}
		}
	}

	if _, err := stageDB.Exec("SET FOREIGN_KEY_CHECKS=1"); err != nil {
		return fmt.Errorf("re-enabling FK checks: %w", err)
	}
	return nil
}

// FileSync executes Step 2: rsync wp-content and fix ownership.
func FileSync(livePath, stagePath string) error {
	src := filepath.Join(livePath, "wp-content") + "/"
	dst := filepath.Join(stagePath, "wp-content") + "/"

	cmd := exec.Command("rsync", "-av", "--delete",
		"--exclude=cache",
		"--exclude=ewww",
		"--exclude=critical-css",
		"--exclude=litespeed",
		src, dst,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync: %w", err)
	}

	// Detect owner:group from staging webroot
	uid, gid := detectOwnership(stagePath)
	chown := exec.Command("chown", "-R", fmt.Sprintf("%d:%d", uid, gid), dst)
	chown.Stdout = os.Stdout
	chown.Stderr = os.Stderr
	if err := chown.Run(); err != nil {
		return fmt.Errorf("chown: %w", err)
	}

	return nil
}

// PostProcess executes Step 3: WP-CLI search-replace and cache flush.
func PostProcess(stagePath, liveDomain, stageDomain string) error {
	commands := [][]string{
		{"wp", "search-replace",
			"https://" + liveDomain, "https://" + stageDomain,
			"--all-tables", "--path=" + stagePath},
		{"wp", "elementor", "replace-urls",
			"https://" + liveDomain, "https://" + stageDomain,
			"--path=" + stagePath},
		{"wp", "cache", "flush", "--path=" + stagePath},
	}

	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("running %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
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
