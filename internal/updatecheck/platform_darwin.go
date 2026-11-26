//go:build darwin

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
)

func platformAutomaticUpdateSupportReason() string {
	executable, err := os.Executable()
	if err != nil {
		return "not-installed"
	}
	bundlePath, ok := macOSAppBundlePath(executable)
	if !ok {
		return "not-installed"
	}
	if !directoryIsWritable(filepath.Dir(bundlePath)) {
		return "not-installed"
	}
	return ""
}

func directoryIsWritable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".nyaterminal-update-probe-*")
	if err != nil {
		return false
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)
	return true
}

func launchInstaller(installerPath, targetVersion string) error {
	if !strings.EqualFold(filepath.Ext(installerPath), ".dmg") {
		return errors.New("the downloaded update is not a supported macOS disk image")
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate current executable: %w", err)
	}
	bundlePath, ok := macOSAppBundlePath(executable)
	if !ok {
		return errors.New("automatic updates are only available when running from a macOS .app bundle")
	}
	if !directoryIsWritable(filepath.Dir(bundlePath)) {
		return errors.New("cannot replace the application in a read-only location; move NyaTerminal.app to Applications first")
	}

	workDir := filepath.Dir(installerPath)
	updaterPath := filepath.Join(workDir, "NyaTerminal-updater")
	if err := copyFile(executable, updaterPath, 0o755); err != nil {
		return fmt.Errorf("prepare updater helper: %w", err)
	}
	if err := os.Chmod(updaterPath, 0o755); err != nil {
		return fmt.Errorf("set updater helper permissions: %w", err)
	}

	// #nosec G204 -- Spawns our own copied binary outside the .app bundle with fixed flags.
	cmd := exec.Command(
		updaterPath,
		applyUpdateFlag,
		"--installer", installerPath,
		"--target", bundlePath,
		"--version", targetVersion,
		"--wait-pid", strconv.Itoa(os.Getpid()),
		"--work-dir", workDir,
	)
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start macOS updater helper: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func applyUpdate(ctx context.Context, opts ApplyOptions, log *slog.Logger) error {
	appBundle := filepath.Clean(opts.Target)
	infoPlist := filepath.Join(appBundle, "Contents", "Info.plist")
	binaryRel := filepath.Join("Contents", "MacOS", "NyaTerminal")
	if !strings.HasSuffix(appBundle, ".app") || !isRegularFile(infoPlist) || !isRegularFile(filepath.Join(appBundle, binaryRel)) {
		waitPID(opts.WaitPID, 30*time.Second)
		_ = launchAppBundle(appBundle)
		return fmt.Errorf("refusing to update non-app bundle target %q", appBundle)
	}

	if !waitPID(opts.WaitPID, 30*time.Second) {
		log.Warn("main app did not exit in time; terminating processes under bundle")
	}
	if count := killUnder(appBundle); count > 0 {
		log.Info("terminated lingering processes", "count", count)
	}

	backup := filepath.Join(opts.WorkDir, "backup.app")
	if err := os.RemoveAll(backup); err != nil {
		_ = launchAppBundle(appBundle)
		return fmt.Errorf("clean previous backup: %w", err)
	}
	if err := copyAppBundle(ctx, appBundle, backup); err != nil {
		_ = launchAppBundle(appBundle)
		return fmt.Errorf("backup existing app bundle: %w", err)
	}

	if err := installFromDMG(ctx, opts.Installer, appBundle, opts.WorkDir, opts.Version); err != nil {
		log.Error("DMG install failed; restoring backup", "error", err)
		if restoreErr := retry(5, func() error {
			return replaceAppBundle(ctx, backup, appBundle)
		}); restoreErr != nil {
			log.Error("backup restore failed", "error", restoreErr)
		}
		_ = launchAppBundle(appBundle)
		return err
	}

	_ = os.RemoveAll(backup)
	_ = os.Remove(opts.Installer)
	return launchAppBundle(appBundle)
}

func copyAppBundle(ctx context.Context, src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	// #nosec G204 -- Duplicates the app bundle preserving macOS bundle metadata and symlinks.
	out, err := exec.CommandContext(ctx, "/usr/bin/ditto", src, dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ditto %s -> %s: %w: %s", src, dst, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func replaceAppBundle(ctx context.Context, srcApp, dstApp string) error {
	parent := filepath.Dir(dstApp)
	base := filepath.Base(dstApp)
	staged := filepath.Join(parent, "."+base+".new")
	prev := filepath.Join(parent, "."+base+".old")

	_ = os.RemoveAll(staged)
	_ = os.RemoveAll(prev)
	if err := copyAppBundle(ctx, srcApp, staged); err != nil {
		return err
	}
	// Clear quarantine xattr if present so Gatekeeper does not block unattended relaunch.
	// #nosec G204 -- Removes quarantine attribute on our verified, SHA-256-checked app bundle.
	_ = exec.CommandContext(ctx, "/usr/bin/xattr", "-dr", "com.apple.quarantine", staged).Run()

	if _, err := os.Stat(dstApp); err == nil {
		if err := os.Rename(dstApp, prev); err != nil {
			_ = os.RemoveAll(staged)
			return fmt.Errorf("move previous app aside: %w", err)
		}
	}
	if err := os.Rename(staged, dstApp); err != nil {
		_ = os.Rename(prev, dstApp)
		_ = os.RemoveAll(staged)
		return fmt.Errorf("replace app bundle: %w", err)
	}
	_ = os.RemoveAll(prev)
	return nil
}

func installFromDMG(ctx context.Context, dmgPath, dstApp, workDir, wantVersion string) error {
	mnt, err := os.MkdirTemp(workDir, "mnt-*")
	if err != nil {
		return fmt.Errorf("create DMG mount directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(mnt) }()

	// #nosec G204 -- Mounts our SHA-256-verified DMG read-only.
	out, err := exec.CommandContext(ctx, "/usr/bin/hdiutil", "attach", "-nobrowse", "-readonly", "-noverify", "-mountpoint", mnt, dmgPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mount DMG: %w: %s", err, strings.TrimSpace(string(out)))
	}
	defer func() {
		// #nosec G204 -- Detaches our temporary DMG mount point.
		_ = exec.Command("/usr/bin/hdiutil", "detach", "-force", mnt).Run()
	}()

	srcApp, err := findAppBundleInDir(mnt, filepath.Base(dstApp))
	if err != nil {
		return err
	}
	if err := verifyInstalledVersion(ctx, filepath.Join(srcApp, "Contents", "MacOS", "NyaTerminal"), wantVersion); err != nil {
		return fmt.Errorf("verify staged app version: %w", err)
	}
	if err := replaceAppBundle(ctx, srcApp, dstApp); err != nil {
		return err
	}
	return verifyInstalledVersion(ctx, filepath.Join(dstApp, "Contents", "MacOS", "NyaTerminal"), wantVersion)
}

func findAppBundleInDir(dir, preferred string) (string, error) {
	if preferred != "" && strings.HasSuffix(preferred, ".app") {
		candidate := filepath.Join(dir, preferred)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".app") {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", errors.New("no .app bundle found inside the DMG")
}

func verifyInstalledVersion(ctx context.Context, binaryPath, wantVersion string) error {
	vctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// #nosec G204 -- Runs the verified NyaTerminal binary with --version.
	out, err := exec.CommandContext(vctx, binaryPath, "--version").Output()
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

func launchAppBundle(appBundle string) error {
	// #nosec G204 -- Relaunches the updated NyaTerminal.app bundle.
	out, err := exec.Command("/usr/bin/open", "-n", "-a", appBundle).CombinedOutput()
	if err != nil {
		return fmt.Errorf("relaunch app bundle: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func waitPID(pid int, timeout time.Duration) bool {
	if pid <= 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func killUnder(dir string) int {
	prefix := filepath.Clean(dir) + "/"
	out, err := exec.Command("/bin/ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return 0
	}
	self := os.Getpid()
	var victims []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sp := strings.IndexAny(line, " \t")
		if sp <= 0 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(line[:sp]))
		if err != nil || pid <= 0 || pid == self {
			continue
		}
		comm := strings.TrimSpace(line[sp+1:])
		if strings.HasPrefix(comm, prefix) {
			if syscall.Kill(pid, syscall.SIGTERM) == nil {
				victims = append(victims, pid)
			}
		}
	}
	if len(victims) == 0 {
		return 0
	}
	deadline := time.Now().Add(10 * time.Second)
	for _, pid := range victims {
		rem := time.Until(deadline)
		if rem < 0 {
			rem = 0
		}
		if !waitPID(pid, rem) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			waitPID(pid, 2*time.Second)
		}
	}
	return len(victims)
}
