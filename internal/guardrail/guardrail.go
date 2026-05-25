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
