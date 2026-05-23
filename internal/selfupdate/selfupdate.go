package selfupdate

import (
	"context"
	"fmt"
	"runtime"

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
