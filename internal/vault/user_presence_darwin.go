//go:build darwin && cgo

package vault

/*
#cgo LDFLAGS: -framework Foundation -framework LocalAuthentication
#include <stdlib.h>
int nya_verify_user_presence(const char *reason, char **error_message);
*/
import "C"

import (
	"errors"
	"unsafe"
)

const userPresenceMethod = "Touch ID / macOS authentication"

func verifyUserPresence(_ string, message string) error {
	reason := C.CString(message)
	defer C.free(unsafe.Pointer(reason))
	var errorMessage *C.char
	if C.nya_verify_user_presence(reason, &errorMessage) == 1 {
		return nil
	}
	if errorMessage == nil {
		return errors.New("macOS user verification failed")
	}
	defer C.free(unsafe.Pointer(errorMessage))
	return errors.New(C.GoString(errorMessage))
}
