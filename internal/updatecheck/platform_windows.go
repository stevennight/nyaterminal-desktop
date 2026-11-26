//go:build windows

package updatecheck

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	createNoWindow         = 0x0800_0000
	detachedProcess        = 0x0000_0008
	createNewProcessGroup  = 0x0000_0200
	createBreakawayFromJob = 0x0100_0000
)

func platformAutomaticUpdateSupportReason() string {
	if installedCopyMatchesExecutable() {
		return ""
	}
	return "not-installed"
}

func launchInstaller(installerPath, targetVersion string) error {
	if !strings.EqualFold(filepath.Ext(installerPath), ".exe") {
		return errors.New("the downloaded update is not a supported Windows installer")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	appDir := filepath.Dir(executable)
	workDir := filepath.Dir(installerPath)
	updaterPath := filepath.Join(workDir, "NyaTerminal-updater.exe")
	if err := copyFile(executable, updaterPath, 0o700); err != nil {
		return fmt.Errorf("prepare updater helper: %w", err)
	}

	params := strings.Join([]string{
		applyUpdateFlag,
		"--installer", quoteWindowsArg(installerPath),
		"--target", quoteWindowsArg(appDir),
		"--version", quoteWindowsArg(targetVersion),
		"--wait-pid", strconv.Itoa(os.Getpid()),
		"--work-dir", quoteWindowsArg(workDir),
	}, " ")

	verb := windows.StringToUTF16Ptr("runas")
	file := windows.StringToUTF16Ptr(updaterPath)
	args := windows.StringToUTF16Ptr(params)
	dir := windows.StringToUTF16Ptr(workDir)
	if err := windows.ShellExecute(0, verb, file, args, dir, windows.SW_HIDE); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return errors.New("the update installer was not started because administrator approval was canceled")
		}
		return fmt.Errorf("start the update installer with administrator approval: %w", err)
	}
	return nil
}

func quoteWindowsArg(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

func applyUpdate(ctx context.Context, opts ApplyOptions, log *slog.Logger) error {
	appDir := filepath.Clean(opts.Target)
	appExe := filepath.Join(appDir, "NyaTerminal.exe")
	if !isRegularFile(appExe) || !isRegularFile(filepath.Join(appDir, "uninstall.exe")) {
		waitPID(opts.WaitPID, 30*time.Second)
		_ = relaunchWindowsApp(appExe)
		return fmt.Errorf("refusing to update non-installed directory %q", appDir)
	}

	if !waitPID(opts.WaitPID, 30*time.Second) {
		log.Warn("main app did not exit in time; terminating processes under install directory")
	}
	if count := killUnder(appDir); count > 0 {
		log.Info("terminated lingering processes", "count", count)
	}

	backup := filepath.Join(opts.WorkDir, "backup")
	if err := os.RemoveAll(backup); err != nil {
		_ = relaunchWindowsApp(appExe)
		return fmt.Errorf("clean previous backup: %w", err)
	}
	if err := copyTree(appDir, backup); err != nil {
		_ = relaunchWindowsApp(appExe)
		return fmt.Errorf("backup existing install directory: %w", err)
	}

	installErr := func() error {
		ictx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		// NSIS requires /D= to be last and unquoted.
		cmdline := fmt.Sprintf(`"%s" /S /D=%s`, opts.Installer, appDir)
		code, err := runHidden(ictx, opts.Installer, cmdline)
		if err != nil {
			return fmt.Errorf("run silent NSIS installer: %w", err)
		}
		if code != 0 {
			return fmt.Errorf("silent NSIS installer exited with code %d", code)
		}
		return verifyInstalledVersion(ctx, appExe, opts.Version)
	}()

	if installErr != nil {
		log.Error("silent NSIS install failed; restoring backup", "error", installErr)
		if restoreErr := retry(10, func() error {
			return copyTree(backup, appDir)
		}); restoreErr != nil {
			log.Error("backup restore failed", "error", restoreErr)
		}
		_ = relaunchWindowsApp(appExe)
		return installErr
	}

	_ = os.RemoveAll(backup)
	_ = os.Remove(opts.Installer)
	return relaunchWindowsApp(appExe)
}

func runHidden(ctx context.Context, exe, cmdline string) (int, error) {
	// #nosec G204 -- Executes the SHA-256-verified NSIS installer silently.
	cmd := exec.CommandContext(ctx, exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: cmdline, HideWindow: true, CreationFlags: createNoWindow}
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func verifyInstalledVersion(ctx context.Context, binaryPath, wantVersion string) error {
	vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// #nosec G204 -- Runs the verified NyaTerminal.exe binary with --version.
	cmd := exec.CommandContext(vctx, binaryPath, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("run %s --version: %w", filepath.Base(binaryPath), err)
	}
	got, ok := parseVersionOutput(string(out))
	if !ok {
		return fmt.Errorf("unexpected version output %q", strings.TrimSpace(string(out)))
	}
	want, ok := canonicalStableVersion(wantVersion)
	if !ok || got != want {
		return fmt.Errorf("installed version is %s, expected %s", got, wantVersion)
	}
	return nil
}

// relaunchWindowsApp starts NyaTerminal.exe via explorer.exe so the relaunched
// GUI runs at the logged-in user's normal (non-elevated) integrity level even
// when the updater itself was elevated via UAC.
func relaunchWindowsApp(exe string) error {
	// #nosec G204 -- Relaunches the installed NyaTerminal.exe via Windows Explorer.
	cmd := exec.Command("explorer.exe", exe)
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
		return nil
	}
	return spawnDetached(exe)
}

func spawnDetached(exe string, args ...string) error {
	start := func(flags uint32) error {
		// #nosec G204 -- Relaunches the installed NyaTerminal.exe binary.
		cmd := exec.Command(exe, args...)
		cmd.Dir = filepath.Dir(exe)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	base := uint32(detachedProcess | createNewProcessGroup | createNoWindow)
	if err := start(base | createBreakawayFromJob); err == nil {
		return nil
	}
	return start(base)
}

func waitPID(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer func() { _ = windows.CloseHandle(h) }()
	ev, _ := windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
	return ev == windows.WAIT_OBJECT_0
}

func killUnder(dir string) int {
	dir = strings.ToLower(filepath.Clean(dir)) + `\`
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer func() { _ = windows.CloseHandle(snap) }()
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	self := uint32(os.Getpid())
	var victims []windows.Handle
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ProcessID == self || e.ProcessID == 0 {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, e.ProcessID)
		if err != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		n := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil &&
			strings.HasPrefix(strings.ToLower(windows.UTF16ToString(buf[:n])), dir) {
			if windows.TerminateProcess(h, 1) == nil {
				victims = append(victims, h)
				continue
			}
		}
		_ = windows.CloseHandle(h)
	}
	for _, h := range victims {
		_, _ = windows.WaitForSingleObject(h, 10_000)
		_ = windows.CloseHandle(h)
	}
	return len(victims)
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
