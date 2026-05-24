package tui

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AdaDigitalAgency/ada-woo-sync/internal/config"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/db"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/discovery"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/export"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/guardrail"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/progress"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/sync"
	"github.com/AdaDigitalAgency/ada-woo-sync/internal/wpconfig"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type step int

const (
	stepStartup step = iota
	stepPaths
	stepCredentials
	stepSyncParams
	stepTableSelect
	stepConfirm
	stepRunning
	stepDone
)

type pathMode int

const (
	pathModePairs  pathMode = iota // selecting from auto-detected pairs
	pathModeManual                 // typing paths manually
)

type model struct {
	step       step
	cfg        *config.Config
	savedCfg   *config.Config // non-nil if config file exists
	discovered []string
	sitePairs  []discovery.SitePair
	pathMode   pathMode
	liveWP     *wpconfig.WPConfig
	stageWP    *wpconfig.WPConfig
	allTables  []string
	cursor     int
	input      string
	err        error
	status     string
	width      int
	height     int

	// Progress tracking for stepRunning
	completedSteps  []string // steps that finished (shown with ✓)
	currentStep     string   // step currently in progress
	currentDetail   string   // detail message under current step
	progressCurrent int
	progressTotal   int
	spinnerFrame    int
	updateAvailable string // latest version, empty if up to date
}

type syncDoneMsg struct{ err error }
type stepMsg struct{ name string }
type detailMsg struct{ msg string }
type progressMsg struct{ current, total int }
type stepDoneMsg struct{ msg string }
type spinTickMsg struct{}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).MarginBottom(1)
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	updateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).MarginTop(1)
)

var activeProgram *tea.Program
var syncCompletedAt time.Time

func Run(latestVersion string) error {
	m := initialModel()
	m.updateAvailable = latestVersion
	p := tea.NewProgram(m, tea.WithAltScreen())
	activeProgram = p
	_, err := p.Run()
	if err == nil && !syncCompletedAt.IsZero() {
		fmt.Printf("Sync completed at %s\n", syncCompletedAt.Format("2006-01-02 15:04:05"))
	}
	return err
}

func initialModel() model {
	saved, _ := config.Load()
	discovered := discovery.Scan()
	pairs := discovery.PairSites(discovered)

	m := model{
		discovered: discovered,
		sitePairs:  pairs,
		savedCfg:   saved,
		cfg: &config.Config{
			OrderCount:      100,
			OrderPreference: "last",
			TableModes:      make(map[string]config.TableMode),
		},
	}

	if saved != nil {
		m.step = stepStartup
	} else {
		m.step = stepPaths
	}

	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case syncDoneMsg:
		m.step = stepDone
		m.err = msg.err
		if msg.err == nil {
			syncCompletedAt = time.Now()
		}
		return m, nil

	case stepMsg:
		m.currentStep = msg.name
		m.currentDetail = ""
		m.progressCurrent = 0
		m.progressTotal = 0
		return m, nil

	case detailMsg:
		m.currentDetail = msg.msg
		return m, nil

	case progressMsg:
		m.progressCurrent = msg.current
		m.progressTotal = msg.total
		return m, nil

	case stepDoneMsg:
		m.completedSteps = append(m.completedSteps, msg.msg)
		m.currentStep = ""
		m.currentDetail = ""
		m.progressCurrent = 0
		m.progressTotal = 0
		return m, nil

	case spinTickMsg:
		if m.step == stepRunning {
			m.spinnerFrame++
			return m, tickSpinner()
		}
		return m, nil

	case tea.KeyMsg:
		// Global quit
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}

		m.err = nil

		switch m.step {
		case stepStartup:
			return m.updateStartup(msg)
		case stepPaths:
			return m.updatePaths(msg)
		case stepCredentials:
			return m.updateCredentials(msg)
		case stepSyncParams:
			return m.updateSyncParams(msg)
		case stepTableSelect:
			return m.updateTableSelect(msg)
		case stepConfirm:
			return m.updateConfirm(msg)
		case stepDone:
			if msg.Type == tea.KeyEnter || msg.String() == "q" {
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("🔄 WP Staging Sync") + "\n\n")

	switch m.step {
	case stepStartup:
		b.WriteString(promptStyle.Render("Saved configuration found.") + "\n\n")
		options := []string{"Use last settings", "Start over"}
		for i, opt := range options {
			if i == m.cursor {
				b.WriteString(selectedStyle.Render("▸ " + opt) + "\n")
			} else {
				b.WriteString("  " + opt + "\n")
			}
		}

	case stepPaths:
		if m.pathMode == pathModePairs && len(m.sitePairs) > 0 {
			b.WriteString(promptStyle.Render("Select a site to sync:") + "\n\n")
			for i, pair := range m.sitePairs {
				marker := "  "
				if i == m.cursor {
					marker = selectedStyle.Render("▸ ")
				}
				if pair.StagePath != "" {
					label := fmt.Sprintf("%s  →  %s", pair.Domain, "stage."+pair.Domain)
					if i == m.cursor {
						b.WriteString(marker + selectedStyle.Render(label) + "\n")
					} else {
						b.WriteString(marker + label + "\n")
					}
				} else {
					label := pair.Domain + dimStyle.Render("  (no staging found)")
					if i == m.cursor {
						b.WriteString(marker + selectedStyle.Render(pair.Domain) + dimStyle.Render("  (no staging found)") + "\n")
					} else {
						b.WriteString(marker + label + "\n")
					}
				}
			}
			// Custom paths option
			marker := "  "
			if m.cursor == len(m.sitePairs) {
				marker = selectedStyle.Render("▸ ")
				b.WriteString(marker + selectedStyle.Render("Custom paths...") + "\n")
			} else {
				b.WriteString(marker + dimStyle.Render("Custom paths...") + "\n")
			}
		} else {
			// Manual input mode
			b.WriteString(promptStyle.Render("Enter paths:") + "\n\n")
			if m.cursor == 0 {
				b.WriteString(selectedStyle.Render("▸ Live Webroot: ") + m.input + "█\n")
				b.WriteString("  Stage Webroot: " + m.cfg.StagePath + "\n")
			} else {
				b.WriteString("  Live Webroot: " + m.cfg.LivePath + "\n")
				b.WriteString(selectedStyle.Render("▸ Stage Webroot: ") + m.input + "█\n")
			}
			b.WriteString("\n" + dimStyle.Render("Esc to go back to site list"))
		}

	case stepCredentials:
		b.WriteString(promptStyle.Render("Extracted credentials:") + "\n\n")
		b.WriteString(fmt.Sprintf("  Live DB: %s @ %s (prefix: %s)\n", m.liveWP.DBName, m.liveWP.DBHost, m.liveWP.TablePrefix))
		b.WriteString(fmt.Sprintf("  Stage DB: %s @ %s (prefix: %s)\n", m.stageWP.DBName, m.stageWP.DBHost, m.stageWP.TablePrefix))
		b.WriteString("\n" + dimStyle.Render("Press Enter to continue"))

	case stepSyncParams:
		b.WriteString(promptStyle.Render("Sync parameters:") + "\n\n")
		if m.cursor == 0 {
			b.WriteString(selectedStyle.Render("▸ Order Count: ") + m.input + "█\n")
			pref := "Last N"
			if m.cfg.OrderPreference == "first" {
				pref = "First N"
			}
			b.WriteString("  Order Preference: " + pref + "\n")
		} else {
			b.WriteString(fmt.Sprintf("  Order Count: %d\n", m.cfg.OrderCount))
			options := []string{"Last N", "First N"}
			for i, opt := range options {
				marker := "  "
				if (i == 0 && m.cfg.OrderPreference == "last") || (i == 1 && m.cfg.OrderPreference == "first") {
					marker = "● "
				}
				b.WriteString("  " + marker + opt + "\n")
			}
		}

	case stepTableSelect:
		b.WriteString(promptStyle.Render("Table sync modes:") + "\n")
		b.WriteString(dimStyle.Render("↑/↓ navigate, Space to cycle mode, Enter to confirm") + "\n\n")

		// Show visible window of tables
		windowSize := m.height - 10
		if windowSize < 5 {
			windowSize = 5
		}
		start := 0
		if m.cursor >= windowSize {
			start = m.cursor - windowSize + 1
		}
		end := start + windowSize
		if end > len(m.allTables) {
			end = len(m.allTables)
		}

		for i := start; i < end; i++ {
			t := m.allTables[i]
			mode := getDisplayMode(m.cfg, t)
			prefix := "  "
			style := lipgloss.NewStyle()
			if i == m.cursor {
				prefix = "▸ "
				style = selectedStyle
			}
			b.WriteString(style.Render(fmt.Sprintf("%s%-50s [%s]", prefix, t, mode)) + "\n")
		}

	case stepConfirm:
		b.WriteString(promptStyle.Render("Ready to sync?") + "\n\n")
		b.WriteString(fmt.Sprintf("  Live:  %s\n", m.cfg.LivePath))
		b.WriteString(fmt.Sprintf("  Stage: %s\n", m.cfg.StagePath))
		b.WriteString(fmt.Sprintf("  Orders: %d (%s)\n", m.cfg.OrderCount, m.cfg.OrderPreference))
		b.WriteString("\n" + dimStyle.Render("Press Enter to start, Esc to go back"))

	case stepRunning:
		b.WriteString(promptStyle.Render("Syncing...") + "\n\n")

		// Completed steps
		for _, s := range m.completedSteps {
			b.WriteString(successStyle.Render("  ✓ "+s) + "\n")
		}

		// Current step with spinner
		if m.currentStep != "" {
			spinChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			spin := spinChars[m.spinnerFrame%len(spinChars)]
			b.WriteString(selectedStyle.Render("  "+spin+" "+m.currentStep) + "\n")

			// Detail line
			if m.currentDetail != "" {
				b.WriteString(dimStyle.Render("    "+m.currentDetail) + "\n")
			}

			// Progress bar
			if m.progressTotal > 0 {
				barWidth := 30
				filled := barWidth * m.progressCurrent / m.progressTotal
				if filled > barWidth {
					filled = barWidth
				}
				bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
				pct := 100 * m.progressCurrent / m.progressTotal
				b.WriteString(dimStyle.Render(fmt.Sprintf("    [%s] %d/%d (%d%%)", bar, m.progressCurrent, m.progressTotal, pct)) + "\n")
			}
		}

	case stepDone:
		// Show completed steps as a summary
		for _, s := range m.completedSteps {
			b.WriteString(successStyle.Render("  ✓ "+s) + "\n")
		}
		b.WriteString("\n")
		if m.err != nil {
			b.WriteString(errorStyle.Render("✗ Sync failed: " + m.err.Error()) + "\n")
		} else {
			b.WriteString(successStyle.Render("✓ Sync complete!") + "\n")
		}
		b.WriteString("\n" + dimStyle.Render("Press Enter or q to exit"))
	}

	if m.err != nil && m.step != stepDone {
		b.WriteString("\n" + errorStyle.Render("Error: "+m.err.Error()))
	}

	if m.updateAvailable != "" {
		b.WriteString("\n" + updateStyle.Render(fmt.Sprintf("Update available: v%s → run 'wp-sync --update'", m.updateAvailable)))
	}

	return b.String()
}

// --- Step handlers ---

func (m model) updateStartup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < 1 {
			m.cursor++
		}
	case tea.KeyEnter:
		if m.cursor == 0 {
			// Use saved config
			m.cfg = m.savedCfg
			m.step = stepCredentials
			m.cursor = 0
			// Parse wp-configs
			var err error
			m.liveWP, err = wpconfig.Parse(m.cfg.LivePath + "/wp-config.php")
			if err != nil {
				m.err = err
				return m, nil
			}
			m.stageWP, err = wpconfig.Parse(m.cfg.StagePath + "/wp-config.php")
			if err != nil {
				m.err = err
				return m, nil
			}
		} else {
			m.step = stepPaths
			m.cursor = 0
			m.input = ""
		}
	}
	return m, nil
}

func (m model) updatePaths(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pathMode == pathModePairs && len(m.sitePairs) > 0 {
		return m.updatePathsPairs(msg)
	}
	return m.updatePathsManual(msg)
}

func (m model) updatePathsPairs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxIdx := len(m.sitePairs) // includes "Custom paths" option
	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < maxIdx {
			m.cursor++
		}
	case tea.KeyEnter:
		if m.cursor == len(m.sitePairs) {
			// Custom paths
			m.pathMode = pathModeManual
			m.cursor = 0
			m.input = ""
			return m, nil
		}
		pair := m.sitePairs[m.cursor]
		if pair.StagePath == "" {
			m.err = fmt.Errorf("no staging site found for %s — use Custom paths", pair.Domain)
			return m, nil
		}
		m.cfg.LivePath = pair.LivePath
		m.cfg.StagePath = pair.StagePath
		var err error
		m.liveWP, err = wpconfig.Parse(m.cfg.LivePath + "/wp-config.php")
		if err != nil {
			m.err = fmt.Errorf("live wp-config: %w", err)
			return m, nil
		}
		m.stageWP, err = wpconfig.Parse(m.cfg.StagePath + "/wp-config.php")
		if err != nil {
			m.err = fmt.Errorf("stage wp-config: %w", err)
			return m, nil
		}
		m.step = stepCredentials
		m.cursor = 0
	}
	return m, nil
}

func (m model) updatePathsManual(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if len(m.sitePairs) > 0 {
			m.pathMode = pathModePairs
			m.cursor = 0
			m.input = ""
		}
	case tea.KeyTab:
		if m.cursor == 0 {
			m.cfg.LivePath = m.input
			m.cursor = 1
			m.input = m.cfg.StagePath
		} else {
			m.cfg.StagePath = m.input
			m.cursor = 0
			m.input = m.cfg.LivePath
		}
	case tea.KeyEnter:
		if m.cursor == 0 {
			m.cfg.LivePath = m.input
			m.cursor = 1
			m.input = m.cfg.StagePath
		} else {
			m.cfg.StagePath = m.input
			var err error
			m.liveWP, err = wpconfig.Parse(m.cfg.LivePath + "/wp-config.php")
			if err != nil {
				m.err = fmt.Errorf("live wp-config: %w", err)
				return m, nil
			}
			m.stageWP, err = wpconfig.Parse(m.cfg.StagePath + "/wp-config.php")
			if err != nil {
				m.err = fmt.Errorf("stage wp-config: %w", err)
				return m, nil
			}
			m.step = stepCredentials
			m.cursor = 0
		}
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
		}
	}
	return m, nil
}

func (m model) updateCredentials(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEnter {
		m.step = stepSyncParams
		m.cursor = 0
		m.input = fmt.Sprintf("%d", m.cfg.OrderCount)
	}
	return m, nil
}

func (m model) updateSyncParams(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab:
		if m.cursor == 0 {
			n, err := strconv.Atoi(m.input)
			if err != nil || n <= 0 {
				m.err = fmt.Errorf("invalid order count")
				return m, nil
			}
			m.cfg.OrderCount = n
			m.cursor = 1
		} else {
			m.cursor = 0
			m.input = fmt.Sprintf("%d", m.cfg.OrderCount)
		}
	case tea.KeyEnter:
		if m.cursor == 0 {
			n, err := strconv.Atoi(m.input)
			if err != nil || n <= 0 {
				m.err = fmt.Errorf("invalid order count")
				return m, nil
			}
			m.cfg.OrderCount = n
			m.cursor = 1
		} else {
			// Move to table selection — need DB connection
			m.step = stepTableSelect
			m.cursor = 0
			liveDB, _, err := db.Connect(m.liveWP, m.stageWP)
			if err != nil {
				m.err = fmt.Errorf("connecting to DB: %w", err)
				m.step = stepSyncParams
				return m, nil
			}
			tables, err := queryTables(liveDB)
			liveDB.Close()
			if err != nil {
				m.err = fmt.Errorf("listing tables: %w", err)
				m.step = stepSyncParams
				return m, nil
			}
			m.allTables = tables
			applyDefaultModes(m.cfg, tables, m.liveWP.TablePrefix)
		}
	case tea.KeyUp, tea.KeyDown:
		if m.cursor == 1 {
			if m.cfg.OrderPreference == "last" {
				m.cfg.OrderPreference = "first"
			} else {
				m.cfg.OrderPreference = "last"
			}
		}
	case tea.KeyBackspace:
		if m.cursor == 0 && len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		if m.cursor == 0 && msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
		}
	}
	return m, nil
}

func (m model) updateTableSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < len(m.allTables)-1 {
			m.cursor++
		}
	case tea.KeySpace:
		t := m.allTables[m.cursor]
		m.cfg.TableModes[t] = cycleMode(m.cfg.TableModes[t])
	case tea.KeyEnter:
		m.step = stepConfirm
		m.cursor = 0
	}
	return m, nil
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.step = stepTableSelect
		m.cursor = 0
		return m, nil
	case tea.KeyEnter:
		m.step = stepRunning
		m.status = "Starting..."
		m.completedSteps = nil
		m.currentStep = ""
		m.currentDetail = ""
		m.progressCurrent = 0
		m.progressTotal = 0
		m.spinnerFrame = 0
		config.Save(m.cfg)

		return m, tea.Batch(
			func() tea.Msg {
				err := runSync(m.cfg, m.liveWP, m.stageWP, activeProgram)
				return syncDoneMsg{err: err}
			},
			tickSpinner(),
		)
	}
	return m, nil
}

// --- Helpers ---

func runSync(cfg *config.Config, liveWP, stageWP *wpconfig.WPConfig, p *tea.Program) error {
	log := &tuiLogger{p: p}

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

	log.Step("Exporting from live database")
	exp, err := export.Run(liveDB, liveWP.TablePrefix, cfg, log)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	log.StepDone("Export complete")

	log.Step("Importing to staging database")
	if err := sync.Import(stageDB, exp, log); err != nil {
		return fmt.Errorf("import: %w", err)
	}
	log.StepDone("Import complete")

	log.Step("Syncing files")
	if err := sync.FileSync(cfg.LivePath, cfg.StagePath, log); err != nil {
		return fmt.Errorf("file sync: %w", err)
	}
	log.StepDone("File sync complete")

	log.Step("Post-processing")
	domain := discovery.ExtractDomain(cfg.LivePath)
	stageDomain := discovery.ExtractDomain(cfg.StagePath)
	if err := sync.PostProcess(cfg.StagePath, domain, stageDomain, log); err != nil {
		return fmt.Errorf("post-processing: %w", err)
	}
	log.StepDone("Post-processing complete")

	return nil
}

func queryTables(liveDB *sql.DB) ([]string, error) {
	rows, err := liveDB.Query("SHOW TABLES")
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

func applyDefaultModes(cfg *config.Config, tables []string, prefix string) {
	structOnly := map[string]bool{
		prefix + "woocommerce_sessions":     true,
		prefix + "actionscheduler_actions":  true,
		prefix + "actionscheduler_claims":   true,
		prefix + "actionscheduler_groups":   true,
		prefix + "actionscheduler_logs":     true,
	}
	customRule := map[string]bool{
		prefix + "wc_orders":                true,
		prefix + "wc_order_addresses":       true,
		prefix + "wc_order_operational_data": true,
		prefix + "wc_orders_meta":           true,
		prefix + "wc_order_stats":           true,
		prefix + "wc_order_product_lookup":  true,
		prefix + "wc_order_tax_lookup":      true,
		prefix + "wc_order_coupon_lookup":   true,
		prefix + "comments":                 true,
		prefix + "commentmeta":              true,
		prefix + "users":                    true,
		prefix + "usermeta":                 true,
	}

	for _, t := range tables {
		if _, exists := cfg.TableModes[t]; exists {
			continue
		}
		if structOnly[t] {
			cfg.TableModes[t] = config.TableModeStructureOnly
		} else if customRule[t] {
			cfg.TableModes[t] = config.TableModeCustomRule
		} else {
			cfg.TableModes[t] = config.TableModeStructureAndData
		}
	}
}

func cycleMode(current config.TableMode) config.TableMode {
	switch current {
	case config.TableModeStructureAndData:
		return config.TableModeStructureOnly
	case config.TableModeStructureOnly:
		return config.TableModeIgnore
	case config.TableModeIgnore:
		return config.TableModeCustomRule
	default:
		return config.TableModeStructureAndData
	}
}

func getDisplayMode(cfg *config.Config, table string) string {
	mode, ok := cfg.TableModes[table]
	if !ok {
		mode = config.TableModeStructureAndData
	}
	switch mode {
	case config.TableModeStructureAndData:
		return "Structure & Data"
	case config.TableModeStructureOnly:
		return "Structure Only"
	case config.TableModeIgnore:
		return "Ignore"
	case config.TableModeCustomRule:
		return "Custom Rule"
	default:
		return string(mode)
	}
}

// --- TUI Logger ---

type tuiLogger struct {
	p *tea.Program
}

func (l *tuiLogger) Step(name string) {
	l.p.Send(stepMsg{name: name})
}

func (l *tuiLogger) Detail(msg string) {
	l.p.Send(detailMsg{msg: msg})
}

func (l *tuiLogger) Progress(current, total int) {
	l.p.Send(progressMsg{current: current, total: total})
}

func (l *tuiLogger) StepDone(msg string) {
	l.p.Send(stepDoneMsg{msg: msg})
}

var _ progress.Logger = (*tuiLogger)(nil)

func tickSpinner() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(_ time.Time) tea.Msg {
		return spinTickMsg{}
	})
}
