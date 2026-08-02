//go:build !windows

package updatecheck

import "errors"

func platformAutomaticUpdateSupportReason() string {
	return "unsupported-platform"
}

func launchInstaller(string) error {
	return errors.New("automatic updates are unavailable on this platform")
}
