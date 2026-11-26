//go:build !windows && !darwin

package updatecheck

import (
	"context"
	"errors"
	"log/slog"
)

func platformAutomaticUpdateSupportReason() string {
	return "unsupported-platform"
}

func launchInstaller(string, string) error {
	return errors.New("automatic updates are unavailable on this platform")
}

func applyUpdate(context.Context, ApplyOptions, *slog.Logger) error {
	return errors.New("automatic updates are unavailable on this platform")
}
