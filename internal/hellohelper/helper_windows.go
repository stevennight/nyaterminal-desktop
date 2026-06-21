//go:build windows

package hellohelper

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-win32api/v2/win32"
	"github.com/zzl/go-winrtapi/winrt"
)

const (
	helperFlag       = "--hello-helper"
	helperClassName  = "NyaHelloHelperWindow"
	helperWindowText = "NyaTerminal 安全验证"
	wmHelperDone     = win32.WM_APP + 1

	exitCodeVerified    = 0
	exitCodeCancelled   = 10
	exitCodeUnavailable = 11
	exitCodeFailed      = 12
	exitCodeUsage       = 64

	helperWindowWidth  = 448
	helperWindowHeight = 196
)

var (
	iidAsyncOperationUserConsentVerificationResult = syscall.GUID{0xFD596FFD, 0x2318, 0x558F,
		[8]byte{0x9D, 0xBE, 0xD2, 0x1D, 0xF4, 0x37, 0x64, 0xA5}}

	logMu            sync.Mutex
	activeHelperDir  string
	helperResultChan chan error
	helperAccent     = win32.RGB(63, 127, 214)
	helperBackground = win32.RGB(244, 247, 251)
	helperPanel      = win32.RGB(255, 255, 255)
	helperText       = win32.RGB(24, 30, 38)
	helperMutedText  = win32.RGB(98, 108, 118)
	helperBorder     = win32.RGB(228, 233, 240)
)

type helperMode string

const (
	helperModeBound helperMode = "bound"
	helperModePlain helperMode = "plain"
)

type helperErrorKind int

const (
	helperErrorCancelled helperErrorKind = iota + 1
	helperErrorUnavailable
	helperErrorFailed
)

type helperError struct {
	kind    helperErrorKind
	message string
}

func (e *helperError) Error() string {
	return e.message
}

type helperConfig struct {
	dataDir string
	message string
	mode    helperMode
	topmost bool
}

func VerifyUserPresence(dataDir, message string) error {
	appendDiagnosticLog(dataDir, "hello-helper-launcher", "verify start message=%q", message)

	exePath, err := os.Executable()
	if err != nil {
		appendDiagnosticLog(dataDir, "hello-helper-launcher", "os.Executable error=%v", err)
		return fmt.Errorf("cannot locate NyaTerminal executable for Windows Hello: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	args := []string{helperFlag, "--mode=bound", "--topmost=true", "--message", message}
	if dataDir != "" {
		args = append(args, "--data-dir", dataDir)
	}

	cmd := exec.CommandContext(ctx, exePath, args...)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	appendDiagnosticLog(
		dataDir,
		"hello-helper-launcher",
		"helper exit err=%v output=%q",
		err,
		text,
	)

	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("Windows Hello timed out")
	}
	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if text != "" {
			return errors.New(text)
		}
		switch exitErr.ExitCode() {
		case exitCodeCancelled:
			return errors.New("Windows Hello verification was cancelled")
		case exitCodeUnavailable:
			return errors.New("Windows Hello is unavailable")
		default:
			return errors.New("Windows Hello verification failed")
		}
	}

	return fmt.Errorf("Windows Hello helper failed: %w", err)
}

func MaybeRun(args []string) (bool, int, error) {
	if len(args) == 0 || args[0] != helperFlag {
		return false, 0, nil
	}

	config, err := parseConfig(args[1:])
	if err != nil {
		return true, exitCodeUsage, err
	}
	if config.dataDir == "" {
		dataDir, dirErr := defaultDataDir()
		if dirErr == nil {
			config.dataDir = dataDir
		}
	}

	err = runHelper(config)
	if err != nil {
		return true, exitCodeForError(err), err
	}
	return true, exitCodeVerified, nil
}

func parseConfig(args []string) (helperConfig, error) {
	config := helperConfig{
		message: "Unlock NyaTerminal",
		mode:    helperModeBound,
		topmost: true,
	}

	flags := flag.NewFlagSet("hello-helper", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var mode string
	flags.StringVar(&mode, "mode", string(helperModeBound), "")
	flags.StringVar(&config.message, "message", config.message, "")
	flags.StringVar(&config.dataDir, "data-dir", "", "")
	flags.BoolVar(&config.topmost, "topmost", true, "")

	if err := flags.Parse(args); err != nil {
		return helperConfig{}, fmt.Errorf("invalid Windows Hello helper arguments: %w", err)
	}

	switch helperMode(mode) {
	case helperModeBound, helperModePlain:
		config.mode = helperMode(mode)
	default:
		return helperConfig{}, fmt.Errorf("unsupported Windows Hello helper mode %q", mode)
	}

	config.message = strings.TrimSpace(config.message)
	if config.message == "" {
		return helperConfig{}, errors.New("Windows Hello helper message cannot be empty")
	}

	return config, nil
}

func runHelper(config helperConfig) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer func() {
		if recovered := recover(); recovered != nil {
			appendDiagnosticLog(
				config.dataDir,
				"hello-helper",
				"helper panic=%v stack=%s",
				recovered,
				string(debug.Stack()),
			)
			err = &helperError{
				kind:    helperErrorFailed,
				message: fmt.Sprintf("Windows Hello helper panicked: %v", recovered),
			}
		}
	}()

	activeHelperDir = config.dataDir
	appendDiagnosticLog(
		config.dataDir,
		"hello-helper",
		"helper start pid=%d tid=%d mode=%s topmost=%t message=%q",
		os.Getpid(),
		win32.GetCurrentThreadId(),
		config.mode,
		config.topmost,
		config.message,
	)

	hInstance, _ := win32.GetModuleHandle(nil)
	if err := registerWindowClass(hInstance); err != nil {
		appendDiagnosticLog(config.dataDir, "hello-helper", "registerWindowClass error=%v", err)
		return err
	}

	hwnd, err := createHelperWindow(hInstance, config.topmost)
	if err != nil {
		appendDiagnosticLog(config.dataDir, "hello-helper", "createHelperWindow error=%v", err)
		return err
	}

	appendDiagnosticLog(
		config.dataDir,
		"hello-helper",
		"window created hwnd=0x%X foreground=0x%X class=%q title=%q",
		uintptr(hwnd),
		uintptr(win32.GetForegroundWindow()),
		windowClassName(hwnd),
		windowTitle(hwnd),
	)

	helperResultChan = make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer func() {
			if recovered := recover(); recovered != nil {
				err := &helperError{
					kind:    helperErrorFailed,
					message: fmt.Sprintf("Windows Hello helper panicked: %v", recovered),
				}
				appendDiagnosticLog(
					config.dataDir,
					"hello-helper",
					"verify panic=%v stack=%s",
					recovered,
					string(debug.Stack()),
				)
				helperResultChan <- err
				_, _ = win32.PostMessageW(hwnd, wmHelperDone, 0, 0)
			}
		}()
		err := verifyWithHello(hwnd, config)
		helperResultChan <- err
		_, _ = win32.PostMessageW(hwnd, wmHelperDone, 0, 0)
	}()

	var msg win32.MSG
	for {
		ok, _ := win32.GetMessageW(&msg, 0, 0, 0)
		if ok == win32.FALSE {
			break
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessageW(&msg)
	}

	select {
	case err := <-helperResultChan:
		appendDiagnosticLog(config.dataDir, "hello-helper", "helper end err=%v", err)
		return err
	case <-time.After(500 * time.Millisecond):
		appendDiagnosticLog(config.dataDir, "hello-helper", "helper end without result")
		return errors.New("Windows Hello helper closed before verification completed")
	}
}

func registerWindowClass(hInstance win32.HMODULE) error {
	className := win32.StrToPwstr(helperClassName)

	var wc win32.WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = syscall.NewCallback(windowProc)
	wc.HInstance = hInstance
	wc.HCursor, _ = win32.LoadCursorW(0, win32.IDC_ARROW)
	wc.HbrBackground = win32.HBRUSH(win32.COLOR_WINDOW + 1)
	wc.LpszClassName = className

	if atom, _ := win32.RegisterClassExW(&wc); atom == 0 {
		return errors.New("RegisterClassExW failed")
	}
	return nil
}

func createHelperWindow(hInstance win32.HMODULE, topmost bool) (win32.HWND, error) {
	x, y := helperWindowPosition()
	exStyle := win32.WINDOW_EX_STYLE(win32.WS_EX_TOOLWINDOW)
	if topmost {
		exStyle |= win32.WS_EX_TOPMOST
	}

	hwnd, _ := win32.CreateWindowExW(
		exStyle,
		win32.StrToPwstr(helperClassName),
		win32.StrToPwstr(helperWindowText),
		win32.WS_CAPTION|win32.WS_SYSMENU|win32.WS_VISIBLE,
		x,
		y,
		helperWindowWidth,
		helperWindowHeight,
		0,
		0,
		hInstance,
		nil,
	)
	if hwnd == 0 {
		return 0, errors.New("CreateWindowExW failed")
	}

	disableCloseButton(hwnd)
	win32.SetWindowTextW(hwnd, win32.StrToPwstr(helperWindowText))

	if topmost {
		_, _ = win32.SetWindowPos(
			hwnd,
			win32.HWND_TOPMOST,
			0,
			0,
			0,
			0,
			win32.SWP_NOMOVE|win32.SWP_NOSIZE|win32.SWP_SHOWWINDOW,
		)
	}
	win32.ShowWindow(hwnd, win32.SW_SHOW)
	win32.BringWindowToTop(hwnd)
	win32.SetForegroundWindow(hwnd)
	win32.SetActiveWindow(hwnd)
	win32.UpdateWindow(hwnd)

	return hwnd, nil
}

func helperWindowPosition() (int32, int32) {
	screenWidth, _ := win32.GetSystemMetrics(win32.SM_CXSCREEN)
	screenHeight, _ := win32.GetSystemMetrics(win32.SM_CYSCREEN)

	x := (screenWidth - helperWindowWidth) / 2
	y := (screenHeight - helperWindowHeight) / 2

	foreground := win32.GetForegroundWindow()
	if foreground == 0 {
		return x, y
	}

	var rect win32.RECT
	if ok, _ := win32.GetWindowRect(foreground, &rect); ok == 0 {
		return x, y
	}

	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	if width > 0 {
		x = rect.Left + (width-helperWindowWidth)/2
	}
	if height > 0 {
		y = rect.Top + (height-helperWindowHeight)/2
	}

	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

func windowProc(hwnd win32.HWND, msg win32.UINT, wParam win32.WPARAM, lParam win32.LPARAM) win32.LRESULT {
	switch msg {
	case win32.WM_PAINT:
		paintHelperWindow(hwnd)
		return 0
	case wmHelperDone:
		appendDiagnosticLog(activeHelperDir, "hello-helper", "wmHelperDone received hwnd=0x%X", uintptr(hwnd))
		win32.DestroyWindow(hwnd)
		return 0
	case win32.WM_CLOSE:
		appendDiagnosticLog(activeHelperDir, "hello-helper", "WM_CLOSE ignored hwnd=0x%X", uintptr(hwnd))
		return 0
	case win32.WM_DESTROY:
		appendDiagnosticLog(activeHelperDir, "hello-helper", "window destroyed hwnd=0x%X", uintptr(hwnd))
		win32.PostQuitMessage(0)
		return 0
	}
	return win32.DefWindowProcW(hwnd, msg, wParam, lParam)
}

func verifyWithHello(hwnd win32.HWND, config helperConfig) error {
	initialized := winrt.InitializeMt()
	defer initialized.Uninitialize()

	appendDiagnosticLog(
		config.dataDir,
		"hello-helper",
		"verify start mode=%s hwnd=0x%X root=0x%X foreground=0x%X tid=%d",
		config.mode,
		uintptr(hwnd),
		uintptr(win32.GetAncestor(hwnd, win32.GA_ROOT)),
		uintptr(win32.GetForegroundWindow()),
		win32.GetCurrentThreadId(),
	)

	verifier := winrt.NewIUserConsentVerifierStatics()
	if verifier == nil {
		return &helperError{
			kind:    helperErrorUnavailable,
			message: "Windows Hello is unavailable",
		}
	}

	availability, err := awaitWinRT(config.dataDir, "CheckAvailabilityAsync", verifier.CheckAvailabilityAsync())
	if err != nil {
		return err
	}
	appendDiagnosticLog(config.dataDir, "hello-helper", "CheckAvailabilityAsync availability=%d", availability)
	if availability != winrt.UserConsentVerifierAvailability_Available {
		return &helperError{
			kind:    helperErrorUnavailable,
			message: fmt.Sprintf("Windows Hello is unavailable: %s", helloAvailabilityMessage(availability)),
		}
	}

	switch config.mode {
	case helperModePlain:
		result, err := awaitWinRT(
			config.dataDir,
			"RequestVerificationAsync",
			verifier.RequestVerificationAsync(config.message),
		)
		if err != nil {
			return err
		}
		appendDiagnosticLog(config.dataDir, "hello-helper", "RequestVerificationAsync result=%d", result)
		return errorForVerificationResult(result)
	case helperModeBound:
		messageHString := winrt.NewHStr(config.message)
		defer messageHString.Dispose()

		className := winrt.NewHStr("Windows.Security.Credentials.UI.UserConsentVerifier")
		defer className.Dispose()

		var interop *win32.IUserConsentVerifierInterop
		hr := win32.RoGetActivationFactory(
			className.Ptr,
			&win32.IID_IUserConsentVerifierInterop,
			unsafe.Pointer(&interop),
		)
		if win32.FAILED(hr) || interop == nil {
			return &helperError{
				kind:    helperErrorFailed,
				message: fmt.Sprintf("Windows Hello activation failed: HRESULT 0x%08X", uint32(hr)),
			}
		}
		defer interop.Release()

		var operation *winrt.IAsyncOperation[winrt.UserConsentVerificationResult]
		hr = interop.RequestVerificationForWindowAsync(
			hwnd,
			messageHString.Ptr,
			&iidAsyncOperationUserConsentVerificationResult,
			unsafe.Pointer(&operation),
		)
		if win32.FAILED(hr) || operation == nil {
			return &helperError{
				kind:    helperErrorFailed,
				message: fmt.Sprintf("Windows Hello bound verification failed: HRESULT 0x%08X", uint32(hr)),
			}
		}
		com.AddToScope(operation)
		appendDiagnosticLog(
			config.dataDir,
			"hello-helper",
			"RequestVerificationForWindowAsync started hwnd=0x%X",
			uintptr(hwnd),
		)

		result, err := awaitWinRT(
			config.dataDir,
			"RequestVerificationForWindowAsync",
			operation,
		)
		if err != nil {
			return err
		}
		appendDiagnosticLog(
			config.dataDir,
			"hello-helper",
			"RequestVerificationForWindowAsync result=%d",
			result,
		)
		return errorForVerificationResult(result)
	default:
		return &helperError{
			kind:    helperErrorFailed,
			message: fmt.Sprintf("unsupported Windows Hello helper mode %q", config.mode),
		}
	}
}

func awaitWinRT[T any](dataDir, name string, operation *winrt.IAsyncOperation[T]) (T, error) {
	var zero T
	if operation == nil {
		appendDiagnosticLog(dataDir, "hello-helper", "%s operation=nil", name)
		return zero, &helperError{
			kind:    helperErrorFailed,
			message: "Windows Hello operation could not be created",
		}
	}

	var asyncInfo *winrt.IAsyncInfo
	hr := operation.QueryInterface(&winrt.IID_IAsyncInfo, unsafe.Pointer(&asyncInfo))
	if win32.FAILED(hr) || asyncInfo == nil {
		appendDiagnosticLog(
			dataDir,
			"hello-helper",
			"%s QueryInterface(IAsyncInfo) failed hr=0x%08X",
			name,
			uint32(hr),
		)
		return zero, &helperError{
			kind:    helperErrorFailed,
			message: fmt.Sprintf("Windows Hello could not query async status: HRESULT 0x%08X", uint32(hr)),
		}
	}
	defer asyncInfo.Release()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		status := asyncInfo.Get_Status()
		switch status {
		case winrt.AsyncStatus_Completed:
			appendDiagnosticLog(dataDir, "hello-helper", "%s status=Completed", name)
			return operation.GetResults(), nil
		case winrt.AsyncStatus_Canceled:
			appendDiagnosticLog(dataDir, "hello-helper", "%s status=Canceled", name)
			return zero, &helperError{
				kind:    helperErrorCancelled,
				message: "Windows Hello verification was cancelled",
			}
		case winrt.AsyncStatus_Error:
			errorCode := uint32(asyncInfo.Get_ErrorCode().Value)
			appendDiagnosticLog(
				dataDir,
				"hello-helper",
				"%s status=Error hresult=0x%08X",
				name,
				errorCode,
			)
			return zero, &helperError{
				kind:    helperErrorFailed,
				message: fmt.Sprintf("Windows Hello verification failed: HRESULT 0x%08X", errorCode),
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	appendDiagnosticLog(dataDir, "hello-helper", "%s status=TimedOut", name)
	return zero, &helperError{
		kind:    helperErrorFailed,
		message: "Windows Hello verification timed out",
	}
}

func errorForVerificationResult(result winrt.UserConsentVerificationResult) error {
	if result == winrt.UserConsentVerificationResult_Verified {
		return nil
	}
	kind := helperErrorFailed
	if result == winrt.UserConsentVerificationResult_Canceled {
		kind = helperErrorCancelled
	}
	return &helperError{
		kind:    kind,
		message: fmt.Sprintf("Windows Hello did not verify the user: %s", helloResultMessage(result)),
	}
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

func exitCodeForError(err error) int {
	var helperErr *helperError
	if errors.As(err, &helperErr) {
		switch helperErr.kind {
		case helperErrorCancelled:
			return exitCodeCancelled
		case helperErrorUnavailable:
			return exitCodeUnavailable
		default:
			return exitCodeFailed
		}
	}
	return exitCodeFailed
}

func defaultDataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "NyaTerminal"), nil
}

func appendDiagnosticLog(dataDir, component, format string, args ...any) {
	if dataDir == "" {
		return
	}
	logPath := filepath.Join(dataDir, "diagnostics.log")
	line := fmt.Sprintf(
		"%s [%s] %s\n",
		time.Now().Format(time.RFC3339Nano),
		component,
		fmt.Sprintf(format, args...),
	)

	logMu.Lock()
	defer logMu.Unlock()

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line)
}

func setDefaultFont(hwnd win32.HWND) {
	font := win32.GetStockObject(win32.DEFAULT_GUI_FONT)
	_, _ = win32.SendMessageW(hwnd, win32.WM_SETFONT, win32.WPARAM(font), 1)
}

func createUIFont(pointSize int32, weight int32) win32.HFONT {
	hdc := win32.GetDC(0)
	if hdc == 0 {
		return 0
	}
	defer win32.ReleaseDC(0, hdc)

	dpiY := win32.GetDeviceCaps(hdc, win32.LOGPIXELSY)
	height := -win32.MulDiv(pointSize, dpiY, 72)
	return win32.CreateFontW(
		height,
		0,
		0,
		0,
		weight,
		0,
		0,
		0,
		uint32(win32.DEFAULT_CHARSET),
		uint32(win32.OUT_DEFAULT_PRECIS),
		uint32(win32.CLIP_DEFAULT_PRECIS),
		uint32(win32.CLEARTYPE_QUALITY),
		uint32(win32.DEFAULT_PITCH)|uint32(win32.FF_DONTCARE),
		win32.StrToPwstr("Microsoft YaHei UI"),
	)
}

func disableCloseButton(hwnd win32.HWND) {
	menu := win32.GetSystemMenu(hwnd, 0)
	if menu == 0 {
		return
	}
	_, _ = win32.DeleteMenu(menu, win32.SC_CLOSE, win32.MF_BYCOMMAND)
	_, _ = win32.DrawMenuBar(hwnd)
}

func paintHelperWindow(hwnd win32.HWND) {
	var ps win32.PAINTSTRUCT
	hdc := win32.BeginPaint(hwnd, &ps)
	if hdc == 0 {
		return
	}
	defer win32.EndPaint(hwnd, &ps)

	var rect win32.RECT
	_, _ = win32.GetClientRect(hwnd, &rect)

	backgroundBrush := win32.CreateSolidBrush(helperBackground)
	panelBrush := win32.CreateSolidBrush(helperPanel)
	accentBrush := win32.CreateSolidBrush(helperAccent)
	borderBrush := win32.CreateSolidBrush(helperBorder)
	labelFont := createUIFont(9, 600)
	titleFont := createUIFont(16, 700)
	bodyFont := createUIFont(10, 400)
	defer win32.DeleteObject(win32.HGDIOBJ(backgroundBrush))
	defer win32.DeleteObject(win32.HGDIOBJ(panelBrush))
	defer win32.DeleteObject(win32.HGDIOBJ(accentBrush))
	defer win32.DeleteObject(win32.HGDIOBJ(borderBrush))
	if labelFont != 0 {
		defer win32.DeleteObject(win32.HGDIOBJ(labelFont))
	}
	if titleFont != 0 {
		defer win32.DeleteObject(win32.HGDIOBJ(titleFont))
	}
	if bodyFont != 0 {
		defer win32.DeleteObject(win32.HGDIOBJ(bodyFont))
	}

	win32.FillRect(hdc, &rect, backgroundBrush)

	panelRect := win32.RECT{Left: 16, Top: 18, Right: rect.Right - 16, Bottom: rect.Bottom - 18}
	win32.FillRect(hdc, &panelRect, panelBrush)

	accentRect := win32.RECT{Left: panelRect.Left, Top: panelRect.Top, Right: panelRect.Left + 5, Bottom: panelRect.Bottom}
	win32.FillRect(hdc, &accentRect, accentBrush)

	topBorder := win32.RECT{Left: panelRect.Left, Top: panelRect.Top, Right: panelRect.Right, Bottom: panelRect.Top + 1}
	bottomBorder := win32.RECT{Left: panelRect.Left, Top: panelRect.Bottom - 1, Right: panelRect.Right, Bottom: panelRect.Bottom}
	leftBorder := win32.RECT{Left: panelRect.Left, Top: panelRect.Top, Right: panelRect.Left + 1, Bottom: panelRect.Bottom}
	rightBorder := win32.RECT{Left: panelRect.Right - 1, Top: panelRect.Top, Right: panelRect.Right, Bottom: panelRect.Bottom}
	win32.FillRect(hdc, &topBorder, borderBrush)
	win32.FillRect(hdc, &bottomBorder, borderBrush)
	win32.FillRect(hdc, &leftBorder, borderBrush)
	win32.FillRect(hdc, &rightBorder, borderBrush)

	win32.SetBkMode(hdc, win32.TRANSPARENT)

	contentLeft := panelRect.Left + 24
	contentRight := panelRect.Right - 24
	labelRect := win32.RECT{Left: contentLeft, Top: panelRect.Top + 18, Right: contentRight, Bottom: panelRect.Top + 36}
	titleRect := win32.RECT{Left: contentLeft, Top: panelRect.Top + 42, Right: contentRight, Bottom: panelRect.Top + 78}
	bodyRect := win32.RECT{Left: contentLeft, Top: panelRect.Top + 86, Right: contentRight, Bottom: panelRect.Bottom - 20}

	if labelFont != 0 {
		oldFont := win32.SelectObject(hdc, win32.HGDIOBJ(labelFont))
		win32.SetTextColor(hdc, helperAccent)
		win32.DrawTextW(
			hdc,
			win32.StrToPwstr("Windows Hello"),
			-1,
			&labelRect,
			win32.DT_LEFT|win32.DT_TOP|win32.DT_SINGLELINE,
		)
		if oldFont != 0 {
			win32.SelectObject(hdc, oldFont)
		}
	}

	if titleFont != 0 {
		oldFont := win32.SelectObject(hdc, win32.HGDIOBJ(titleFont))
		win32.SetTextColor(hdc, helperText)
		win32.DrawTextW(
			hdc,
			win32.StrToPwstr("请完成安全验证"),
			-1,
			&titleRect,
			win32.DT_LEFT|win32.DT_TOP|win32.DT_SINGLELINE,
		)
		if oldFont != 0 {
			win32.SelectObject(hdc, oldFont)
		}
	}

	if bodyFont != 0 {
		oldFont := win32.SelectObject(hdc, win32.HGDIOBJ(bodyFont))
		win32.SetTextColor(hdc, helperMutedText)
		win32.DrawTextW(
			hdc,
			win32.StrToPwstr("请在弹出的 Windows Hello 窗口中使用 PIN、指纹或人脸完成验证。\n\n验证完成后会自动返回 NyaTerminal。"),
			-1,
			&bodyRect,
			win32.DT_LEFT|win32.DT_TOP|win32.DT_WORDBREAK,
		)
		if oldFont != 0 {
			win32.SelectObject(hdc, oldFont)
		}
	}
}

func windowClassName(hwnd win32.HWND) string {
	if hwnd == 0 {
		return ""
	}
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
	if hwnd == 0 {
		return ""
	}
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
