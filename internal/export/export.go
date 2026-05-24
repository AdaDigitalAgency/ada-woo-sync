package export

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/AdaDigitalAgency/ada-woo-sync/internal/config"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/progress"
)

// Result holds all generated SQL grouped by category.
type Result struct {
	SchemaOnly []string // CREATE TABLE statements for structure-only tables
	Base       []string // Full schema+data for base tables
	Users      []string // Filtered users + usermeta
	Orders     []string // Filtered HPOS, order lookups, comments
}

// structureOnlyTables are always exported as schema-only.
var structureOnlyTables = []string{
	"woocommerce_sessions",
	"actionscheduler_actions",
	"actionscheduler_claims",
	"actionscheduler_groups",
	"actionscheduler_logs",
	"mainwp_child_changes_logs",
	"mainwp_child_changes_meta",
}

// customOrderTables are filtered by order ID.
var customOrderTables = map[string]string{
	"wc_orders":                 "id",
	"wc_order_addresses":        "order_id",
	"wc_order_operational_data": "order_id",
	"wc_orders_meta":            "order_id",
	"wc_order_stats":            "order_id",
	"wc_order_product_lookup":   "order_id",
	"wc_order_tax_lookup":       "order_id",
	"wc_order_coupon_lookup":    "order_id",
	"woocommerce_order_items":   "order_id",
}

// customRuleTables are handled with special filtering logic.
var customRuleTables = map[string]bool{
	"woocommerce_order_itemmeta":  true,
	"yith_ywpar_points_log":       true,
	"comments":                    true,
	"commentmeta":                 true,
	"users":                       true,
	"usermeta":                    true,
}

// DefaultTableMode returns the built-in mode for a table name (without prefix).
func DefaultTableMode(shortName string) config.TableMode {
	for _, t := range structureOnlyTables {
		if t == shortName {
			return config.TableModeStructureOnly
		}
	}
	if _, ok := customOrderTables[shortName]; ok {
		return config.TableModeCustomRule
	}
	if customRuleTables[shortName] {
		return config.TableModeCustomRule
	}
	return config.TableModeStructureAndData
}

func Run(liveDB *sql.DB, prefix string, cfg *config.Config, log progress.Logger) (*Result, error) {
	res := &Result{}

	// 1. Determine target order IDs
	log.Detail(fmt.Sprintf("Querying target orders (%s %d)", cfg.OrderPreference, cfg.OrderCount))
	orderIDs, err := getTargetOrderIDs(liveDB, prefix, cfg.OrderCount, cfg.OrderPreference)
	if err != nil {
		return nil, fmt.Errorf("target orders: %w", err)
	}
	log.Detail(fmt.Sprintf("Found %d orders", len(orderIDs)))

	// 2. Calculate safe user IDs
	log.Detail("Resolving safe user set")
	userIDs, err := getSafeUserIDs(liveDB, prefix, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("safe users: %w", err)
	}
	log.Detail(fmt.Sprintf("Found %d users to export", len(userIDs)))

	// 3. All tables in the database
	allTables, err := listTables(liveDB)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	log.Detail(fmt.Sprintf("Found %d tables in live database", len(allTables)))

	handled := make(map[string]bool)
	tableDone := 0
	totalTables := len(allTables)

	// 4. Structure-only tables
	tableSet := make(map[string]bool, len(allTables))
	for _, t := range allTables {
		tableSet[t] = true
	}
	for _, t := range structureOnlyTables {
		full := prefix + t
		if !tableSet[full] {
			continue
		}
		log.Detail(fmt.Sprintf("Schema-only: %s", full))
		ddl, err := getCreateTable(liveDB, full)
		if err != nil {
			return nil, fmt.Errorf("schema for %s: %w", full, err)
		}
		res.SchemaOnly = append(res.SchemaOnly, ddl)
		handled[full] = true
		tableDone++
		log.Progress(tableDone, totalTables)
	}

	// 5. Custom rule: HPOS & order tables
	for t, col := range customOrderTables {
		full := prefix + t
		log.Detail(fmt.Sprintf("Orders: %s", full))
		ddl, err := getCreateTable(liveDB, full)
		if err != nil {
			return nil, fmt.Errorf("schema for %s: %w", full, err)
		}
		res.Orders = append(res.Orders, ddl)
		inserts, err := dumpFilteredData(liveDB, full, col, orderIDs)
		if err != nil {
			return nil, fmt.Errorf("data for %s: %w", full, err)
		}
		res.Orders = append(res.Orders, inserts...)
		handled[full] = true
		tableDone++
		log.Progress(tableDone, totalTables)
	}

	// 5b. woocommerce_order_itemmeta filtered by order_item_id from order_items
	log.Detail("Resolving order item IDs for itemmeta")
	orderItemIDs, err := getOrderItemIDs(liveDB, prefix, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("order item IDs: %w", err)
	}
	{
		full := prefix + "woocommerce_order_itemmeta"
		log.Detail(fmt.Sprintf("Orders: %s (%d items)", full, len(orderItemIDs)))
		ddl, err := getCreateTable(liveDB, full)
		if err != nil {
			return nil, fmt.Errorf("schema for %s: %w", full, err)
		}
		res.Orders = append(res.Orders, ddl)
		inserts, err := dumpFilteredData(liveDB, full, "order_item_id", orderItemIDs)
		if err != nil {
			return nil, fmt.Errorf("data for %s: %w", full, err)
		}
		res.Orders = append(res.Orders, inserts...)
		handled[full] = true
		tableDone++
		log.Progress(tableDone, totalTables)
	}

	// 5b. Comments filtered by order IDs
	log.Detail("Exporting comments for target orders")
	commentIDs, err := exportComments(liveDB, prefix, orderIDs, res)
	if err != nil {
		return nil, fmt.Errorf("comments: %w", err)
	}
	handled[prefix+"comments"] = true
	tableDone++
	log.Progress(tableDone, totalTables)

	// 5c. Commentmeta filtered by comment IDs
	log.Detail(fmt.Sprintf("Exporting commentmeta (%d comments)", len(commentIDs)))
	if err := exportCommentmeta(liveDB, prefix, commentIDs, res); err != nil {
		return nil, fmt.Errorf("commentmeta: %w", err)
	}
	handled[prefix+"commentmeta"] = true
	tableDone++
	log.Progress(tableDone, totalTables)

	// 6. Users & usermeta
	log.Detail(fmt.Sprintf("Exporting %d users + usermeta", len(userIDs)))
	if err := exportUsers(liveDB, prefix, userIDs, res); err != nil {
		return nil, fmt.Errorf("users: %w", err)
	}
	handled[prefix+"users"] = true
	handled[prefix+"usermeta"] = true
	tableDone += 2
	log.Progress(tableDone, totalTables)
	// 7. YITH points log (filtered by order_id OR user_id)
	yithTable := prefix + "yith_ywpar_points_log"
	if tableSet[yithTable] {
		log.Detail(fmt.Sprintf("Orders+Users: %s", yithTable))
		ddl, err := getCreateTable(liveDB, yithTable)
		if err != nil {
			return nil, fmt.Errorf("schema for %s: %w", yithTable, err)
		}
		res.Orders = append(res.Orders, ddl)
		inserts, err := dumpFilteredByOrderOrUser(liveDB, yithTable, orderIDs, userIDs)
		if err != nil {
			return nil, fmt.Errorf("data for %s: %w", yithTable, err)
		}
		res.Orders = append(res.Orders, inserts...)
		handled[yithTable] = true
		tableDone++
		log.Progress(tableDone, totalTables)
	}

	// 8. Base tables (everything else marked as Structure & Data)
	for _, t := range allTables {
		if handled[t] {
			continue
		}
		mode := getTableMode(cfg, t, prefix)
		switch mode {
		case config.TableModeIgnore:
			log.Detail(fmt.Sprintf("Skipping: %s", t))
		case config.TableModeStructureOnly:
			log.Detail(fmt.Sprintf("Schema-only: %s", t))
			ddl, err := getCreateTable(liveDB, t)
			if err != nil {
				return nil, fmt.Errorf("schema for %s: %w", t, err)
			}
			res.SchemaOnly = append(res.SchemaOnly, ddl)
		case config.TableModeStructureAndData:
			log.Detail(fmt.Sprintf("Dumping: %s", t))
			ddl, err := getCreateTable(liveDB, t)
			if err != nil {
				return nil, fmt.Errorf("schema for %s: %w", t, err)
			}
			res.Base = append(res.Base, ddl)
			inserts, err := dumpFullData(liveDB, t)
			if err != nil {
				return nil, fmt.Errorf("data for %s: %w", t, err)
			}
			res.Base = append(res.Base, inserts...)
		}
		tableDone++
		log.Progress(tableDone, totalTables)
	}

	return res, nil
}

func getTableMode(cfg *config.Config, table, prefix string) config.TableMode {
	if mode, ok := cfg.TableModes[table]; ok {
		return mode
	}
	// Strip prefix for lookup
	short := strings.TrimPrefix(table, prefix)
	if mode, ok := cfg.TableModes[short]; ok {
		return mode
	}
	return config.TableModeStructureAndData
}

func getTargetOrderIDs(db *sql.DB, prefix string, count int, pref string) ([]int64, error) {
	order := "DESC"
	if pref == "first" {
		order = "ASC"
	}
	query := fmt.Sprintf("SELECT id FROM %swc_orders ORDER BY id %s LIMIT ?", prefix, order)
	rows, err := db.Query(query, count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func getSafeUserIDs(db *sql.DB, prefix string, orderIDs []int64) ([]int64, error) {
	userSet := make(map[int64]bool)

	// Set A: all non-customer users
	query := fmt.Sprintf(`
		SELECT DISTINCT u.ID FROM %susers u
		INNER JOIN %susermeta um ON u.ID = um.user_id
		WHERE um.meta_key = '%scapabilities'
		AND um.meta_value NOT LIKE '%%\"customer\"%%'
	`, prefix, prefix, prefix)
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("non-customer users: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		userSet[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Also get users with multiple roles (one of which might be customer)
	queryMulti := fmt.Sprintf(`
		SELECT DISTINCT u.ID FROM %susers u
		INNER JOIN %susermeta um ON u.ID = um.user_id
		WHERE um.meta_key = '%scapabilities'
		AND um.meta_value LIKE '%%\"customer\"%%'
		AND um.meta_value LIKE '%%"%%"%%"%%"%%'
	`, prefix, prefix, prefix)
	rows2, err := db.Query(queryMulti)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var id int64
			if err := rows2.Scan(&id); err != nil {
				return nil, err
			}
			userSet[id] = true
		}
	}

	// Set B: customers linked to target orders
	if len(orderIDs) > 0 {
		placeholders := makeInPlaceholders(len(orderIDs))
		query := fmt.Sprintf(
			"SELECT DISTINCT customer_id FROM %swc_orders WHERE id IN (%s) AND customer_id > 0",
			prefix, placeholders,
		)
		args := int64sToArgs(orderIDs)
		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("order customers: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			userSet[id] = true
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	ids := make([]int64, 0, len(userSet))
	for id := range userSet {
		ids = append(ids, id)
	}
	return ids, nil
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

func getCreateTable(db *sql.DB, table string) (string, error) {
	var name, ddl string
	err := db.QueryRow(fmt.Sprintf("SHOW CREATE TABLE `%s`", table)).Scan(&name, &ddl)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS `%s`;\n%s;\n", table, ddl), nil
}

func dumpFilteredData(db *sql.DB, table, column string, ids []int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := makeInPlaceholders(len(ids))
	query := fmt.Sprintf("SELECT * FROM `%s` WHERE `%s` IN (%s)", table, column, placeholders)
	args := int64sToArgs(ids)
	return dumpRows(db, table, query, args)
}

func dumpFilteredByOrderOrUser(db *sql.DB, table string, orderIDs, userIDs []int64) ([]string, error) {
	if len(orderIDs) == 0 && len(userIDs) == 0 {
		return nil, nil
	}
	var conditions []string
	var args []interface{}
	if len(orderIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("`order_id` IN (%s)", makeInPlaceholders(len(orderIDs))))
		args = append(args, int64sToArgs(orderIDs)...)
	}
	if len(userIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("`user_id` IN (%s)", makeInPlaceholders(len(userIDs))))
		args = append(args, int64sToArgs(userIDs)...)
	}
	query := fmt.Sprintf("SELECT * FROM `%s` WHERE %s", table, strings.Join(conditions, " OR "))
	return dumpRows(db, table, query, args)
}

func dumpFullData(db *sql.DB, table string) ([]string, error) {
	query := fmt.Sprintf("SELECT * FROM `%s`", table)
	return dumpRows(db, table, query, nil)
}

func dumpRows(db *sql.DB, table, query string, args []interface{}) ([]string, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	const maxBatchBytes = 8 * 1024 * 1024 // 8MB per INSERT statement

	var stmts []string
	batch := make([]string, 0, 500)
	batchBytes := 0

	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}

		vals := make([]string, len(values))
		for i, v := range values {
			vals[i] = formatValue(v)
		}
		row := "(" + strings.Join(vals, ",") + ")"
		batch = append(batch, row)
		batchBytes += len(row)

		if batchBytes >= maxBatchBytes {
			stmts = append(stmts, buildInsert(table, cols, batch))
			batch = batch[:0]
			batchBytes = 0
		}
	}
	if len(batch) > 0 {
		stmts = append(stmts, buildInsert(table, cols, batch))
	}
	return stmts, rows.Err()
}

func buildInsert(table string, cols []string, rows []string) string {
	quotedCols := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = "`" + c + "`"
	}
	return fmt.Sprintf("INSERT INTO `%s` (%s) VALUES\n%s;\n",
		table, strings.Join(quotedCols, ","), strings.Join(rows, ",\n"))
}

func formatValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case []byte:
		s := string(val)
		return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\\", "\\\\"), "'", "\\'") + "'"
	case string:
		return "'" + strings.ReplaceAll(strings.ReplaceAll(val, "\\", "\\\\"), "'", "\\'") + "'"
	case int64:
		return fmt.Sprintf("%d", val)
	case float64:
		return fmt.Sprintf("%g", val)
	default:
		return fmt.Sprintf("'%v'", val)
	}
}

func exportComments(db *sql.DB, prefix string, orderIDs []int64, res *Result) ([]int64, error) {
	table := prefix + "comments"
	ddl, err := getCreateTable(db, table)
	if err != nil {
		return nil, err
	}
	res.Orders = append(res.Orders, ddl)

	if len(orderIDs) == 0 {
		return nil, nil
	}

	placeholders := makeInPlaceholders(len(orderIDs))
	query := fmt.Sprintf("SELECT comment_ID FROM `%s` WHERE comment_post_ID IN (%s)", table, placeholders)
	args := int64sToArgs(orderIDs)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commentIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		commentIDs = append(commentIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Dump the actual comment data
	inserts, err := dumpFilteredData(db, table, "comment_post_ID", orderIDs)
	if err != nil {
		return nil, err
	}
	res.Orders = append(res.Orders, inserts...)

	return commentIDs, nil
}

func exportCommentmeta(db *sql.DB, prefix string, commentIDs []int64, res *Result) error {
	table := prefix + "commentmeta"
	ddl, err := getCreateTable(db, table)
	if err != nil {
		return err
	}
	res.Orders = append(res.Orders, ddl)

	if len(commentIDs) == 0 {
		return nil
	}

	inserts, err := dumpFilteredData(db, table, "comment_id", commentIDs)
	if err != nil {
		return err
	}
	res.Orders = append(res.Orders, inserts...)
	return nil
}

func exportUsers(db *sql.DB, prefix string, userIDs []int64, res *Result) error {
	usersTable := prefix + "users"
	usermetaTable := prefix + "usermeta"

	ddl1, err := getCreateTable(db, usersTable)
	if err != nil {
		return err
	}
	res.Users = append(res.Users, ddl1)

	ddl2, err := getCreateTable(db, usermetaTable)
	if err != nil {
		return err
	}
	res.Users = append(res.Users, ddl2)

	if len(userIDs) == 0 {
		return nil
	}

	inserts, err := dumpFilteredData(db, usersTable, "ID", userIDs)
	if err != nil {
		return err
	}
	res.Users = append(res.Users, inserts...)

	inserts2, err := dumpFilteredData(db, usermetaTable, "user_id", userIDs)
	if err != nil {
		return err
	}
	res.Users = append(res.Users, inserts2...)
	return nil
}

func makeInPlaceholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

func int64sToArgs(ids []int64) []interface{} {
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

func getOrderItemIDs(db *sql.DB, prefix string, orderIDs []int64) ([]int64, error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}
	placeholders := makeInPlaceholders(len(orderIDs))
	query := fmt.Sprintf(
		"SELECT order_item_id FROM `%swoocommerce_order_items` WHERE order_id IN (%s)",
		prefix, placeholders,
	)
	args := int64sToArgs(orderIDs)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
