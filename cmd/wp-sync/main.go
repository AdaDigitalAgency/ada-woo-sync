package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/AdaDigitalAgency/ada-woo-sync/internal/config"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/db"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/discovery"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/export"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/guardrail"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/progress"
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

	latestVersion := selfupdate.CheckVersion(version)

	if *unattended {
		if latestVersion != "" {
			fmt.Fprintf(os.Stderr, "\033[33mUpdate available: v%s → v%s. Run 'wp-sync --update' to upgrade.\033[0m\n", version, latestVersion)
		}
		if err := runUnattended(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := tui.Run(latestVersion); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runUnattended() error {
	log := progress.NewCLILogger()

	log.Step("Loading saved configuration")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("no saved config found: %w", err)
	}
	log.Detail(fmt.Sprintf("Live: %s", cfg.LivePath))
	log.Detail(fmt.Sprintf("Stage: %s", cfg.StagePath))

	log.Step("Parsing wp-config files")
	liveWP, err := wpconfig.Parse(cfg.LivePath + "/wp-config.php")
	if err != nil {
		return fmt.Errorf("parsing live wp-config.php: %w", err)
	}
	stageWP, err := wpconfig.Parse(cfg.StagePath + "/wp-config.php")
	if err != nil {
		return fmt.Errorf("parsing stage wp-config.php: %w", err)
	}
	log.StepDone("Config parsed")

	// Safety: ensure live ≠ stage
	log.Step("Validating paths and databases")
	if err := guardrail.ValidatePaths(cfg.LivePath, cfg.StagePath); err != nil {
		return err
	}
	if err := guardrail.ValidateDBs(liveWP, stageWP); err != nil {
		return err
	}
	log.StepDone("Validation passed")

	log.Step("Connecting to databases")
	liveDB, stageDB, err := db.Connect(liveWP, stageWP)
	if err != nil {
		return fmt.Errorf("database connection: %w", err)
	}
	defer liveDB.Close()
	defer stageDB.Close()
	log.StepDone("Connected")

	// Step 0: Export
	log.Step("Exporting from live database")
	exp, err := export.Run(liveDB, liveWP.TablePrefix, cfg, log)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	log.StepDone("Export complete")

	// Step 1: Import
	log.Step("Importing to staging database")
	if err := sync.Import(stageDB, exp, log); err != nil {
		return fmt.Errorf("import: %w", err)
	}
	log.StepDone("Import complete")

	// Step 2: File sync
	log.Step("Syncing files")
	if err := sync.FileSync(cfg.LivePath, cfg.StagePath, log); err != nil {
		return fmt.Errorf("file sync: %w", err)
	}
	log.StepDone("File sync complete")

	// Step 3: Post-processing
	log.Step("Post-processing")
	domain := discovery.ExtractDomain(cfg.LivePath)
	stageDomain := discovery.ExtractDomain(cfg.StagePath)
	if err := sync.PostProcess(cfg.StagePath, domain, stageDomain, log); err != nil {
		return fmt.Errorf("post-processing: %w", err)
	}
	log.StepDone("Post-processing complete")

	log.StepDone(fmt.Sprintf("Sync completed at %s", time.Now().Format("2006-01-02 15:04:05")))
	return nil
}
