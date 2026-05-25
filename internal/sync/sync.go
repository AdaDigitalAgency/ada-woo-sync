package sync

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/AdaDigitalAgency/wp-stage-sync/internal/export"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/progress"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/wpcli"
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

// DefaultExcludes are the default rsync exclude patterns.
var DefaultExcludes = []string{
	"cache",
	"ewww",
	"critical-css",
	"litespeed",
	"updraft",
	"archive-master-db",
}

// FileSync executes Step 2: rsync wp-content and fix ownership.
func FileSync(livePath, stagePath string, excludes []string, log progress.Logger) error {
	src := filepath.Join(livePath, "wp-content") + "/"
	dst := filepath.Join(stagePath, "wp-content") + "/"

	log.Detail(fmt.Sprintf("rsync %s → %s", src, dst))
	args := []string{"-a", "--delete"}
	for _, ex := range excludes {
		args = append(args, "--exclude="+ex)
	}
	args = append(args, src, dst)
	cmd := exec.Command("rsync", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rsync: %w", err)
	}

	// Detect owner:group from staging webroot
	uid, gid := detectOwnership(stagePath)
	log.Detail(fmt.Sprintf("Fixing ownership to %d:%d", uid, gid))
	chown := exec.Command("chown", "-R", fmt.Sprintf("%d:%d", uid, gid), dst)
	if err := chown.Run(); err != nil {
		return fmt.Errorf("chown: %w", err)
	}

	return nil
}

// PostProcess executes Step 3: WP-CLI search-replace and cache flush.
func PostProcess(stagePath, liveDomain, stageDomain string, log progress.Logger) error {
	wpBase, err := wpcli.Resolve()
	if err != nil {
		return fmt.Errorf("wp-cli: %w", err)
	}
	wpArgs := func(args ...string) []string {
		cmd := make([]string, len(wpBase), len(wpBase)+len(args))
		copy(cmd, wpBase)
		return append(cmd, args...)
	}

	hasElementor := dirExists(filepath.Join(stagePath, "wp-content", "plugins", "elementor"))
	hasJetpack := dirExists(filepath.Join(stagePath, "wp-content", "plugins", "jetpack"))

	commands := []struct {
		label string
		args  []string
		skip  bool
	}{
		{fmt.Sprintf("Search-replace: %s → %s", liveDomain, stageDomain),
			wpArgs("search-replace",
				"https://"+liveDomain, "https://"+stageDomain,
				"--all-tables", "--allow-root", "--path="+stagePath),
			false},
		{"Elementor URL replace",
			wpArgs("elementor", "replace-urls",
				"https://"+liveDomain, "https://"+stageDomain,
				"--allow-root", "--path="+stagePath),
			!hasElementor},
		{"Jetpack safe mode",
			wpArgs("option", "update", "jetpack_safe_mode_confirmed", "1",
				"--allow-root", "--quiet", "--path="+stagePath),
			!hasJetpack},
		{"Cache flush",
			wpArgs("cache", "flush", "--allow-root", "--path="+stagePath),
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

// Anonymize masks personal customer details in the staging database.
func Anonymize(db *sql.DB, prefix string, log progress.Logger) error {
	log.Detail("Anonymizing users and usermeta")

	// 1. Update customer users in users table
	usersQuery := fmt.Sprintf(`
		UPDATE %susers u
		INNER JOIN %susermeta um ON u.ID = um.user_id AND um.meta_key = '%scapabilities'
		SET 
			u.user_email = CONCAT('customer_', u.ID, '@example.com'), 
			u.user_login = CONCAT('customer_', u.ID),
			u.user_nicename = CONCAT('customer_', u.ID),
			u.display_name = CONCAT('Customer ', u.ID)
		WHERE um.meta_value LIKE '%%"customer"%%'
	`, prefix, prefix, prefix)

	if _, err := db.Exec(usersQuery); err != nil {
		return fmt.Errorf("anonymizing users: %w", err)
	}

	// 2. Update customer usermeta billing/shipping
	metaQuery := fmt.Sprintf(`
		UPDATE %susermeta um
		INNER JOIN %susermeta uc ON um.user_id = uc.user_id AND uc.meta_key = '%scapabilities'
		SET um.meta_value = CASE 
			WHEN um.meta_key IN ('first_name', 'billing_first_name', 'shipping_first_name') THEN 'Customer'
			WHEN um.meta_key IN ('last_name', 'billing_last_name', 'shipping_last_name') THEN CONCAT('LN_', um.user_id)
			WHEN um.meta_key = 'billing_email' THEN CONCAT('customer_', um.user_id, '@example.com')
			WHEN um.meta_key = 'billing_phone' THEN '555-0000'
			WHEN um.meta_key IN ('billing_address_1', 'shipping_address_1') THEN '123 Staging Lane'
			WHEN um.meta_key IN ('billing_address_2', 'shipping_address_2') THEN ''
			WHEN um.meta_key IN ('billing_city', 'shipping_city') THEN 'Staging City'
			WHEN um.meta_key IN ('billing_postcode', 'shipping_postcode') THEN '12345'
			ELSE um.meta_value
		END
		WHERE uc.meta_value LIKE '%%"customer"%%'
		AND um.meta_key IN (
			'first_name', 'billing_first_name', 'shipping_first_name',
			'last_name', 'billing_last_name', 'shipping_last_name',
			'billing_email', 'billing_phone',
			'billing_address_1', 'shipping_address_1',
			'billing_address_2', 'shipping_address_2',
			'billing_city', 'shipping_city',
			'billing_postcode', 'shipping_postcode'
		)
	`, prefix, prefix, prefix)

	if _, err := db.Exec(metaQuery); err != nil {
		return fmt.Errorf("anonymizing usermeta: %w", err)
	}

	// 3. Update WooCommerce order addresses (HPOS) if table exists
	addressTable := prefix + "wc_order_addresses"
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", addressTable).Scan(&count)
	if err == nil && count > 0 {
		log.Detail("Anonymizing order addresses")
		addressQuery := fmt.Sprintf(`
			UPDATE %swc_order_addresses
			SET 
				first_name = 'Customer',
				last_name = CONCAT('LN_', order_id),
				company = '',
				address_1 = '123 Staging Lane',
				address_2 = '',
				city = 'Staging City',
				postcode = '12345',
				email = CONCAT('order_', order_id, '@example.com'),
				phone = '555-0000'
		`, prefix)
		if _, err := db.Exec(addressQuery); err != nil {
			return fmt.Errorf("anonymizing order addresses: %w", err)
		}
	}

	return nil
}
