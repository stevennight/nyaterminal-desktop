# NyaTerminal desktop

## Development requirements

- 64-bit Go 1.24+
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
