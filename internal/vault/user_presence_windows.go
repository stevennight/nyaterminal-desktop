//go:build windows

package vault

import (
	"errors"
	"fmt"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"

	"github.com/zzl/go-win32api/v2/win32"
	"github.com/zzl/go-winrtapi/winrt"
)

const userPresenceMethod = "Windows Hello"
const appWindowClassName = "wailsWindow"

type asyncResult[T any] struct {
	value T
	err   error
}

func awaitWinRT[T any](dataDir, name string, operation *winrt.IAsyncOperation[T]) (T, error) {
	var zero T
	if operation == nil {
		appendDiagnosticLog(dataDir, "hello", "%s operation=nil", name)
		return zero, errors.New("Windows Hello operation could not be created")
	}
	var asyncInfo *winrt.IAsyncInfo
	hr := operation.QueryInterface(&winrt.IID_IAsyncInfo, unsafe.Pointer(&asyncInfo))
	if win32.FAILED(hr) || asyncInfo == nil {
		appendDiagnosticLog(dataDir, "hello", "%s QueryInterface(IAsyncInfo) failed hr=0x%08X", name, uint32(hr))
		return zero, fmt.Errorf(
			"Windows Hello could not query async status information: HRESULT 0x%08X",
			uint32(hr),
		)
	}
	defer asyncInfo.Release()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		status := asyncInfo.Get_Status()
		switch status {
		case winrt.AsyncStatus_Completed:
			appendDiagnosticLog(dataDir, "hello", "%s status=Completed", name)
			return operation.GetResults(), nil
		case winrt.AsyncStatus_Canceled:
			appendDiagnosticLog(dataDir, "hello", "%s status=Canceled", name)
			return zero, errors.New("Windows Hello verification was cancelled")
		case winrt.AsyncStatus_Error:
			appendDiagnosticLog(
				dataDir,
				"hello",
				"%s status=Error hresult=0x%08X",
				name,
				uint32(asyncInfo.Get_ErrorCode().Value),
			)
			return zero, fmt.Errorf(
				"Windows Hello verification failed: HRESULT 0x%08X",
				uint32(asyncInfo.Get_ErrorCode().Value),
			)
		}
		time.Sleep(50 * time.Millisecond)
	}
	appendDiagnosticLog(dataDir, "hello", "%s status=TimedOut", name)
	return zero, errors.New("Windows Hello verification timed out")
}

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
	initialized := winrt.InitializeMt()
	defer initialized.Uninitialize()
	appendDiagnosticLog(dataDir, "hello", "WinRT initialized")

	verifier := winrt.NewIUserConsentVerifierStatics()
	if verifier == nil {
		appendDiagnosticLog(dataDir, "hello", "NewIUserConsentVerifierStatics returned nil")
		return errors.New("Windows Hello is unavailable")
	}
	availability, err := awaitWinRT(dataDir, "CheckAvailabilityAsync", verifier.CheckAvailabilityAsync())
	if err != nil {
		appendDiagnosticLog(dataDir, "hello", "CheckAvailabilityAsync error=%v", err)
		return err
	}
	appendDiagnosticLog(dataDir, "hello", "CheckAvailabilityAsync availability=%d", availability)
	if availability != winrt.UserConsentVerifierAvailability_Available {
		return fmt.Errorf("Windows Hello is unavailable: %s", helloAvailabilityMessage(availability))
	}
	operation, err := requestVerificationForWindow(dataDir, verifier, message)
	if err != nil {
		appendDiagnosticLog(dataDir, "hello", "requestVerificationForWindow error=%v", err)
		return err
	}
	result, err := awaitWinRT(dataDir, "RequestVerification", operation)
	if err != nil {
		appendDiagnosticLog(dataDir, "hello", "RequestVerification error=%v", err)
		return err
	}
	appendDiagnosticLog(dataDir, "hello", "RequestVerification result=%d", result)
	if result != winrt.UserConsentVerificationResult_Verified {
		return fmt.Errorf("Windows Hello did not verify the user: %s", helloResultMessage(result))
	}
	appendDiagnosticLog(dataDir, "hello", "verifyUserPresence success")
	return nil
}

func helloAvailabilityMessage(value winrt.UserConsentVerifierAvailability) string {
	switch value {
	case winrt.UserConsentVerifierAvailability_DeviceNotPresent:
		return "no compatible biometric or PIN device is present"
	case winrt.UserConsentVerifierAvailability_NotConfiguredForUser:
		return "Windows Hello is not configured for this user"
	case winrt.UserConsentVerifierAvailability_DisabledByPolicy:
		return "disabled by system policy"
	case winrt.UserConsentVerifierAvailability_DeviceBusy:
		return "the verification device is busy"
	default:
		return "unknown availability state"
	}
}

func helloResultMessage(value winrt.UserConsentVerificationResult) string {
	switch value {
	case winrt.UserConsentVerificationResult_Canceled:
		return "verification was cancelled"
	case winrt.UserConsentVerificationResult_RetriesExhausted:
		return "verification retries were exhausted"
	case winrt.UserConsentVerificationResult_NotConfiguredForUser:
		return "Windows Hello is not configured for this user"
	case winrt.UserConsentVerificationResult_DisabledByPolicy:
		return "disabled by system policy"
	case winrt.UserConsentVerificationResult_DeviceNotPresent:
		return "no compatible biometric or PIN device is present"
	case winrt.UserConsentVerificationResult_DeviceBusy:
		return "the verification device is busy"
	default:
		return "verification failed"
	}
}

func requestVerificationForWindow(
	dataDir string,
	verifier *winrt.IUserConsentVerifierStatics,
	message string,
) (*winrt.IAsyncOperation[winrt.UserConsentVerificationResult], error) {
	window, err := currentAppWindow()
	if err != nil {
		appendDiagnosticLog(dataDir, "hello", "currentAppWindow error=%v using=RequestVerificationAsync", err)
		return verifier.RequestVerificationAsync(message), nil
	}
	appendDiagnosticLog(
		dataDir,
		"hello",
		"window-bound verification disabled due to crash hwnd=0x%X class=%q title=%q using=RequestVerificationAsync",
		uintptr(window),
		windowClassName(window),
		windowTitle(window),
	)
	return verifier.RequestVerificationAsync(message), nil
}

func currentAppWindow() (win32.HWND, error) {
	currentProcessID := win32.GetCurrentProcessId()
	if handle := win32.GetForegroundWindow(); handle != 0 {
		handle = win32.GetAncestor(handle, win32.GA_ROOT)
		if isExpectedAppWindow(handle, currentProcessID) {
			return handle, nil
		}
	}

	var found win32.HWND
	callback := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		window := win32.HWND(hwnd)
		if isExpectedAppWindow(window, currentProcessID) {
			found = window
			return 0
		}
		return 1
	})
	win32.EnumWindows(win32.WNDENUMPROC(callback), 0)
	if found == 0 {
		return 0, errors.New("Windows Hello could not find the application window")
	}
	return found, nil
}

func isExpectedAppWindow(hwnd win32.HWND, currentProcessID uint32) bool {
	if hwnd == 0 {
		return false
	}
	if win32.GetAncestor(hwnd, win32.GA_ROOT) != hwnd {
		return false
	}
	if owner, _ := win32.GetWindow(hwnd, win32.GW_OWNER); owner != 0 {
		return false
	}
	if win32.IsWindowVisible(hwnd) == 0 {
		return false
	}
	var processID uint32
	win32.GetWindowThreadProcessId(hwnd, &processID)
	if processID != currentProcessID {
		return false
	}
	return windowClassName(hwnd) == appWindowClassName
}

func windowClassName(hwnd win32.HWND) string {
	var buffer [256]uint16
	length, _ := win32.GetClassNameW(
		hwnd,
		win32.PWSTR(unsafe.Pointer(&buffer[0])),
		int32(len(buffer)),
	)
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}

func windowTitle(hwnd win32.HWND) string {
	var buffer [256]uint16
	length, _ := win32.GetWindowTextW(
		hwnd,
		win32.PWSTR(unsafe.Pointer(&buffer[0])),
		int32(len(buffer)),
	)
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}
