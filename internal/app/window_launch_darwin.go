//go:build darwin

package app

import "os/exec"

func startNewWindowProcess(executable string) error {
	var command *exec.Cmd
	if bundlePath, ok := macOSAppBundlePath(executable); ok {
		// #nosec G204 -- Relaunches this application's own .app bundle with a fixed flag.
		command = exec.Command("/usr/bin/open", "-n", bundlePath, "--args", "--new-window")
	} else {
		// #nosec G204 -- Relaunches this application's own executable with a fixed flag.
		command = exec.Command(executable, "--new-window")
	}
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	return nil
}
