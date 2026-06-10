package wpcli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

const pharURL = "https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar"

// Resolve returns the wp-cli command parts: either ["wp"] if wp is in PATH,
// or ["php", "/path/to/wp-cli.phar"] after downloading the PHAR if needed.
func Resolve() ([]string, error) {
	if path, err := exec.LookPath("wp"); err == nil {
		return []string{path}, nil
	}

	pharPath := localPharPath()
	if _, err := os.Stat(pharPath); err == nil {
		return pharCmd(pharPath)
	}

	// wp not found — try to download the PHAR
	phpPath, err := exec.LookPath("php")
	if err != nil {
		return nil, fmt.Errorf("wp-cli not found and php not available to run the PHAR fallback; install wp-cli or php")
	}

	if err := downloadPhar(pharPath); err != nil {
		return nil, fmt.Errorf("downloading wp-cli.phar: %w", err)
	}

	return []string{phpPath, pharPath}, nil
}

// ResolveExisting behaves like Resolve but never downloads the PHAR. It returns
// the wp-cli command parts and ok=true when wp-cli is available locally, or
// ok=false when it is not (so callers can plan without side effects).
func ResolveExisting() ([]string, bool) {
	if path, err := exec.LookPath("wp"); err == nil {
		return []string{path}, true
	}
	pharPath := localPharPath()
	if _, err := os.Stat(pharPath); err == nil {
		if cmd, err := pharCmd(pharPath); err == nil {
			return cmd, true
		}
	}
	return nil, false
}

func localPharPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "wp-stage-sync", "wp-cli.phar")
}

func pharCmd(pharPath string) ([]string, error) {
	phpPath, err := exec.LookPath("php")
	if err != nil {
		return nil, fmt.Errorf("wp-cli.phar exists but php not found in PATH")
	}
	return []string{phpPath, pharPath}, nil
}

func downloadPhar(dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	resp, err := http.Get(pharURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(dest)
		return err
	}
	return os.Chmod(dest, 0755)
}
