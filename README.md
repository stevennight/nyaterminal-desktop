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

Or use the helper script from `desktop/`:

```powershell
.\build.ps1
```

Release builds inject independent desktop version metadata:

```powershell
.\build.ps1 -Version v0.1.0 -Commit <git-sha> -BuildDate 2026-07-04T00:00:00Z -UpdateRepository owner/NyaTerminal
```

For a dev loop:

```powershell
.\build.ps1 -Mode dev
```

The frontend never receives stored credentials directly. SSH and SFTP operations
resolve encrypted credentials inside the Go process.

The Windows agent integration supports both the native OpenSSH agent and
Pageant. Keyboard-interactive authentication is presented as a live challenge;
one-time codes are never saved.

The terminal prefers WebGL rendering and falls back to a 2D canvas renderer
before using xterm's default DOM renderer.

## Updates

The desktop client checks GitHub Releases from the configured update repository.
It considers stable `v*` tags, reminds the user when a new desktop release is
available, and does not download or install updates automatically.

Release a desktop version by pushing a tag such as `v0.1.0`. The release
workflow builds the Windows client and uploads it to the GitHub Release.

SSH port forwarding/tunnels are intentionally not exposed in the first release.
The Go SSH session layer contains a reserved port-forwarding interface so a
future implementation can be added without changing connection storage or UI
contracts.

## Security behavior

- Windows quick unlock is gated by Windows Hello user verification.
- macOS CGO builds use LocalAuthentication; Linux uses the current Secret
  Service session and falls back to the master password when unavailable.
- Default SSH negotiation is pinned to `ssh.SupportedAlgorithms()`; weak legacy
  algorithms are only appended per connection after an explicit risk prompt.
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
