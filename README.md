# NyaTerminal desktop

## Development requirements

- 64-bit Go 1.25+（推荐使用 Go 1.26）
- Node.js 22+
- Wails 2.10+
- Windows WebView2, macOS WebKit, or Linux GTK/WebKit dependencies

The current Windows development machine must use an AMD64 or ARM64 Go toolchain.
A `windows/386` toolchain can run package tests but cannot produce a supported
Wails release.

```powershell
cd frontend
npm install
npm run build
cd ..
wails dev
```

The frontend never receives stored credentials directly. SSH and SFTP operations
resolve encrypted credentials inside the Go process.

The Windows agent integration supports both the native OpenSSH agent and
Pageant. Keyboard-interactive authentication is presented as a live challenge;
one-time codes are never saved.

## Security behavior

- Windows quick unlock is gated by Windows Hello user verification.
- macOS CGO builds use LocalAuthentication; Linux uses the current Secret
  Service session and falls back to the master password when unavailable.
- The vault database is encrypted record-by-record with XChaCha20-Poly1305 and
  the Windows data directory receives a protected user/SYSTEM/Administrators ACL.
- SFTP uses a bounded backend queue with progress, pause, cancellation and
  resumable `.nyapart` files. ZMODEM receive data is streamed to a system-picked
  destination instead of accumulating the complete file in WebView memory.
- Terminal links only open explicit HTTP/HTTPS URLs through the system browser.

## Verification

```powershell
go test ./...
go vet ./...
cd frontend
npm test
npm audit --audit-level=high
npm run build
```
