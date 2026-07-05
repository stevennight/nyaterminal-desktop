//go:build !windows

package vault

import "os"

func hardenDirectory(path string) error {
	// #nosec G302 -- Directories need execute permission for the owner; 0700 denies access to group and others.
	return os.Chmod(path, 0o700)
}

func hardenFile(path string) error {
	return os.Chmod(path, 0o600)
}
