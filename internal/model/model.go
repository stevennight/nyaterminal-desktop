package model

import (
	"regexp"
	"strings"
	"time"
)

type Group struct {
	ID        string    `json:"id"`
	ParentID  string    `json:"parentId,omitempty"`
	Name      string    `json:"name"`
	SortOrder int       `json:"sortOrder"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Tag struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Credential struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Username      string    `json:"username,omitempty"`
	Password      string    `json:"password,omitempty"`
	PrivateKeyPEM string    `json:"privateKeyPem,omitempty"`
	Passphrase    string    `json:"passphrase,omitempty"`
	SyncOverride  *bool     `json:"syncOverride,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type Connection struct {
	ID                string    `json:"id"`
	GroupID           string    `json:"groupId,omitempty"`
	Name              string    `json:"name"`
	Remark            string    `json:"remark,omitempty"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	Username          string    `json:"username"`
	CredentialID      string    `json:"credentialId,omitempty"`
	Authentication    string    `json:"authentication"`
	Tags              []string  `json:"tags"`
	SortOrder         int       `json:"sortOrder"`
	Encoding          string    `json:"encoding"`
	KeepAliveSeconds  int       `json:"keepAliveSeconds"`
	ConnectTimeoutSec int       `json:"connectTimeoutSeconds"`
	LegacyAlgorithms  bool      `json:"legacyAlgorithms"`
	SyncSecrets       *bool     `json:"syncSecrets,omitempty"`
	CommandHistory    bool      `json:"commandHistory"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type CommandHistory struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connectionId"`
	Command      string    `json:"command"`
	UseCount     int       `json:"useCount"`
	LastUsedAt   time.Time `json:"lastUsedAt"`
}

type TerminalThemeColors struct {
	Background          string `json:"background"`
	Foreground          string `json:"foreground"`
	Cursor              string `json:"cursor"`
	CursorAccent        string `json:"cursorAccent"`
	SelectionBackground string `json:"selectionBackground"`
	SelectionForeground string `json:"selectionForeground"`
	Black               string `json:"black"`
	Red                 string `json:"red"`
	Green               string `json:"green"`
	Yellow              string `json:"yellow"`
	Blue                string `json:"blue"`
	Magenta             string `json:"magenta"`
	Cyan                string `json:"cyan"`
	White               string `json:"white"`
	BrightBlack         string `json:"brightBlack"`
	BrightRed           string `json:"brightRed"`
	BrightGreen         string `json:"brightGreen"`
	BrightYellow        string `json:"brightYellow"`
	BrightBlue          string `json:"brightBlue"`
	BrightMagenta       string `json:"brightMagenta"`
	BrightCyan          string `json:"brightCyan"`
	BrightWhite         string `json:"brightWhite"`
}

type Settings struct {
	Theme                 string              `json:"theme"`
	FontFamily            string              `json:"fontFamily"`
	FontSize              int                 `json:"fontSize"`
	TerminalThemePreset   string              `json:"terminalThemePreset"`
	TerminalThemeColors   TerminalThemeColors `json:"terminalThemeColors"`
	LockAfterMinutes      int                 `json:"lockAfterMinutes"`
	DisconnectOnLock      bool                `json:"disconnectOnLock"`
	SyncCommandHistory    bool                `json:"syncCommandHistory"`
	SyncSecretsByDefault  bool                `json:"syncSecretsByDefault"`
	SensitiveCommandRules []string            `json:"sensitiveCommandRules"`
}

type RecordEnvelope struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	UpdatedAt time.Time `json:"updatedAt"`
	Data      []byte    `json:"data"`
}

func DefaultSettings() Settings {
	return Settings{
		Theme:                "dark",
		FontFamily:           "Cascadia Mono, JetBrains Mono, monospace",
		FontSize:             14,
		TerminalThemePreset:  "default",
		TerminalThemeColors:  DefaultTerminalThemeColors(),
		LockAfterMinutes:     15,
		DisconnectOnLock:     false,
		SyncCommandHistory:   false,
		SyncSecretsByDefault: false,
		SensitiveCommandRules: []string{
			`(?i)(password|passwd|token|secret|api[_-]?key)\s*[=:]\s*\S+`,
			`(?i)authorization:\s*bearer\s+\S+`,
		},
	}
}

func DefaultTerminalThemeColors() TerminalThemeColors {
	return TerminalThemeColors{
		Background:          "#0A0E16",
		Foreground:          "#DCE3EE",
		Cursor:              "#77E4D4",
		CursorAccent:        "#0A0E16",
		SelectionBackground: "#325A6A",
		SelectionForeground: "#DCE3EE",
		Black:               "#111827",
		Red:                 "#F7768E",
		Green:               "#70D6A1",
		Yellow:              "#F2C94C",
		Blue:                "#6CB6FF",
		Magenta:             "#C792EA",
		Cyan:                "#72D7E6",
		White:               "#DCE3EE",
		BrightBlack:         "#68788E",
		BrightRed:           "#FF8A9A",
		BrightGreen:         "#8DE5B8",
		BrightYellow:        "#FFD479",
		BrightBlue:          "#8CC5FF",
		BrightMagenta:       "#D9B8FF",
		BrightCyan:          "#9AEAF2",
		BrightWhite:         "#F5F7FA",
	}
}

func NormalizeSettings(value Settings) Settings {
	defaults := DefaultSettings()
	if strings.TrimSpace(value.Theme) == "" {
		value.Theme = defaults.Theme
	}
	if strings.TrimSpace(value.FontFamily) == "" {
		value.FontFamily = defaults.FontFamily
	}
	if strings.TrimSpace(value.TerminalThemePreset) == "" {
		value.TerminalThemePreset = defaults.TerminalThemePreset
	}
	value.TerminalThemeColors = normalizeTerminalThemeColors(value.TerminalThemeColors, defaults.TerminalThemeColors)
	return value
}

func normalizeTerminalThemeColors(value, defaults TerminalThemeColors) TerminalThemeColors {
	if !validHexColor(value.Background) {
		value.Background = defaults.Background
	}
	if !validHexColor(value.Foreground) {
		value.Foreground = defaults.Foreground
	}
	if !validHexColor(value.Cursor) {
		value.Cursor = defaults.Cursor
	}
	if !validHexColor(value.CursorAccent) {
		value.CursorAccent = defaults.CursorAccent
	}
	if !validHexColor(value.SelectionBackground) {
		value.SelectionBackground = defaults.SelectionBackground
	}
	if !validHexColor(value.SelectionForeground) {
		value.SelectionForeground = defaults.SelectionForeground
	}
	if !validHexColor(value.Black) {
		value.Black = defaults.Black
	}
	if !validHexColor(value.Red) {
		value.Red = defaults.Red
	}
	if !validHexColor(value.Green) {
		value.Green = defaults.Green
	}
	if !validHexColor(value.Yellow) {
		value.Yellow = defaults.Yellow
	}
	if !validHexColor(value.Blue) {
		value.Blue = defaults.Blue
	}
	if !validHexColor(value.Magenta) {
		value.Magenta = defaults.Magenta
	}
	if !validHexColor(value.Cyan) {
		value.Cyan = defaults.Cyan
	}
	if !validHexColor(value.White) {
		value.White = defaults.White
	}
	if !validHexColor(value.BrightBlack) {
		value.BrightBlack = defaults.BrightBlack
	}
	if !validHexColor(value.BrightRed) {
		value.BrightRed = defaults.BrightRed
	}
	if !validHexColor(value.BrightGreen) {
		value.BrightGreen = defaults.BrightGreen
	}
	if !validHexColor(value.BrightYellow) {
		value.BrightYellow = defaults.BrightYellow
	}
	if !validHexColor(value.BrightBlue) {
		value.BrightBlue = defaults.BrightBlue
	}
	if !validHexColor(value.BrightMagenta) {
		value.BrightMagenta = defaults.BrightMagenta
	}
	if !validHexColor(value.BrightCyan) {
		value.BrightCyan = defaults.BrightCyan
	}
	if !validHexColor(value.BrightWhite) {
		value.BrightWhite = defaults.BrightWhite
	}
	return value
}

var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func validHexColor(value string) bool {
	return hexColorPattern.MatchString(strings.TrimSpace(value))
}
