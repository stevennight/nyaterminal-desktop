package app

import (
	"path/filepath"
	"strings"
)

// macOSAppBundlePath returns the enclosing .app bundle path when executable
// points to a binary inside "<App>.app/Contents/MacOS/<binary>".
func macOSAppBundlePath(executable string) (string, bool) {
	cleaned := filepath.Clean(executable)
	macosDir := filepath.Dir(cleaned)
	if filepath.Base(macosDir) != "MacOS" {
		return "", false
	}
	contentsDir := filepath.Dir(macosDir)
	if filepath.Base(contentsDir) != "Contents" {
		return "", false
	}
	bundleDir := filepath.Dir(contentsDir)
	if !strings.HasSuffix(bundleDir, ".app") || bundleDir == ".app" {
		return "", false
	}
	return bundleDir, true
}
