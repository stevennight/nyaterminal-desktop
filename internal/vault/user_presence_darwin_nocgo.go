//go:build darwin && !cgo

package vault

import "errors"

const userPresenceMethod = "Touch ID / macOS authentication"

func verifyUserPresence(string) error {
	return errors.New("macOS native authentication requires a CGO-enabled build")
}
