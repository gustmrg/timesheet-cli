//go:build !windows

package updater

import "os"

func replaceExecutable(replacement, executable string) error {
	return os.Rename(replacement, executable)
}
