//go:build windows

package updater

import (
	"fmt"
	"os"
)

func replaceExecutable(replacement, executable string) error {
	backup := executable + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(executable, backup); err != nil {
		return fmt.Errorf("move current executable aside: %w", err)
	}
	if err := os.Rename(replacement, executable); err != nil {
		if rollbackErr := os.Rename(backup, executable); rollbackErr != nil {
			return fmt.Errorf("install replacement: %v; rollback failed: %w", err, rollbackErr)
		}
		return fmt.Errorf("install replacement: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}
