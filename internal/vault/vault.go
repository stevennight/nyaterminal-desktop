// noinspection SqlNoDataSourceInspection
package vault

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	_ "modernc.org/sqlite"
)

var (
	ErrLocked          = errors.New("vault is locked")
	ErrNotInitialized  = errors.New("vault is not initialized")
	ErrAlreadyExists   = errors.New("vault is already initialized")
	ErrInvalidPassword = errors.New("invalid password")
)

const (
	kdfMemory      = 64 * 1024
	kdfIterations  = 3
	kdfParallelism = 2
	kdfKeyLength   = 32
)

type Vault struct {
	mu      sync.RWMutex
	db      *sql.DB
	dataDir string
	key     []byte
}

type Status struct {
	Initialized       bool   `json:"initialized"`
	Locked            bool   `json:"locked"`
	QuickUnlock       bool   `json:"quickUnlock"`
	QuickUnlockMethod string `json:"quickUnlockMethod"`
}

type ExportedRecord struct {
	ID        string
	Type      string
	Data      []byte
	UpdatedAt time.Time
}

func Open(path string) (*Vault, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := hardenDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
		"PRAGMA foreign_keys = ON",
		`CREATE TABLE IF NOT EXISTS vault_metadata (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			salt BLOB NOT NULL,
			nonce BLOB NOT NULL,
			wrapped_key BLOB NOT NULL,
			kdf_memory INTEGER NOT NULL,
			kdf_iterations INTEGER NOT NULL,
			kdf_parallelism INTEGER NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS encrypted_records (
			id TEXT PRIMARY KEY,
			record_type TEXT NOT NULL,
			nonce BLOB NOT NULL,
			ciphertext BLOB NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS encrypted_records_type_idx
		 ON encrypted_records(record_type, updated_at)`,
		`CREATE TABLE IF NOT EXISTS quick_unlock (
			profile TEXT PRIMARY KEY,
			nonce BLOB NOT NULL,
			wrapped_key BLOB NOT NULL
		)`,
		"DROP TABLE IF EXISTS password_wrappers",
	} {
		if _, err := db.Exec(statement); err != nil {
			if closeErr := db.Close(); closeErr != nil {
				err = errors.Join(err, closeErr)
			}
			return nil, err
		}
	}
	if err := hardenFile(path); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		return nil, err
	}
	return &Vault{db: db, dataDir: filepath.Dir(path)}, nil
}

func (v *Vault) Close() error {
	v.Lock()
	return v.db.Close()
}

func (v *Vault) Status(ctx context.Context) (Status, error) {
	var count int
	if err := v.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM vault_metadata").Scan(&count); err != nil {
		return Status{}, err
	}
	v.mu.RLock()
	locked := len(v.key) == 0
	v.mu.RUnlock()
	var quickUnlock int
	if err := v.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM quick_unlock").Scan(&quickUnlock); err != nil {
		return Status{}, err
	}
	return Status{
		Initialized:       count > 0,
		Locked:            locked,
		QuickUnlock:       quickUnlock > 0,
		QuickUnlockMethod: userPresenceMethod,
	}, nil
}

func (v *Vault) Initialize(ctx context.Context, password string) error {
	if len(password) < 12 {
		return errors.New("master password must contain at least 12 characters")
	}
	status, err := v.Status(ctx)
	if err != nil {
		return err
	}
	if status.Initialized {
		return ErrAlreadyExists
	}
	vaultKey := make([]byte, chacha20poly1305.KeySize)
	salt := make([]byte, 16)
	if _, err := rand.Read(vaultKey); err != nil {
		return err
	}
	if _, err := rand.Read(salt); err != nil {
		wipe(vaultKey)
		return err
	}
	wrappingKey := deriveKey(password, salt, kdfMemory, kdfIterations, kdfParallelism)
	nonce, wrapped, err := seal(wrappingKey, vaultKey, []byte("nyaterminal:vault-key:v1"))
	wipe(wrappingKey)
	if err != nil {
		wipe(vaultKey)
		return err
	}
	_, err = v.db.ExecContext(ctx, `
		INSERT INTO vault_metadata(
			id, salt, nonce, wrapped_key, kdf_memory, kdf_iterations,
			kdf_parallelism, created_at
		) VALUES(1, ?, ?, ?, ?, ?, ?, ?)`,
		salt, nonce, wrapped, kdfMemory, kdfIterations, kdfParallelism,
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		wipe(vaultKey)
		return err
	}
	v.mu.Lock()
	v.key = vaultKey
	v.mu.Unlock()
	return nil
}

func (v *Vault) Unlock(ctx context.Context, password string) error {
	var salt, nonce, wrapped []byte
	var memory, iterations uint32
	var parallelism uint8
	err := v.db.QueryRowContext(ctx, `
		SELECT salt, nonce, wrapped_key, kdf_memory, kdf_iterations, kdf_parallelism
		FROM vault_metadata WHERE id = 1`,
	).Scan(&salt, &nonce, &wrapped, &memory, &iterations, &parallelism)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotInitialized
	}
	if err != nil {
		return err
	}
	if memory > 256*1024 || iterations > 10 || parallelism > 16 {
		return errors.New("unsafe KDF parameters")
	}
	wrappingKey := deriveKey(password, salt, memory, iterations, parallelism)
	key, err := open(wrappingKey, nonce, wrapped, []byte("nyaterminal:vault-key:v1"))
	wipe(wrappingKey)
	if err != nil || len(key) != chacha20poly1305.KeySize {
		wipe(key)
		return ErrInvalidPassword
	}
	v.mu.Lock()
	wipe(v.key)
	v.key = key
	v.mu.Unlock()
	return nil
}

func (v *Vault) ChangePassword(ctx context.Context, oldPassword, newPassword string) error {
	if len(newPassword) < 12 {
		return errors.New("master password must contain at least 12 characters")
	}
	if err := v.Unlock(ctx, oldPassword); err != nil {
		return err
	}
	key, err := v.keyCopy()
	if err != nil {
		return err
	}
	defer wipe(key)
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	wrappingKey := deriveKey(newPassword, salt, kdfMemory, kdfIterations, kdfParallelism)
	nonce, wrapped, err := seal(wrappingKey, key, []byte("nyaterminal:vault-key:v1"))
	wipe(wrappingKey)
	if err != nil {
		return err
	}
	_, err = v.db.ExecContext(ctx, `
		UPDATE vault_metadata SET salt = ?, nonce = ?, wrapped_key = ?,
		kdf_memory = ?, kdf_iterations = ?, kdf_parallelism = ? WHERE id = 1`,
		salt, nonce, wrapped, kdfMemory, kdfIterations, kdfParallelism,
	)
	return err
}

func (v *Vault) Lock() {
	v.mu.Lock()
	wipe(v.key)
	v.key = nil
	v.mu.Unlock()
}

func (v *Vault) EnableQuickUnlock(ctx context.Context, profile string) error {
	appendDiagnosticLog(v.dataDir, "hello", "EnableQuickUnlock start profile=%s", profile)
	if err := verifyUserPresence(v.dataDir, "Enable Windows Hello unlock for NyaTerminal"); err != nil {
		appendDiagnosticLog(v.dataDir, "hello", "EnableQuickUnlock verifyUserPresence error=%v", err)
		return err
	}
	key, err := v.keyCopy()
	if err != nil {
		appendDiagnosticLog(v.dataDir, "hello", "EnableQuickUnlock keyCopy error=%v", err)
		return err
	}
	defer wipe(key)
	quickKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(quickKey); err != nil {
		appendDiagnosticLog(v.dataDir, "hello", "EnableQuickUnlock rand error=%v", err)
		return err
	}
	defer wipe(quickKey)
	nonce, wrapped, err := seal(quickKey, key, []byte("nyaterminal:quick-unlock:v1:"+profile))
	if err != nil {
		appendDiagnosticLog(v.dataDir, "hello", "EnableQuickUnlock seal error=%v", err)
		return err
	}
	if err := SaveQuickUnlock(profile, quickKey); err != nil {
		appendDiagnosticLog(v.dataDir, "hello", "EnableQuickUnlock SaveQuickUnlock error=%v", err)
		return err
	}
	_, err = v.db.ExecContext(ctx, `
		INSERT INTO quick_unlock(profile, nonce, wrapped_key) VALUES(?, ?, ?)
		ON CONFLICT(profile) DO UPDATE SET nonce = excluded.nonce,
		wrapped_key = excluded.wrapped_key`, profile, nonce, wrapped)
	if err != nil {
		_ = DeleteQuickUnlock(profile)
	}
	appendDiagnosticLog(v.dataDir, "hello", "EnableQuickUnlock done error=%v", err)
	return err
}

func (v *Vault) UnlockQuick(ctx context.Context, profile string) error {
	appendDiagnosticLog(v.dataDir, "hello", "UnlockQuick start profile=%s", profile)
	if err := verifyUserPresence(v.dataDir, "Unlock NyaTerminal"); err != nil {
		appendDiagnosticLog(v.dataDir, "hello", "UnlockQuick verifyUserPresence error=%v", err)
		return err
	}
	quickKey, err := LoadQuickUnlock(profile)
	if err != nil {
		appendDiagnosticLog(v.dataDir, "hello", "UnlockQuick LoadQuickUnlock error=%v", err)
		return ErrInvalidPassword
	}
	defer wipe(quickKey)
	var nonce, wrapped []byte
	if err := v.db.QueryRowContext(ctx,
		"SELECT nonce, wrapped_key FROM quick_unlock WHERE profile = ?", profile,
	).Scan(&nonce, &wrapped); err != nil {
		appendDiagnosticLog(v.dataDir, "hello", "UnlockQuick query error=%v", err)
		return ErrInvalidPassword
	}
	key, err := open(quickKey, nonce, wrapped, []byte("nyaterminal:quick-unlock:v1:"+profile))
	if err != nil || len(key) != chacha20poly1305.KeySize {
		wipe(key)
		appendDiagnosticLog(v.dataDir, "hello", "UnlockQuick unwrap error=%v keyLen=%d", err, len(key))
		return ErrInvalidPassword
	}
	v.mu.Lock()
	wipe(v.key)
	v.key = key
	v.mu.Unlock()
	appendDiagnosticLog(v.dataDir, "hello", "UnlockQuick success profile=%s", profile)
	return nil
}

func (v *Vault) DisableQuickUnlock(ctx context.Context, profile string) error {
	_ = DeleteQuickUnlock(profile)
	_, err := v.db.ExecContext(ctx, "DELETE FROM quick_unlock WHERE profile = ?", profile)
	return err
}

func (v *Vault) Put(ctx context.Context, recordType, id string, value any) error {
	key, err := v.keyCopy()
	if err != nil {
		return err
	}
	defer wipe(key)
	plain, err := json.Marshal(value)
	if err != nil {
		return err
	}
	defer wipe(plain)
	aad := []byte("nyaterminal:record:v1:" + recordType + ":" + id)
	nonce, ciphertext, err := seal(key, plain, aad)
	if err != nil {
		return err
	}
	_, err = v.db.ExecContext(ctx, `
		INSERT INTO encrypted_records(id, record_type, nonce, ciphertext, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET record_type = excluded.record_type,
		nonce = excluded.nonce, ciphertext = excluded.ciphertext,
		updated_at = excluded.updated_at`,
		id, recordType, nonce, ciphertext, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (v *Vault) Get(ctx context.Context, recordType, id string, target any) error {
	key, err := v.keyCopy()
	if err != nil {
		return err
	}
	defer wipe(key)
	var storedType string
	var nonce, ciphertext []byte
	err = v.db.QueryRowContext(ctx,
		"SELECT record_type, nonce, ciphertext FROM encrypted_records WHERE id = ?", id,
	).Scan(&storedType, &nonce, &ciphertext)
	if err != nil {
		return err
	}
	if storedType != recordType {
		return errors.New("record type mismatch")
	}
	aad := []byte("nyaterminal:record:v1:" + recordType + ":" + id)
	plain, err := open(key, nonce, ciphertext, aad)
	if err != nil {
		return errors.New("record authentication failed")
	}
	defer wipe(plain)
	return json.Unmarshal(plain, target)
}

func (v *Vault) List(ctx context.Context, recordType string, constructor func() any) (result []any, err error) {
	key, err := v.keyCopy()
	if err != nil {
		return nil, err
	}
	defer wipe(key)
	rows, err := v.db.QueryContext(ctx, `
		SELECT id, nonce, ciphertext FROM encrypted_records
		WHERE record_type = ? ORDER BY updated_at, id`, recordType)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		var id string
		var nonce, ciphertext []byte
		if err := rows.Scan(&id, &nonce, &ciphertext); err != nil {
			return nil, err
		}
		aad := []byte("nyaterminal:record:v1:" + recordType + ":" + id)
		plain, err := open(key, nonce, ciphertext, aad)
		if err != nil {
			return nil, fmt.Errorf("decrypt record %s: authentication failed", id)
		}
		value := constructor()
		err = json.Unmarshal(plain, value)
		wipe(plain)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		err = rowsErr
		return nil, err
	}
	return result, nil
}

func (v *Vault) Delete(ctx context.Context, id string) error {
	key, err := v.keyCopy()
	if err != nil {
		return err
	}
	wipe(key)
	_, err = v.db.ExecContext(ctx, "DELETE FROM encrypted_records WHERE id = ?", id)
	return err
}

func (v *Vault) RecordType(ctx context.Context, id string) (string, error) {
	var recordType string
	err := v.db.QueryRowContext(ctx,
		"SELECT record_type FROM encrypted_records WHERE id = ?", id,
	).Scan(&recordType)
	return recordType, err
}

// DeleteAndPut atomically removes one encrypted record and writes another.
// It is used for the encrypted synchronization deletion journal so a crash
// cannot leave a deletion without a corresponding tombstone.
func (v *Vault) DeleteAndPut(
	ctx context.Context, deletedID, recordType, id string, value any,
) (err error) {
	key, err := v.keyCopy()
	if err != nil {
		return err
	}
	defer wipe(key)
	plain, err := json.Marshal(value)
	if err != nil {
		return err
	}
	defer wipe(plain)
	aad := []byte("nyaterminal:record:v1:" + recordType + ":" + id)
	nonce, ciphertext, err := seal(key, plain, aad)
	if err != nil {
		return err
	}
	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx, &err)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO encrypted_records(id, record_type, nonce, ciphertext, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET record_type = excluded.record_type,
		nonce = excluded.nonce, ciphertext = excluded.ciphertext,
		updated_at = excluded.updated_at`,
		id, recordType, nonce, ciphertext, time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM encrypted_records WHERE id = ?", deletedID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (v *Vault) ExportRecords(
	ctx context.Context, allowedTypes map[string]bool,
) (result []ExportedRecord, err error) {
	key, err := v.keyCopy()
	if err != nil {
		return nil, err
	}
	defer wipe(key)
	rows, err := v.db.QueryContext(ctx, `
		SELECT id, record_type, nonce, ciphertext, updated_at
		FROM encrypted_records ORDER BY updated_at, id`)
	if err != nil {
		return nil, err
	}
	defer closeRows(rows, &err)
	for rows.Next() {
		var record ExportedRecord
		var nonce, ciphertext []byte
		var updated string
		if err := rows.Scan(&record.ID, &record.Type, &nonce, &ciphertext, &updated); err != nil {
			return nil, err
		}
		if !allowedTypes[record.Type] {
			continue
		}
		aad := []byte("nyaterminal:record:v1:" + record.Type + ":" + record.ID)
		record.Data, err = open(key, nonce, ciphertext, aad)
		if err != nil {
			return nil, fmt.Errorf("decrypt record %s: authentication failed", record.ID)
		}
		record.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			wipe(record.Data)
			return nil, err
		}
		result = append(result, record)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		err = rowsErr
		return nil, err
	}
	return result, nil
}

func (v *Vault) PutJSON(ctx context.Context, recordType, id string, data []byte) error {
	var value json.RawMessage
	if !json.Valid(data) {
		return errors.New("invalid JSON record")
	}
	value = append(value, data...)
	defer wipe(value)
	return v.Put(ctx, recordType, id, value)
}

func (v *Vault) keyCopy() ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	if len(v.key) != chacha20poly1305.KeySize {
		return nil, ErrLocked
	}
	key := make([]byte, len(v.key))
	copy(key, v.key)
	return key, nil
}

func deriveKey(password string, salt []byte, memory, iterations uint32, parallelism uint8) []byte {
	return argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, kdfKeyLength)
}

func seal(key, plaintext, aad []byte) ([]byte, []byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad), nil
}

func open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid nonce")
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func closeRows(rows *sql.Rows, errp *error) {
	if closeErr := rows.Close(); closeErr != nil {
		*errp = errors.Join(*errp, closeErr)
	}
}

func rollbackTx(tx *sql.Tx, errp *error) {
	if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
		*errp = errors.Join(*errp, rollbackErr)
	}
}
