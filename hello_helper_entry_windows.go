//go:build windows

package main

import "github.com/nyaterminal/nyaterminal-desktop/internal/hellohelper"

func runHelloHelperIfRequested(args []string) (bool, int, error) {
	return hellohelper.MaybeRun(args)
}
