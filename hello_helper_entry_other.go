//go:build !windows

package main

func runHelloHelperIfRequested(_ []string) (bool, int, error) {
	return false, 0, nil
}
