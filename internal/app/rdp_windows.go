//go:build windows

package app

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/nyaterminal/nyaterminal-desktop/internal/model"
	"golang.org/x/sys/windows"
)

var (
	crypt32DLL           = windows.NewLazySystemDLL("crypt32.dll")
	kernel32DLL          = windows.NewLazySystemDLL("kernel32.dll")
	procCryptProtectData = crypt32DLL.NewProc("CryptProtectData")
	procLocalFree        = kernel32DLL.NewProc("LocalFree")
)

const cryptProtectUIForbidden = 0x1

type dpapiBlob struct {
	cbData uint32
	pbData *byte
}

// dpapiProtect encrypts data with the current user's DPAPI key. The result is
// what mstsc expects in an .rdp "password 51:b:" field.
func dpapiProtect(data []byte) ([]byte, error) {
	var in dpapiBlob
	if len(data) > 0 {
		in.cbData = uint32(len(data))
		in.pbData = &data[0]
	}
	var out dpapiBlob
	ret, _, callErr := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		uintptr(cryptProtectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return nil, callErr
		}
		return nil, errors.New("CryptProtectData failed")
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	result := make([]byte, out.cbData)
	copy(result, unsafe.Slice(out.pbData, out.cbData))
	return result, nil
}

// utf16LEBytes returns the little-endian UTF-16 encoding of s without a BOM or
// trailing NUL, matching what mstsc protects for the password field.
func utf16LEBytes(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		out[i*2] = byte(u)
		out[i*2+1] = byte(u >> 8)
	}
	return out
}

func (a *App) launchRDP(conn model.Connection, cred model.Credential) error {
	mstsc, err := exec.LookPath("mstsc.exe")
	if err != nil {
		return errors.New("the Windows Remote Desktop client (mstsc.exe) was not found")
	}

	address := strings.TrimSpace(conn.Host)
	if address == "" {
		return errors.New("the connection has no host")
	}
	if conn.Port > 0 && conn.Port != 3389 {
		address = fmt.Sprintf("%s:%d", address, conn.Port)
	}

	var builder strings.Builder
	line := func(format string, args ...any) {
		fmt.Fprintf(&builder, format+"\r\n", args...)
	}

	line("full address:s:%s", address)
	if user := strings.TrimSpace(conn.Username); user != "" {
		line("username:s:%s", user)
	}
	if strings.TrimSpace(cred.Password) != "" {
		blob, protectErr := dpapiProtect(utf16LEBytes(cred.Password))
		if protectErr != nil {
			return fmt.Errorf("could not protect the saved password: %w", protectErr)
		}
		line("password 51:b:%s", hex.EncodeToString(blob))
		line("prompt for credentials:i:0")
	} else {
		line("prompt for credentials:i:1")
	}

	if conn.RDPScreenMode == "windowed" {
		line("screen mode id:i:1")
	} else {
		line("screen mode id:i:2")
	}
	if conn.RDPWidth > 0 && conn.RDPHeight > 0 {
		line("desktopwidth:i:%d", conn.RDPWidth)
		line("desktopheight:i:%d", conn.RDPHeight)
	}
	if conn.RDPMultiMonitor {
		line("use multimon:i:1")
	} else {
		line("use multimon:i:0")
	}
	if conn.RDPRedirectClipboard == nil || *conn.RDPRedirectClipboard {
		line("redirectclipboard:i:1")
	} else {
		line("redirectclipboard:i:0")
	}
	if conn.RDPRedirectDrives {
		line("drivestoredirect:s:*")
	}
	if gateway := strings.TrimSpace(conn.RDPGateway); gateway != "" {
		line("gatewayhostname:s:%s", gateway)
		line("gatewayusagemethod:i:1")
		line("gatewaycredentialssource:i:4")
		line("gatewayprofileusagemethod:i:1")
	}

	dir := filepath.Join(os.TempDir(), "nyaterminal-rdp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "session-"+strconv.FormatInt(time.Now().UnixNano(), 36)+".rdp")
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		return err
	}

	args := []string{path}
	if conn.RDPAdminSession {
		args = append(args, "/admin")
	}
	cmd := exec.Command(mstsc, args...)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(path)
		return err
	}
	// mstsc reads the file at startup; drop it once the client exits, with a
	// short fallback so the DPAPI-wrapped password never lingers on disk.
	go func() {
		_ = cmd.Wait()
		_ = os.Remove(path)
	}()
	time.AfterFunc(60*time.Second, func() { _ = os.Remove(path) })
	return nil
}
