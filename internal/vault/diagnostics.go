package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var diagnosticsMu sync.Mutex

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

	diagnosticsMu.Lock()
	defer diagnosticsMu.Unlock()

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		return
	}
	_ = file.Close()
}

func AppendDiagnosticLogForApp(dataDir, format string, args ...any) {
	appendDiagnosticLog(dataDir, "app", format, args...)
}
