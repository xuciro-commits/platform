package fileio

import (
	"fmt"
	"os"
)

func WriteAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("journal: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("journal: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("journal: %w", err)
	}
	return os.Rename(tmp, path)
}
