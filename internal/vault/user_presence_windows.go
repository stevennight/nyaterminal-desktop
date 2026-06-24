//go:build windows

package vault

import (
	"github.com/nyaterminal/nyaterminal/desktop/internal/hellohelper"
)

const userPresenceMethod = "Windows Hello"

func verifyUserPresence(dataDir, message string) error {
	return hellohelper.VerifyUserPresence(dataDir, message)
}
