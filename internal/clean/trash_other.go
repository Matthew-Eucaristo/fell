//go:build !darwin && !linux

package clean

import (
	"errors"
	"os"
)

func moveToTrash(string) error {
	return errors.New("moving to the trash is not supported on this OS")
}

func stagingParent() string { return os.TempDir() }
