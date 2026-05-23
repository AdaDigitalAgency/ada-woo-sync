package db

import (
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"

	"github.com/AdaDigitalAgency/ada-woo-sync/internal/wpconfig"
)

// Connect establishes connections to both live and stage databases.
// Tries root via unix socket first, falls back to wp-config credentials.
func Connect(liveWP, stageWP *wpconfig.WPConfig) (*sql.DB, *sql.DB, error) {
	liveDB, err := connect(liveWP)
	if err != nil {
		return nil, nil, fmt.Errorf("live db: %w", err)
	}
	stageDB, err := connect(stageWP)
	if err != nil {
		liveDB.Close()
		return nil, nil, fmt.Errorf("stage db: %w", err)
	}
	return liveDB, stageDB, nil
}

func connect(wp *wpconfig.WPConfig) (*sql.DB, error) {
	socketPaths := []string{
		"/var/run/mysqld/mysqld.sock",
		"/var/lib/mysql/mysql.sock",
		"/tmp/mysql.sock",
	}

	// Try wp-config credentials via unix socket first (matches how WP itself connects)
	for _, sock := range socketPaths {
		dsn := fmt.Sprintf("%s:%s@unix(%s)/%s?multiStatements=true",
			wp.DBUser, wp.DBPassword, sock, wp.DBName)
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			continue
		}
		if err := db.Ping(); err != nil {
			db.Close()
			continue
		}
		return db, nil
	}

	// Try root via socket (passwordless)
	for _, sock := range socketPaths {
		dsn := fmt.Sprintf("root@unix(%s)/%s?multiStatements=true", sock, wp.DBName)
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			continue
		}
		if err := db.Ping(); err != nil {
			db.Close()
			continue
		}
		return db, nil
	}

	// Fall back to TCP — force IPv4 when host is "localhost" to avoid ::1 issues
	host := wp.DBHost
	if host == "localhost" {
		host = "127.0.0.1"
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?multiStatements=true",
		wp.DBUser, wp.DBPassword, host, wp.DBName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening connection: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging: %w", err)
	}
	return db, nil
}
