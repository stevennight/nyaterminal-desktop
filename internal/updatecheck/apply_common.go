package updatecheck

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const applyUpdateFlag = "--apply-update"

// ApplyOptions configures the detached self-updater helper process.
type ApplyOptions struct {
	Installer string
	Target    string
	Version   string
	WaitPID   int
	WorkDir   string
}

// RunHelperIfRequested executes the detached update helper when the process is
// started with "--apply-update". It returns handled=false for normal launches.
func RunHelperIfRequested(args []string) (bool, int, error) {
	if len(args) == 0 || args[0] != applyUpdateFlag {
		return false, 0, nil
	}
	fs := flag.NewFlagSet("apply-update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts ApplyOptions
	fs.StringVar(&opts.Installer, "installer", "", "path to verified installer")
	fs.StringVar(&opts.Target, "target", "", "path to installed app bundle or directory")
	fs.StringVar(&opts.Version, "version", "", "expected version after install")
	fs.IntVar(&opts.WaitPID, "wait-pid", 0, "parent process PID to wait for")
	fs.StringVar(&opts.WorkDir, "work-dir", "", "working directory for backups and logs")
	if err := fs.Parse(args[1:]); err != nil {
		return true, 2, err
	}
	if strings.TrimSpace(opts.Installer) == "" || strings.TrimSpace(opts.Target) == "" || strings.TrimSpace(opts.Version) == "" {
		return true, 2, errors.New("apply-update requires --installer, --target, and --version")
	}
	if strings.TrimSpace(opts.WorkDir) == "" {
		opts.WorkDir = filepath.Dir(opts.Installer)
	}

	logger, closeLog := openUpdateLogger(filepath.Join(opts.WorkDir, "update.log"))
	defer closeLog()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	logger.Info("starting unattended update", "target", opts.Target, "version", opts.Version, "wait_pid", opts.WaitPID)
	if err := applyUpdate(ctx, opts, logger); err != nil {
		logger.Error("unattended update failed", "error", err)
		return true, 1, err
	}
	logger.Info("unattended update succeeded", "version", opts.Version)
	return true, 0, nil
}

func openUpdateLogger(logPath string) (*slog.Logger, func()) {
	_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
	// #nosec G304 -- Log path is fixed inside the user's update cache directory.
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil)), func() {}
	}
	return slog.New(slog.NewTextHandler(file, nil)), func() { _ = file.Close() }
}

func parseVersionOutput(output string) (string, bool) {
	fields := strings.Fields(output)
	if len(fields) < 2 {
		return "", false
	}
	return canonicalStableVersion(fields[1])
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

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

func copyFile(src, dst string, perm os.FileMode) (err error) {
	// #nosec G304 -- Copies verified application/backup files within controlled update paths.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	// #nosec G304 -- Writes inside controlled update or install directories.
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(linkTarget, target)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func retry(attempts int, fn func() error) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if lastErr = fn(); lastErr == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("failed after %d attempts: %w", attempts, lastErr)
}
