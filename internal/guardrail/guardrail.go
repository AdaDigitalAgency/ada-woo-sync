package guardrail

import (
	"fmt"
	"path/filepath"

	"github.com/AdaDigitalAgency/wp-stage-sync/internal/wpconfig"
)

// ValidatePaths ensures live and stage paths are different.
func ValidatePaths(livePath, stagePath string) error {
	absLive, err := filepath.Abs(livePath)
	if err != nil {
		return fmt.Errorf("resolving live path: %w", err)
	}
	absStage, err := filepath.Abs(stagePath)
	if err != nil {
		return fmt.Errorf("resolving stage path: %w", err)
	}
	if absLive == absStage {
		return fmt.Errorf("SAFETY: live path and stage path are identical (%s) — aborting to protect production", absLive)
	}
	return nil
}

// ValidateDBs ensures live and stage databases are different.
func ValidateDBs(liveWP, stageWP *wpconfig.WPConfig) error {
	if liveWP.DBName == stageWP.DBName && liveWP.DBHost == stageWP.DBHost {
		return fmt.Errorf("SAFETY: live and stage use the same database (%s@%s) — aborting to protect production", liveWP.DBName, liveWP.DBHost)
	}
	return nil
}

// ValidateWPContentPaths ensures live and stage wp-content directories are different.
func ValidateWPContentPaths(livePath, stagePath string) error {
	absLive, err := filepath.Abs(filepath.Join(livePath, "wp-content"))
	if err != nil {
		return fmt.Errorf("resolving live wp-content path: %w", err)
	}
	absStage, err := filepath.Abs(filepath.Join(stagePath, "wp-content"))
	if err != nil {
		return fmt.Errorf("resolving stage wp-content path: %w", err)
	}
	if absLive == absStage {
		return fmt.Errorf("SAFETY: live and stage wp-content paths are identical (%s) — aborting to protect production", absLive)
	}
	return nil
}
