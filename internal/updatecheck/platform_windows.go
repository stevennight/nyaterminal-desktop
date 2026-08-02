//go:build windows

package updatecheck

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func platformAutomaticUpdateSupportReason() string {
	if installedCopyMatchesExecutable() {
		return ""
	}
	return "not-installed"
}

func launchInstaller(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".exe") {
		return errors.New("the downloaded update is not a supported Windows installer")
	}
	// #nosec G204 -- path is a private temp file populated from a trusted, checksum-verified release asset.
	if err := exec.Command(path).Start(); err != nil {
		return fmt.Errorf("start the update installer: %w", err)
	}
	return nil
}

func installedCopyMatchesExecutable() bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	executable = normalizeWindowsPath(executable)
	const uninstallPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall`
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
			uninstall, err := registry.OpenKey(root, uninstallPath, registry.READ|view)
			if err != nil {
				continue
			}
			names, _ := uninstall.ReadSubKeyNames(-1)
			for _, name := range names {
				entry, err := registry.OpenKey(uninstall, name, registry.READ|view)
				if err != nil {
					continue
				}
				displayName, _, _ := entry.GetStringValue("DisplayName")
				displayIcon, _, _ := entry.GetStringValue("DisplayIcon")
				installLocation, _, _ := entry.GetStringValue("InstallLocation")
				_ = entry.Close()
				if strings.EqualFold(strings.TrimSpace(displayName), "NyaTerminal") &&
					registryEntryMatchesExecutable(executable, installLocation, displayIcon) {
					_ = uninstall.Close()
					return true
				}
			}
			_ = uninstall.Close()
		}
	}
	return false
}

func registryEntryMatchesExecutable(executable, installLocation, displayIcon string) bool {
	if location := strings.TrimSpace(installLocation); location != "" {
		location = normalizeWindowsPath(location)
		if executable == location || strings.HasPrefix(executable, location+`\`) {
			return true
		}
	}
	icon := strings.TrimSpace(displayIcon)
	if strings.HasPrefix(icon, `"`) {
		icon = strings.TrimPrefix(icon, `"`)
		if end := strings.Index(icon, `"`); end >= 0 {
			icon = icon[:end]
		}
	} else if comma := strings.Index(icon, ","); comma >= 0 {
		icon = icon[:comma]
	}
	return icon != "" && executable == normalizeWindowsPath(icon)
}

func normalizeWindowsPath(path string) string {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err == nil {
		path = absolute
	}
	return strings.ToLower(strings.TrimRight(strings.ReplaceAll(path, "/", `\`), `\`))
}
