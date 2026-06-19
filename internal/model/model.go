package model

import "time"

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

type Settings struct {
	Theme                 string   `json:"theme"`
	FontFamily            string   `json:"fontFamily"`
	FontSize              int      `json:"fontSize"`
	LockAfterMinutes      int      `json:"lockAfterMinutes"`
	DisconnectOnLock      bool     `json:"disconnectOnLock"`
	SyncCommandHistory    bool     `json:"syncCommandHistory"`
	SyncSecretsByDefault  bool     `json:"syncSecretsByDefault"`
	SensitiveCommandRules []string `json:"sensitiveCommandRules"`
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
