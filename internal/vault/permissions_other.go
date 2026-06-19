//go:build !windows

package vault

import "os"

func hardenDirectory(path string) error {
	return os.Chmod(path, 0o700)
}

func hardenFile(path string) error {
	return os.Chmod(path, 0o600)
}
