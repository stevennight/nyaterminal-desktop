package app

import (
	"os"
	"path/filepath"
)

func defaultSFTPLocalDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "."
	}
	desktop := filepath.Join(home, "Desktop")
	if info, err := os.Stat(desktop); err == nil && info.IsDir() {
		return desktop
	}
	return home
}
