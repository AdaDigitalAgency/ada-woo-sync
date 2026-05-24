package progress

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Logger reports sync progress. Implementations differ for TUI vs CLI.
type Logger interface {
	Step(name string)
	Detail(msg string)
	Progress(current, total int)
	StepDone(msg string)
}

// --- CLI Logger (unattended mode) ---

type CLILogger struct{}

func NewCLILogger() *CLILogger { return &CLILogger{} }

func (l *CLILogger) Step(name string) {
	fmt.Fprintf(os.Stderr, "%s ► %s\n", ts(), name)
}

func (l *CLILogger) Detail(msg string) {
	fmt.Fprintf(os.Stderr, "%s   %s\n", ts(), msg)
}

func (l *CLILogger) Progress(current, total int) {
	if total <= 0 {
		return
	}
	bar := renderBar(current, total, 24)
	fmt.Fprintf(os.Stderr, "%s   %s %d/%d\n", ts(), bar, current, total)
}

func (l *CLILogger) StepDone(msg string) {
	fmt.Fprintf(os.Stderr, "%s ✓ %s\n", ts(), msg)
}

// --- Nop Logger ---

type NopLogger struct{}

func (NopLogger) Step(string)          {}
func (NopLogger) Detail(string)        {}
func (NopLogger) Progress(int, int)    {}
func (NopLogger) StepDone(string)      {}

// --- Helpers ---

func ts() string {
	return fmt.Sprintf("[%s]", time.Now().Format("15:04:05"))
}

func renderBar(current, total, width int) string {
	if total <= 0 {
		return ""
	}
	filled := width * current / total
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}
