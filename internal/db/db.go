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
	// Try root via socket
	socketPaths := []string{
		"/var/run/mysqld/mysqld.sock",
		"/var/lib/mysql/mysql.sock",
		"/tmp/mysql.sock",
	}
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

	// Fall back to wp-config credentials
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?multiStatements=true",
		wp.DBUser, wp.DBPassword, wp.DBHost, wp.DBName)
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
