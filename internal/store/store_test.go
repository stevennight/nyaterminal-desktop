package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/nyaterminal/nyaterminal/desktop/internal/model"
	"github.com/nyaterminal/nyaterminal/desktop/internal/vault"
)

func TestDeleteCreatesEncryptedSynchronizationTombstone(t *testing.T) {
	ctx := context.Background()
	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVaultOnCleanup(t, v)
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	s := New(v)
	connection, err := s.PutConnection(ctx, model.Connection{
		Name: "server", Host: "example.test", Port: 22, Username: "root",
		Authentication: "agent", Encoding: "utf-8", CommandHistory: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, connection.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetConnection(ctx, connection.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted connection remains readable: %v", err)
	}
	deletions, err := s.ListDeletions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(deletions) != 1 ||
		deletions[0].EntityID != connection.ID ||
		deletions[0].EntityType != TypeConnection {
		t.Fatalf("unexpected deletion journal: %#v", deletions)
	}
}

func TestCommandSuggestionsIncludeGlobalHistory(t *testing.T) {
	ctx := context.Background()
	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVaultOnCleanup(t, v)
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	s := New(v)
	connection, err := s.PutConnection(ctx, model.Connection{
		Name: "server", Host: "example.test", Port: 22, Username: "root",
		Authentication: "agent", Encoding: "utf-8", CommandHistory: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddCommand(ctx, connection.ID, "ls -la", false); err != nil {
		t.Fatal(err)
	}
	if err := s.AddCommand(ctx, connection.ID, "ls -la", false); err != nil {
		t.Fatal(err)
	}
	history, err := s.SuggestCommands(ctx, connection.ID, "ls", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Command != "ls -la" || history[0].UseCount != 2 {
		t.Fatalf("unexpected connection history: %#v", history)
	}
	var global model.CommandHistory
	if err := v.Get(ctx, TypeHistory, commandHistoryID("", "ls -la"), &global); err != nil {
		t.Fatal(err)
	}
	if global.ConnectionID != "" || global.Command != "ls -la" || global.UseCount != 2 {
		t.Fatalf("unexpected global history: %#v", global)
	}
}

func TestPutConnectionTrimsRemark(t *testing.T) {
	ctx := context.Background()
	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVaultOnCleanup(t, v)
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	s := New(v)
	connection, err := s.PutConnection(ctx, model.Connection{
		Name: "server", Remark: "  primary bastion  ", Host: "example.test", Port: 22, Username: "root",
		Authentication: "agent", Encoding: "utf-8", CommandHistory: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if connection.Remark != "primary bastion" {
		t.Fatalf("unexpected remark after save: %q", connection.Remark)
	}

	stored, err := s.GetConnection(ctx, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Remark != "primary bastion" {
		t.Fatalf("unexpected persisted remark: %q", stored.Remark)
	}
}

func TestSettingsNormalizeTerminalThemeColors(t *testing.T) {
	ctx := context.Background()
	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVaultOnCleanup(t, v)
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	s := New(v)
	err = s.PutSettings(ctx, model.Settings{
		Theme:               "dark",
		FontFamily:          "Cascadia Mono, monospace",
		FontSize:            14,
		TerminalThemePreset: "custom",
		TerminalThemeColors: model.TerminalThemeColors{
			Background: "#123456",
			Foreground: "oops",
		},
		LockAfterMinutes:      15,
		DisconnectOnLock:      false,
		AutoReconnect:         true,
		SyncCommandHistory:    false,
		SyncSecretsByDefault:  false,
		SensitiveCommandRules: []string{`(?i)secret=\S+`},
	})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.TerminalThemeColors.Background != "#123456" {
		t.Fatalf("background was not preserved: %#v", settings.TerminalThemeColors)
	}
	if settings.TerminalThemeColors.Foreground != model.DefaultTerminalThemeColors().Foreground {
		t.Fatalf("invalid foreground was not normalized: %#v", settings.TerminalThemeColors)
	}
}

func TestDefaultSettingsEnableAutoReconnect(t *testing.T) {
	settings := model.DefaultSettings()
	if !settings.AutoReconnect {
		t.Fatal("auto reconnect should be enabled by default")
	}
}

func closeVaultOnCleanup(t *testing.T, v *vault.Vault) {
	t.Helper()
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Errorf("close vault: %v", err)
		}
	})
}
