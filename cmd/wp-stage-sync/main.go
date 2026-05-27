package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
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
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "WP Stage Sync - Safely synchronize WordPress staging to live environments.\n\n")
		fmt.Fprintf(os.Stderr, "Usage: wp-stage-sync [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fmt.Fprintf(os.Stderr, "  -h, --help       Print this help message\n")
		fmt.Fprintf(os.Stderr, "  -v, --version    Print version and exit\n")
		fmt.Fprintf(os.Stderr, "  --update         Update to the latest version\n")
		fmt.Fprintf(os.Stderr, "  -u, --unattended Run in unattended mode using saved config\n")
		fmt.Fprintf(os.Stderr, "  --promote        Enter promote mode (stage → live)\n")
		fmt.Fprintf(os.Stderr, "  --restore        Enter restore mode\n")
		fmt.Fprintf(os.Stderr, "  -s, --site <id>  Target a specific site by identifier\n")
		fmt.Fprintf(os.Stderr, "  -l, --list       List all configured sites\n")
		fmt.Fprintf(os.Stderr, "  --delete         Delete a site config (requires --site)\n")
		fmt.Fprintf(os.Stderr, "  --reset          Remove all saved site configs\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  wp-stage-sync                        # Launch the TUI\n")
		fmt.Fprintf(os.Stderr, "  wp-stage-sync -l                     # List all configured sites\n")
		fmt.Fprintf(os.Stderr, "  wp-stage-sync -u -s my-site          # Sync 'my-site' unattended\n")
		fmt.Fprintf(os.Stderr, "  wp-stage-sync --promote -s my-site   # Promote 'my-site' in unattended mode\n")
		fmt.Fprintf(os.Stderr, "  wp-stage-sync --delete -s my-site    # Delete 'my-site' configuration\n")
		fmt.Fprintf(os.Stderr, "  wp-stage-sync --reset                # Wipe all configurations\n")
	}

	unattended := flag.Bool("u", false, "Run in unattended mode using saved config")
	flag.BoolVar(unattended, "unattended", false, "Run in unattended mode using saved config")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.BoolVar(showVersion, "v", false, "Print version and exit")
	doUpdate := flag.Bool("update", false, "Update to the latest version")
	siteFlag := flag.String("site", "", "Site identifier (use --list to see configured sites)")
	flag.StringVar(siteFlag, "s", "", "Site identifier (use --list to see configured sites)")
	promoteFlag := flag.Bool("promote", false, "Enter promote mode (stage → live)")
	restoreFlag := flag.Bool("restore", false, "Enter restore mode")
	listFlag := flag.Bool("list", false, "List all configured sites")
	flag.BoolVar(listFlag, "l", false, "List all configured sites")
	deleteFlag := flag.Bool("delete", false, "Delete a site config (requires --site)")
	resetFlag := flag.Bool("reset", false, "Remove all saved site configs")
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

	// Migrate legacy flat config files on every startup
	config.MigrateLegacyConfigs()

	if *listFlag {
		sites := config.ListSites()
		if len(sites) == 0 {
			fmt.Println("No sites configured. Run wp-stage-sync interactively to set one up.")
			return
		}
		for _, s := range sites {
			stageDomain := config.DomainFromPath(s.Config.StagePath)
			fmt.Printf("%s  →  %s\n", s.Domain, stageDomain)
		}
		return
	}

	if *deleteFlag {
		if *siteFlag == "" {
			fmt.Fprintf(os.Stderr, "The --delete flag requires --site to specify which site to remove.\n")
			os.Exit(1)
		}
		if !confirm(fmt.Sprintf("Delete all config and backups for %q?", *siteFlag)) {
			fmt.Println("Cancelled.")
			return
		}
		if err := config.DeleteSite(*siteFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Deleted site config for %q.\n", *siteFlag)
		return
	}

	if *resetFlag {
		sites := config.ListSites()
		if len(sites) == 0 {
			fmt.Println("No sites configured. Nothing to reset.")
			return
		}
		fmt.Printf("This will delete %d site config(s) and all backups:\n", len(sites))
		for _, s := range sites {
			fmt.Printf("  %s\n", s.Domain)
		}
		if !confirm("Are you sure?") {
			fmt.Println("Cancelled.")
			return
		}
		if err := config.ResetAllSites(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("All site configs removed.")
		return
	}

	latestVersion := selfupdate.CheckVersion(version)

	// Validate flag combinations
	if *promoteFlag && *unattended {
		fmt.Fprintf(os.Stderr, "Promote mode is not available in unattended mode.\n")
		os.Exit(1)
	}
	if *restoreFlag && *unattended {
		fmt.Fprintf(os.Stderr, "Restore mode is not available in unattended mode.\n")
		os.Exit(1)
	}
	if *restoreFlag && *siteFlag != "" {
		fmt.Fprintf(os.Stderr, "Restore mode does not support --site. Use the interactive TUI.\n")
		os.Exit(1)
	}
	if *siteFlag != "" && !*unattended && !*promoteFlag {
		fmt.Fprintf(os.Stderr, "The --site flag requires unattended mode (-u) or promote mode (--promote).\n")
		os.Exit(1)
	}

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

	// Resolve config for promote/restore modes
	if *promoteFlag || *restoreFlag {
		cfg, err := resolveConfig(*siteFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		mode := tui.ModePromote
		if *restoreFlag {
			mode = tui.ModeRestore
		}
		if err := tui.RunWithMode(version, latestVersion, mode, cfg); err != nil {
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

func resolveConfig(siteDomain string) (*config.Config, error) {
	sites := config.ListSites()

	switch {
	case siteDomain != "":
		cfg, err := config.Load(siteDomain)
		if err != nil {
			return nil, fmt.Errorf("no saved config for site %q: %w", siteDomain, err)
		}
		return cfg, nil
	case len(sites) == 1:
		return sites[0].Config, nil
	case len(sites) > 1:
		fmt.Fprintf(os.Stderr, "Multiple sites configured. Use --site to specify which one:\n")
		for _, s := range sites {
			fmt.Fprintf(os.Stderr, "  --site %s\n", s.Domain)
		}
		return nil, fmt.Errorf("--site flag required when multiple sites are configured")
	default:
		return nil, fmt.Errorf("no saved config found — run wp-stage-sync interactively first")
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

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}
