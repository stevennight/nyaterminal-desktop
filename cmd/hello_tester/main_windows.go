//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	appClassName = "NyaHelloTesterWindow"
	wmAppendLog  = 0x8001

	idLogEdit           = 100
	idPlainHello        = 101
	idBoundHello        = 102
	idBoundBgHello      = 103
	idTopmostCheckbox   = 104
	idHideThenHello     = 105
	idMinimizeThenHello = 106
)

var (
	mainWindow win32.HWND
	logEdit    win32.HWND
	topmostBox win32.HWND
	logPath    string
	uiThreadID uint32

	logFileMu     sync.Mutex
	pendingLogsMu sync.Mutex
	pendingLogs   []string

	iidAsyncOperationUserConsentVerificationResult = syscall.GUID{0xFD596FFD, 0x2318, 0x558F,
		[8]byte{0x9D, 0xBE, 0xD2, 0x1D, 0xF4, 0x37, 0x64, 0xA5}}
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	dir, err := os.UserConfigDir()
	if err != nil {
		panic(err)
	}
	logDir := filepath.Join(dir, "NyaTerminal")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		panic(err)
	}
	logPath = filepath.Join(logDir, "hello_tester.log")
	_ = os.Remove(logPath)
	uiThreadID = win32.GetCurrentThreadId()

	hInstance, _ := win32.GetModuleHandle(nil)
	if err := createMainWindow(hInstance); err != nil {
		panic(err)
	}

	appendLog("hello tester started pid=%d tid=%d log=%s", win32.GetCurrentProcessId(), win32.GetCurrentThreadId(), logPath)

	var msg win32.MSG
	for {
		ok, _ := win32.GetMessageW(&msg, 0, 0, 0)
		if ok == win32.FALSE {
			break
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessageW(&msg)
	}
}

func createMainWindow(hInstance win32.HMODULE) error {
	className := win32.StrToPwstr(appClassName)

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

	hwnd, _ := win32.CreateWindowExW(
		0,
		className,
		win32.StrToPwstr("Windows Hello Tester"),
		win32.WS_OVERLAPPEDWINDOW|win32.WS_VISIBLE,
		win32.CW_USEDEFAULT,
		win32.CW_USEDEFAULT,
		920,
		720,
		0,
		0,
		hInstance,
		nil,
	)
	if hwnd == 0 {
		return errors.New("CreateWindowExW failed")
	}
	mainWindow = hwnd
	win32.ShowWindow(hwnd, win32.SW_SHOW)
	win32.UpdateWindow(hwnd)
	return nil
}

func windowProc(hwnd win32.HWND, msg win32.UINT, wParam win32.WPARAM, lParam win32.LPARAM) win32.LRESULT {
	switch msg {
	case win32.WM_CREATE:
		setupControls(hwnd)
		return 0
	case wmAppendLog:
		flushPendingLogs()
		return 0
	case win32.WM_COMMAND:
		onCommand(hwnd, wParam)
		return 0
	case win32.WM_DESTROY:
		appendLog("window destroyed")
		win32.PostQuitMessage(0)
		return 0
	}
	return win32.DefWindowProcW(hwnd, msg, wParam, lParam)
}

func setupControls(hwnd win32.HWND) {
	hInstance, _ := win32.GetModuleHandle(nil)
	buttonStyle := win32.WS_CHILD | win32.WS_VISIBLE | win32.WS_TABSTOP

	createButton := func(id int, text string, x int32, y int32, width int32) {
		control, _ := win32.CreateWindowExW(
			0,
			win32.StrToPwstr("Button"),
			win32.StrToPwstr(text),
			buttonStyle|win32.WINDOW_STYLE(win32.BS_PUSHBUTTON),
			x,
			y,
			width,
			30,
			hwnd,
			win32.HMENU(id),
			hInstance,
			nil,
		)
		setDefaultFont(control)
	}

	createCheck := func(id int, text string, x int32, y int32, width int32) win32.HWND {
		control, _ := win32.CreateWindowExW(
			0,
			win32.StrToPwstr("Button"),
			win32.StrToPwstr(text),
			buttonStyle|win32.WINDOW_STYLE(win32.BS_AUTOCHECKBOX),
			x,
			y,
			width,
			24,
			hwnd,
			win32.HMENU(id),
			hInstance,
			nil,
		)
		setDefaultFont(control)
		return control
	}

	createButton(idPlainHello, "Plain Hello", 16, 16, 120)
	createButton(idBoundHello, "Bound Hello (UI)", 148, 16, 160)
	createButton(idBoundBgHello, "Bound Hello (BG)", 320, 16, 160)
	createButton(idHideThenHello, "Hide Then Plain", 492, 16, 140)
	createButton(idMinimizeThenHello, "Minimize Then Plain", 644, 16, 180)
	topmostBox = createCheck(idTopmostCheckbox, "Always On Top", 16, 56, 160)

	logEdit, _ = win32.CreateWindowExW(
		win32.WS_EX_STATICEDGE,
		win32.StrToPwstr("Edit"),
		nil,
		win32.WS_CHILD|win32.WS_VISIBLE|win32.WS_VSCROLL|win32.WS_BORDER|
			win32.WINDOW_STYLE(win32.ES_MULTILINE|win32.ES_AUTOVSCROLL|win32.ES_READONLY),
		16,
		96,
		872,
		580,
		hwnd,
		win32.HMENU(idLogEdit),
		hInstance,
		nil,
	)
	setDefaultFont(logEdit)

	appendLog(
		"window created hwnd=0x%X class=%q title=%q tid=%d",
		uintptr(hwnd),
		windowClassName(hwnd),
		windowTitle(hwnd),
		win32.GetCurrentThreadId(),
	)
}

func setDefaultFont(hwnd win32.HWND) {
	font := win32.GetStockObject(win32.DEFAULT_GUI_FONT)
	_, _ = win32.SendMessageW(hwnd, win32.WM_SETFONT, win32.WPARAM(font), 1)
}

func onCommand(hwnd win32.HWND, wParam win32.WPARAM) {
	controlID := int(win32.LOWORD(uint32(wParam)))
	notifyCode := win32.HIWORD(uint32(wParam))
	if notifyCode != uint16(win32.BN_CLICKED) {
		return
	}

	switch controlID {
	case idPlainHello:
		go runAsync("plain-hello", func() error {
			return verifyWithHello(0, false, "Plain Hello Test")
		})
	case idBoundHello:
		runSync("bound-hello-ui", func() error {
			return verifyWithHello(hwnd, true, "Bound Hello Test")
		})
	case idBoundBgHello:
		go runAsync("bound-hello-bg", func() error {
			return verifyWithHello(hwnd, true, "Bound Hello Background Thread Test")
		})
	case idHideThenHello:
		appendLog("action=hide-then-plain begin")
		win32.ShowWindow(hwnd, win32.SW_HIDE)
		go runAsync("hide-then-plain", func() error {
			err := verifyWithHello(0, false, "Hide Then Plain Hello Test")
			win32.ShowWindowAsync(hwnd, win32.SW_SHOW)
			return err
		})
	case idMinimizeThenHello:
		appendLog("action=minimize-then-plain begin")
		win32.ShowWindow(hwnd, win32.SW_MINIMIZE)
		go runAsync("minimize-then-plain", func() error {
			err := verifyWithHello(0, false, "Minimize Then Plain Hello Test")
			win32.ShowWindowAsync(hwnd, win32.SW_SHOW)
			return err
		})
	case idTopmostCheckbox:
		state, _ := win32.SendMessageW(topmostBox, win32.BM_GETCHECK, 0, 0)
		topmost := uint32(state) == uint32(win32.BST_CHECKED)
		insertAfter := win32.HWND_NOTOPMOST
		if topmost {
			insertAfter = win32.HWND_TOPMOST
		}
		_, _ = win32.SetWindowPos(
			hwnd,
			insertAfter,
			0,
			0,
			0,
			0,
			win32.SWP_NOMOVE|win32.SWP_NOSIZE|win32.SWP_NOACTIVATE|win32.SWP_SHOWWINDOW,
		)
		appendLog("action=toggle-topmost enabled=%t", topmost)
	}
}

func runAsync(name string, fn func() error) {
	appendLog("task=%s start tid=%d foreground=0x%X", name, win32.GetCurrentThreadId(), uintptr(win32.GetForegroundWindow()))
	defer func() {
		if recovered := recover(); recovered != nil {
			appendLog("task=%s panic=%v", name, recovered)
		}
	}()
	start := time.Now()
	err := fn()
	appendLog("task=%s end duration=%s err=%v foreground=0x%X", name, time.Since(start), err, uintptr(win32.GetForegroundWindow()))
}

func runSync(name string, fn func() error) {
	appendLog("task=%s start tid=%d foreground=0x%X", name, win32.GetCurrentThreadId(), uintptr(win32.GetForegroundWindow()))
	defer func() {
		if recovered := recover(); recovered != nil {
			appendLog("task=%s panic=%v", name, recovered)
		}
	}()
	start := time.Now()
	err := fn()
	appendLog("task=%s end duration=%s err=%v foreground=0x%X", name, time.Since(start), err, uintptr(win32.GetForegroundWindow()))
}

func verifyWithHello(hwnd win32.HWND, bound bool, message string) error {
	initialized := winrt.InitializeMt()
	defer initialized.Uninitialize()

	appendLog(
		"verify start bound=%t hwnd=0x%X root=0x%X class=%q title=%q tid=%d",
		bound,
		uintptr(hwnd),
		uintptr(win32.GetAncestor(hwnd, win32.GA_ROOT)),
		windowClassName(hwnd),
		windowTitle(hwnd),
		win32.GetCurrentThreadId(),
	)

	verifier := winrt.NewIUserConsentVerifierStatics()
	if verifier == nil {
		return errors.New("verifier unavailable")
	}
	availability, err := awaitWinRTHello("CheckAvailabilityAsync", verifier.CheckAvailabilityAsync())
	if err != nil {
		return err
	}
	appendLog("CheckAvailabilityAsync availability=%d", availability)

	if availability != winrt.UserConsentVerifierAvailability_Available {
		return fmt.Errorf("availability=%d", availability)
	}

	if !bound {
		result, err := awaitWinRTHello("RequestVerificationAsync", verifier.RequestVerificationAsync(message))
		if err != nil {
			return err
		}
		appendLog("RequestVerificationAsync result=%d", result)
		if result != winrt.UserConsentVerificationResult_Verified {
			return fmt.Errorf("result=%d", result)
		}
		return nil
	}

	messageHString := winrt.NewHStr(message)
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
		return fmt.Errorf("RoGetActivationFactory failed hr=0x%08X", uint32(hr))
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
		return fmt.Errorf("RequestVerificationForWindowAsync failed hr=0x%08X", uint32(hr))
	}
	com.AddToScope(operation)
	appendLog("RequestVerificationForWindowAsync started hwnd=0x%X", uintptr(hwnd))

	result, err := awaitWinRTHello("RequestVerificationForWindowAsync", operation)
	if err != nil {
		return err
	}
	appendLog("RequestVerificationForWindowAsync result=%d", result)
	if result != winrt.UserConsentVerificationResult_Verified {
		return fmt.Errorf("result=%d", result)
	}
	return nil
}

func awaitWinRTHello[T any](name string, operation *winrt.IAsyncOperation[T]) (T, error) {
	var zero T
	if operation == nil {
		appendLog("%s operation=nil", name)
		return zero, errors.New("operation=nil")
	}
	var asyncInfo *winrt.IAsyncInfo
	hr := operation.QueryInterface(&winrt.IID_IAsyncInfo, unsafe.Pointer(&asyncInfo))
	if win32.FAILED(hr) || asyncInfo == nil {
		appendLog("%s QueryInterface(IAsyncInfo) failed hr=0x%08X", name, uint32(hr))
		return zero, fmt.Errorf("QueryInterface hr=0x%08X", uint32(hr))
	}
	defer asyncInfo.Release()

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		status := asyncInfo.Get_Status()
		switch status {
		case winrt.AsyncStatus_Completed:
			appendLog("%s status=Completed", name)
			return operation.GetResults(), nil
		case winrt.AsyncStatus_Canceled:
			appendLog("%s status=Canceled", name)
			return zero, errors.New("canceled")
		case winrt.AsyncStatus_Error:
			appendLog("%s status=Error hresult=0x%08X", name, uint32(asyncInfo.Get_ErrorCode().Value))
			return zero, fmt.Errorf("async error=0x%08X", uint32(asyncInfo.Get_ErrorCode().Value))
		}
		time.Sleep(50 * time.Millisecond)
	}
	appendLog("%s status=TimedOut", name)
	return zero, errors.New("timed out")
}

func appendLog(format string, args ...any) {
	line := fmt.Sprintf(
		"%s %s",
		time.Now().Format(time.RFC3339Nano),
		fmt.Sprintf(format, args...),
	)

	logFileMu.Lock()
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		_, _ = file.WriteString(line + "\n")
		_ = file.Close()
	}
	logFileMu.Unlock()

	if logEdit == 0 {
		return
	}
	if win32.GetCurrentThreadId() == uiThreadID {
		appendLineToEdit(line)
		return
	}
	pendingLogsMu.Lock()
	pendingLogs = append(pendingLogs, line)
	pendingLogsMu.Unlock()
	if mainWindow != 0 {
		_, _ = win32.PostMessageW(mainWindow, wmAppendLog, 0, 0)
	}
}

func flushPendingLogs() {
	pendingLogsMu.Lock()
	lines := append([]string(nil), pendingLogs...)
	pendingLogs = nil
	pendingLogsMu.Unlock()
	for _, line := range lines {
		appendLineToEdit(line)
	}
}

func appendLineToEdit(line string) {
	if logEdit == 0 {
		return
	}
	text := strings.ReplaceAll(line, "\n", " ")
	utf16, err := syscall.UTF16FromString(text + "\r\n")
	if err != nil {
		return
	}
	length, _ := win32.GetWindowTextLengthW(logEdit)
	_, _ = win32.SendMessageW(logEdit, win32.EM_SETSEL, win32.WPARAM(length), win32.WPARAM(length))
	_, _ = win32.SendMessageW(
		logEdit,
		win32.EM_REPLACESEL,
		1,
		win32.LPARAM(uintptr(unsafe.Pointer(&utf16[0]))),
	)
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
