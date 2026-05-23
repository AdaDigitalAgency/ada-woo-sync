package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/AdaDigitalAgency/ada-woo-sync/internal/config"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/db"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/discovery"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/export"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/guardrail"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/selfupdate"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/sync"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/tui"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/wpconfig"
)

// Set via -ldflags at build time
var version = "dev"

func main() {
	unattended := flag.Bool("u", false, "Run in unattended mode using saved config")
	flag.BoolVar(unattended, "unattended", false, "Run in unattended mode using saved config")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "v", false, "Print version and exit")
	doUpdate := flag.Bool("update", false, "Update to the latest version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("wp-sync %s\n", version)
		return
	}

	if *doUpdate {
		if err := selfupdate.Update(version); err != nil {
			fmt.Fprintf(os.Stderr, "Update failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *unattended {
		if err := runUnattended(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := tui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runUnattended() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("no saved config found: %w", err)
	}

	liveWP, err := wpconfig.Parse(cfg.LivePath + "/wp-config.php")
	if err != nil {
		return fmt.Errorf("parsing live wp-config.php: %w", err)
	}
	stageWP, err := wpconfig.Parse(cfg.StagePath + "/wp-config.php")
	if err != nil {
		return fmt.Errorf("parsing stage wp-config.php: %w", err)
	}

	// Safety: ensure live ≠ stage
	if err := guardrail.ValidatePaths(cfg.LivePath, cfg.StagePath); err != nil {
		return err
	}
	if err := guardrail.ValidateDBs(liveWP, stageWP); err != nil {
		return err
	}

	liveDB, stageDB, err := db.Connect(liveWP, stageWP)
	if err != nil {
		return fmt.Errorf("database connection: %w", err)
	}
	defer liveDB.Close()
	defer stageDB.Close()

	// Step 0: Export
	exp, err := export.Run(liveDB, liveWP.TablePrefix, cfg)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}

	// Step 1: Import
	if err := sync.Import(stageDB, exp); err != nil {
		return fmt.Errorf("import: %w", err)
	}

	// Step 2: File sync
	if err := sync.FileSync(cfg.LivePath, cfg.StagePath); err != nil {
		return fmt.Errorf("file sync: %w", err)
	}

	// Step 3: Post-processing
	domain := discovery.ExtractDomain(cfg.LivePath)
	stageDomain := discovery.ExtractDomain(cfg.StagePath)
	if err := sync.PostProcess(cfg.StagePath, domain, stageDomain); err != nil {
		return fmt.Errorf("post-processing: %w", err)
	}

	fmt.Println("Sync complete.")
	return nil
}
