//go:build !darwin

package app

import "os/exec"

func startNewWindowProcess(executable string) error {
	// #nosec G204 -- Relaunches this application's own executable with a fixed flag.
	command := exec.Command(executable, "--new-window")
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	return nil
}
