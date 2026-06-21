//go:build windows

package vault

import (
	"runtime/debug"

	"github.com/nyaterminal/nyaterminal/desktop/internal/hellohelper"
)

const userPresenceMethod = "Windows Hello"

func verifyUserPresence(dataDir, message string) error {
	appendDiagnosticLog(dataDir, "hello", "verifyUserPresence start message=%q", message)
	defer func() {
		if recovered := recover(); recovered != nil {
			appendDiagnosticLog(
				dataDir,
				"hello",
				"verifyUserPresence panic=%v stack=%s",
				recovered,
				string(debug.Stack()),
			)
			panic(recovered)
		}
	}()
	err := hellohelper.VerifyUserPresence(dataDir, message)
	if err != nil {
		appendDiagnosticLog(dataDir, "hello", "hellohelper.VerifyUserPresence error=%v", err)
		return err
	}
	appendDiagnosticLog(dataDir, "hello", "verifyUserPresence success")
	return nil
}
