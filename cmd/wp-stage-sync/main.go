package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/AdaDigitalAgency/wp-stage-sync/internal/config"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/db"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/discovery"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/export"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/guardrail"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/progress"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/selfupdate"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/sync"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/tui"
	"github.com/AdaDigitalAgency/wp-stage-sync/internal/wpconfig"
)

// Set via -ldflags at build time
var version = "dev"

func main() {
	unattended := flag.Bool("u", false, "Run in unattended mode using saved config")
	flag.BoolVar(unattended, "unattended", false, "Run in unattended mode using saved config")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "v", false, "Print version and exit")
	doUpdate := flag.Bool("update", false, "Update to the latest version")
	siteFlag := flag.String("site", "", "Site domain to sync (for multi-site servers)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("wp-stage-sync %s\n", version)
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
			fmt.Fprintf(os.Stderr, "\033[33mUpdate available: v%s → v%s. Run 'wp-stage-sync --update' to upgrade.\033[0m\n", version, latestVersion)
		}
		if err := runUnattended(*siteFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := tui.Run(version, latestVersion); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runUnattended(siteDomain string) error {
	log := progress.NewCLILogger()
	started := time.Now()

	// Resolve which site config to use
	sites := config.ListSites()
	var cfg *config.Config

	switch {
	case siteDomain != "":
		var err error
		cfg, err = config.Load(siteDomain)
		if err != nil {
			return fmt.Errorf("no saved config for site %q: %w", siteDomain, err)
		}
	case len(sites) == 1:
		cfg = sites[0].Config
	case len(sites) > 1:
		fmt.Fprintf(os.Stderr, "Multiple sites configured. Use --site to specify which one:\n")
		for _, s := range sites {
			fmt.Fprintf(os.Stderr, "  --site %s\n", s.Domain)
		}
		return fmt.Errorf("--site flag required when multiple sites are configured")
	default:
		return fmt.Errorf("no saved config found — run wp-stage-sync interactively first")
	}

	log.Detail(fmt.Sprintf("Site: %s", config.DomainFromPath(cfg.LivePath)))
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
	excludes := cfg.RsyncExcludes
	if len(excludes) == 0 {
		excludes = sync.DefaultExcludes
	}
	if err := sync.FileSync(cfg.LivePath, cfg.StagePath, excludes, log); err != nil {
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

	duration := time.Since(started).Truncate(time.Second)
	log.StepDone(fmt.Sprintf("Sync completed at %s and it took %s", time.Now().Format("2006-01-02 15:04:05"), duration))
	return nil
}
