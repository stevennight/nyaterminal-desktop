package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultEncryptsRecordsAndLocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ctx := context.Background()
	password := "correct horse battery staple"
	if err := v.Initialize(ctx, password); err != nil {
		t.Fatal(err)
	}
	secret := struct {
		Host     string `json:"host"`
		Password string `json:"password"`
	}{Host: "secret.example", Password: "never-plaintext"}
	if err := v.Put(ctx, "connection", "record-1", secret); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if err := v.Get(ctx, "connection", "record-1", &secret); !errors.Is(err, ErrLocked) {
		t.Fatalf("expected locked error, got %v", err)
	}
	if err := v.Unlock(ctx, "wrong password value"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("expected invalid password, got %v", err)
	}
	if err := v.Unlock(ctx, password); err != nil {
		t.Fatal(err)
	}
	var restored struct {
		Host     string `json:"host"`
		Password string `json:"password"`
	}
	if err := v.Get(ctx, "connection", "record-1", &restored); err != nil {
		t.Fatal(err)
	}
	if restored != secret {
		t.Fatalf("restored value differs: %#v", restored)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range [][]byte{[]byte(secret.Host), []byte(secret.Password)} {
		if bytes.Contains(raw, plaintext) {
			t.Fatalf("database contains plaintext %q", plaintext)
		}
	}
}

func TestChangePasswordRewrapsVaultKey(t *testing.T) {
	v, err := Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ctx := context.Background()
	oldPassword := "old password with enough entropy"
	newPassword := "new password with enough entropy"
	if err := v.Initialize(ctx, oldPassword); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "test", "id", map[string]string{"value": "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := v.ChangePassword(ctx, oldPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	if err := v.Unlock(ctx, oldPassword); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("old password unexpectedly worked: %v", err)
	}
	if err := v.Unlock(ctx, newPassword); err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := v.Get(ctx, "test", "id", &value); err != nil {
		t.Fatal(err)
	}
	if value["value"] != "secret" {
		t.Fatal("record was not preserved")
	}
}

func TestLegacyLockPasswordWrappersAreRemovedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.db")
	v, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS password_wrappers (
			profile TEXT PRIMARY KEY,
			salt BLOB NOT NULL,
			nonce BLOB NOT NULL,
			wrapped_key BLOB NOT NULL,
			kdf_memory INTEGER NOT NULL,
			kdf_iterations INTEGER NOT NULL,
			kdf_parallelism INTEGER NOT NULL
		)`); err != nil {
		t.Fatal(err)
	}
	if _, err := v.db.ExecContext(ctx, `
		INSERT INTO password_wrappers(
			profile, salt, nonce, wrapped_key, kdf_memory, kdf_iterations, kdf_parallelism
		) VALUES('lock', X'00', X'00', X'00', 1, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	v, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()

	var tableCount int
	if err := v.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'password_wrappers'`,
	).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 0 {
		t.Fatal("legacy password_wrappers table was not removed")
	}
}

func TestRecordTamperingIsDetectedAndNoncesAreUnique(t *testing.T) {
	v, err := Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	ctx := context.Background()
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "test", "one", map[string]string{"value": "first"}); err != nil {
		t.Fatal(err)
	}
	var firstNonce []byte
	if err := v.db.QueryRowContext(ctx,
		"SELECT nonce FROM encrypted_records WHERE id = 'one'",
	).Scan(&firstNonce); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "test", "one", map[string]string{"value": "second"}); err != nil {
		t.Fatal(err)
	}
	var secondNonce, ciphertext []byte
	if err := v.db.QueryRowContext(ctx,
		"SELECT nonce, ciphertext FROM encrypted_records WHERE id = 'one'",
	).Scan(&secondNonce, &ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(firstNonce, secondNonce) {
		t.Fatal("record encryption reused a nonce")
	}
	ciphertext[len(ciphertext)/2] ^= 0x80
	if _, err := v.db.ExecContext(ctx,
		"UPDATE encrypted_records SET ciphertext = ? WHERE id = 'one'", ciphertext,
	); err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := v.Get(ctx, "test", "one", &value); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}
