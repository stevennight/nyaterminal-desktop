//go:build !windows && !darwin

package vault

const userPresenceMethod = "system credential store"

func verifyUserPresence(string) error {
	// Linux Secret Service only releases the item when the user's keyring is
	// unlocked. If no Secret Service is available, keyring access fails and
	// the application falls back to the master password.
	return nil
}
