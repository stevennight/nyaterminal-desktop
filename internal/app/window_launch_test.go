package app

import (
	"path/filepath"
	"testing"
)

func TestMacOSAppBundlePath(t *testing.T) {
	tests := []struct {
		name       string
		executable string
		wantPath   string
		wantOK     bool
	}{
		{
			name:       "standard app bundle",
			executable: filepath.FromSlash("/Applications/NyaTerminal.app/Contents/MacOS/NyaTerminal"),
			wantPath:   filepath.FromSlash("/Applications/NyaTerminal.app"),
			wantOK:     true,
		},
		{
			name:       "standalone binary",
			executable: filepath.FromSlash("/usr/local/bin/NyaTerminal"),
			wantPath:   "",
			wantOK:     false,
		},
		{
			name:       "non-bundle contents layout",
			executable: filepath.FromSlash("/tmp/NyaTerminal/Contents/MacOS/NyaTerminal"),
			wantPath:   "",
			wantOK:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotPath, gotOK := macOSAppBundlePath(test.executable)
			if gotOK != test.wantOK || gotPath != test.wantPath {
				t.Fatalf("macOSAppBundlePath(%q) = (%q, %v), want (%q, %v)", test.executable, gotPath, gotOK, test.wantPath, test.wantOK)
			}
		})
	}
}
