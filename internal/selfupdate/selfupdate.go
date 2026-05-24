package selfupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/creativeprojects/go-selfupdate"
)

const (
	repoOwner = "AdaDigitalAgency"
	repoName  = "ada-woo-sync"
)

func Update(currentVersion string) error {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return fmt.Errorf("creating GitHub source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return fmt.Errorf("creating updater: %w", err)
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.NewRepositorySlug(repoOwner, repoName))
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	if !found {
		fmt.Println("No releases found.")
		return nil
	}

	if currentVersion != "dev" && latest.LessOrEqual(currentVersion) {
		fmt.Printf("Already up to date (v%s).\n", currentVersion)
		return nil
	}

	fmt.Printf("Updating from %s to %s (%s/%s)...\n",
		currentVersion, latest.Version(), runtime.GOOS, runtime.GOARCH)

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	if err := updater.UpdateTo(context.Background(), latest, exe); err != nil {
		return fmt.Errorf("applying update: %w", err)
	}

	fmt.Printf("Updated to v%s.\n", latest.Version())
	return nil
}

// CheckVersion prints a one-liner to stderr if a newer version is available.
// Skips the network call if the last check was less than 8 hours ago.
func CheckVersion(currentVersion string) {
	if currentVersion == "dev" {
		return
	}

	tsFile := updateCheckPath()
	if !shouldCheck(tsFile) {
		return
	}

	// Persist timestamp regardless of outcome so we don't spam on errors
	saveTimestamp(tsFile)

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return
	}
	updater, err := selfupdate.NewUpdater(selfupdate.Config{Source: source})
	if err != nil {
		return
	}

	latest, found, err := updater.DetectLatest(context.Background(), selfupdate.NewRepositorySlug(repoOwner, repoName))
	if err != nil || !found {
		return
	}

	if latest.LessOrEqual(currentVersion) {
		return
	}

	fmt.Fprintf(os.Stderr, "\033[33mA new version is available: v%s (current: %s). Run 'wp-sync --update' to upgrade.\033[0m\n", latest.Version(), currentVersion)
}

func updateCheckPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "wp-sync", ".last-update-check")
}

func shouldCheck(path string) bool {
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true // no file = never checked
	}
	ts, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return true
	}
	return time.Since(time.Unix(ts, 0)) >= 8*time.Hour
}

func saveTimestamp(path string) {
	if path == "" {
		return
	}
	_ = os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0644)
}
