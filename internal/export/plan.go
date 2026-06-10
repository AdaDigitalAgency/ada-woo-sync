package export

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/AdaDigitalAgency/wp-stage-sync/internal/config"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/progress"
)

// TablePlan describes how a single table would be exported during a dry-run.
type TablePlan struct {
	Name   string           // full table name (with prefix)
	Mode   config.TableMode // effective export mode
	Filter string           // human-readable filter description, "" for full data
	Rows   int64            // rows that would be exported (0 for schema-only/ignored)
}

// Plan summarizes what an export would produce, without dumping any data.
// It mirrors the classification logic in Run but issues only read-only
// COUNT queries — keep the two in sync when table handling changes.
type Plan struct {
	OrderPreference string
	OrderCount      int
	TargetOrders    int
	SafeUsers       int
	TotalTables     int
	Tables          []TablePlan
}

// ExportedRows returns the total number of rows that would be written.
func (p *Plan) ExportedRows() int64 {
	var total int64
	for _, t := range p.Tables {
		total += t.Rows
	}
	return total
}

// BuildPlan performs the same discovery and table classification as Run but,
// instead of building INSERT statements, counts the rows each table would
// contribute. It issues no writes.
func BuildPlan(liveDB *sql.DB, prefix string, cfg *config.Config, log progress.Logger) (*Plan, error) {
	p := &Plan{OrderPreference: cfg.OrderPreference, OrderCount: cfg.OrderCount}

	log.Detail(fmt.Sprintf("Querying target orders (%s %d)", cfg.OrderPreference, cfg.OrderCount))
	orderIDs, err := getTargetOrderIDs(liveDB, prefix, cfg.OrderCount, cfg.OrderPreference)
	if err != nil {
		return nil, fmt.Errorf("target orders: %w", err)
	}
	p.TargetOrders = len(orderIDs)
	log.Detail(fmt.Sprintf("Found %d orders", len(orderIDs)))

	log.Detail("Resolving safe user set")
	userIDs, err := getSafeUserIDs(liveDB, prefix, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("safe users: %w", err)
	}
	p.SafeUsers = len(userIDs)
	log.Detail(fmt.Sprintf("Found %d users to export", len(userIDs)))

	allTables, err := listTables(liveDB)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	p.TotalTables = len(allTables)
	log.Detail(fmt.Sprintf("Found %d tables in live database", len(allTables)))

	tableSet := make(map[string]bool, len(allTables))
	for _, t := range allTables {
		tableSet[t] = true
	}
	handled := make(map[string]bool)

	add := func(name, filter string, mode config.TableMode, rows int64) {
		p.Tables = append(p.Tables, TablePlan{Name: name, Mode: mode, Filter: filter, Rows: rows})
		handled[name] = true
	}

	// Structure-only tables.
	for _, t := range structureOnlyTables {
		full := prefix + t
		if !tableSet[full] {
			continue
		}
		add(full, "schema only (no rows)", config.TableModeStructureOnly, 0)
	}

	// HPOS & order tables, filtered by order ID.
	for t, col := range customOrderTables {
		full := prefix + t
		if !tableSet[full] {
			continue
		}
		n, err := countFiltered(liveDB, full, col, orderIDs)
		if err != nil {
			return nil, fmt.Errorf("counting %s: %w", full, err)
		}
		add(full, fmt.Sprintf("%s IN (%d target orders)", col, len(orderIDs)), config.TableModeCustomRule, n)
	}

	// woocommerce_order_itemmeta, filtered by order_item_id.
	if itemmeta := prefix + "woocommerce_order_itemmeta"; tableSet[itemmeta] {
		orderItemIDs, err := getOrderItemIDs(liveDB, prefix, orderIDs)
		if err != nil {
			return nil, fmt.Errorf("order item IDs: %w", err)
		}
		n, err := countFiltered(liveDB, itemmeta, "order_item_id", orderItemIDs)
		if err != nil {
			return nil, fmt.Errorf("counting %s: %w", itemmeta, err)
		}
		add(itemmeta, fmt.Sprintf("order_item_id IN (%d items)", len(orderItemIDs)), config.TableModeCustomRule, n)
	}

	// Comments, filtered by order (post) ID.
	commentIDs, err := getCommentIDs(liveDB, prefix, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("comment IDs: %w", err)
	}
	commentsTable := prefix + "comments"
	commentsN, err := countFiltered(liveDB, commentsTable, "comment_post_ID", orderIDs)
	if err != nil {
		return nil, fmt.Errorf("counting %s: %w", commentsTable, err)
	}
	add(commentsTable, fmt.Sprintf("comment_post_ID IN (%d target orders)", len(orderIDs)), config.TableModeCustomRule, commentsN)

	// Commentmeta, filtered by comment ID.
	commentmetaTable := prefix + "commentmeta"
	commentmetaN, err := countFiltered(liveDB, commentmetaTable, "comment_id", commentIDs)
	if err != nil {
		return nil, fmt.Errorf("counting %s: %w", commentmetaTable, err)
	}
	add(commentmetaTable, fmt.Sprintf("comment_id IN (%d comments)", len(commentIDs)), config.TableModeCustomRule, commentmetaN)

	// Users & usermeta, filtered by the safe user set.
	usersTable := prefix + "users"
	usersN, err := countFiltered(liveDB, usersTable, "ID", userIDs)
	if err != nil {
		return nil, fmt.Errorf("counting %s: %w", usersTable, err)
	}
	add(usersTable, fmt.Sprintf("ID IN (%d safe users)", len(userIDs)), config.TableModeCustomRule, usersN)

	usermetaTable := prefix + "usermeta"
	usermetaN, err := countFiltered(liveDB, usermetaTable, "user_id", userIDs)
	if err != nil {
		return nil, fmt.Errorf("counting %s: %w", usermetaTable, err)
	}
	add(usermetaTable, fmt.Sprintf("user_id IN (%d safe users)", len(userIDs)), config.TableModeCustomRule, usermetaN)

	// YITH points log, filtered by order OR user.
	if yith := prefix + "yith_ywpar_points_log"; tableSet[yith] {
		n, err := countYith(liveDB, yith, orderIDs, userIDs)
		if err != nil {
			return nil, fmt.Errorf("counting %s: %w", yith, err)
		}
		add(yith, fmt.Sprintf("order_id (%d orders) OR user_id (%d users)", len(orderIDs), len(userIDs)), config.TableModeCustomRule, n)
	}

	// Base tables — everything else, per configured mode.
	for _, t := range allTables {
		if handled[t] {
			continue
		}
		mode := getTableMode(cfg, t, prefix)
		switch mode {
		case config.TableModeIgnore:
			add(t, "ignored (skipped)", mode, 0)
		case config.TableModeStructureOnly:
			add(t, "schema only (no rows)", mode, 0)
		case config.TableModeStructureAndData:
			n, err := countAll(liveDB, t)
			if err != nil {
				return nil, fmt.Errorf("counting %s: %w", t, err)
			}
			add(t, "", mode, n)
		}
	}

	return p, nil
}

func countFiltered(db *sql.DB, table, column string, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE `%s` IN (%s)", table, column, makeInPlaceholders(len(ids)))
	var n int64
	if err := db.QueryRow(query, int64sToArgs(ids)...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func countAll(db *sql.DB, table string) (int64, error) {
	var n int64
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM `%s`", table)).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func countYith(db *sql.DB, table string, orderIDs, userIDs []int64) (int64, error) {
	if len(orderIDs) == 0 && len(userIDs) == 0 {
		return 0, nil
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
	query := fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE %s", table, strings.Join(conditions, " OR "))
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func getCommentIDs(db *sql.DB, prefix string, orderIDs []int64) ([]int64, error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(
		"SELECT comment_ID FROM `%scomments` WHERE comment_post_ID IN (%s)",
		prefix, makeInPlaceholders(len(orderIDs)),
	)
	rows, err := db.Query(query, int64sToArgs(orderIDs)...)
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
