package zmodemstore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nyaterminal/nyaterminal-desktop/internal/sftpclient"
)

type Store struct {
	mu        sync.Mutex
	files     map[string]*pendingFile
	transfers map[string]*sftpclient.Transfer
	active    map[string]string
}

type pendingFile struct {
	file      *os.File
	finalPath string
	tempPath  string
	expected  int64
	written   int64
}

type TransferUpdate struct {
	SessionID    string
	ConnectionID string
	Name         string
	Direction    string
	Status       string
	BytesDone    int64
	TotalBytes   int64
	Error        string
}

const maxReceiveSize = int64(100 << 30)

func New() *Store {
	return &Store{
		files:     make(map[string]*pendingFile),
		transfers: make(map[string]*sftpclient.Transfer),
		active:    make(map[string]string),
	}
}

func (s *Store) Begin(finalPath string, expected int64) (string, error) {
	absolute, err := filepath.Abs(finalPath)
	if err != nil {
		return "", err
	}
	if expected < 0 || expected > maxReceiveSize {
		return "", errors.New("invalid ZMODEM file size")
	}
	info, err := os.Stat(filepath.Dir(absolute))
	if err != nil || !info.IsDir() {
		return "", errors.New("ZMODEM destination directory does not exist")
	}
	tempPath := absolute + ".nyapart"
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	s.mu.Lock()
	s.files[id] = &pendingFile{
		file: file, finalPath: absolute, tempPath: tempPath, expected: expected,
	}
	s.mu.Unlock()
	return id, nil
}

func (s *Store) Record(update TransferUpdate) {
	if update.SessionID == "" || update.ConnectionID == "" || update.Name == "" || update.Status == "" {
		return
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.active[update.SessionID]
	record := s.transfers[id]
	if update.Status == "running" {
		if record == nil || record.Mode != "zmodem" || record.Name != update.Name ||
			record.Direction != update.Direction || record.ConnectionID != update.ConnectionID {
			id = uuid.NewString()
			record = &sftpclient.Transfer{
				ID: id, ConnectionID: update.ConnectionID, SessionID: update.SessionID, Mode: "zmodem",
				Name: update.Name, Direction: update.Direction, Status: update.Status,
				BytesDone: update.BytesDone, TotalBytes: update.TotalBytes,
				Error: update.Error, CreatedAt: now, UpdatedAt: now,
			}
			s.transfers[id] = record
			s.active[update.SessionID] = id
			return
		}
	}
	if record == nil {
		id = uuid.NewString()
		record = &sftpclient.Transfer{
			ID: id, ConnectionID: update.ConnectionID, SessionID: update.SessionID, Mode: "zmodem",
			Name: update.Name, Direction: update.Direction, Status: update.Status,
			BytesDone: update.BytesDone, TotalBytes: update.TotalBytes,
			Error: update.Error, CreatedAt: now, UpdatedAt: now,
		}
		s.transfers[id] = record
		if isTransferTerminal(update.Status) {
			return
		}
		s.active[update.SessionID] = id
		return
	}
	record.ConnectionID = update.ConnectionID
	record.SessionID = update.SessionID
	record.Mode = "zmodem"
	record.Name = update.Name
	record.Direction = update.Direction
	record.Status = update.Status
	record.BytesDone = update.BytesDone
	if update.TotalBytes > 0 || record.TotalBytes == 0 {
		record.TotalBytes = update.TotalBytes
	}
	record.Error = update.Error
	record.UpdatedAt = now
	if isTransferTerminal(update.Status) {
		delete(s.active, update.SessionID)
	}
}

func (s *Store) Write(id string, data []byte) error {
	s.mu.Lock()
	pending := s.files[id]
	if pending == nil {
		s.mu.Unlock()
		return errors.New("ZMODEM receive handle not found")
	}
	if pending.written+int64(len(data)) > maxReceiveSize ||
		(pending.expected > 0 && pending.written+int64(len(data)) > pending.expected) {
		s.mu.Unlock()
		return errors.New("ZMODEM sender exceeded announced file size")
	}
	written, err := pending.file.Write(data)
	pending.written += int64(written)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if written != len(data) {
		return errors.New("short ZMODEM file write")
	}
	return nil
}

func (s *Store) Finish(id string) error {
	s.mu.Lock()
	pending := s.files[id]
	delete(s.files, id)
	s.mu.Unlock()
	if pending == nil {
		return errors.New("ZMODEM receive handle not found")
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(pending.tempPath)
		}
	}()
	if err := pending.file.Sync(); err != nil {
		pending.file.Close()
		return err
	}
	if err := pending.file.Close(); err != nil {
		return err
	}
	if pending.expected > 0 && pending.written != pending.expected {
		return errors.New("ZMODEM file size does not match sender metadata")
	}
	_ = os.Remove(pending.finalPath)
	if err := os.Rename(pending.tempPath, pending.finalPath); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) Cancel(id string) error {
	s.mu.Lock()
	pending := s.files[id]
	delete(s.files, id)
	s.mu.Unlock()
	if pending == nil {
		return nil
	}
	_ = pending.file.Close()
	return os.Remove(pending.tempPath)
}

func (s *Store) Close() {
	s.mu.Lock()
	files := s.files
	s.files = make(map[string]*pendingFile)
	s.mu.Unlock()
	for _, pending := range files {
		_ = pending.file.Close()
		_ = os.Remove(pending.tempPath)
	}
}

func (s *Store) ListTransfers() []sftpclient.Transfer {
	s.mu.Lock()
	result := make([]sftpclient.Transfer, 0, len(s.transfers))
	for _, transfer := range s.transfers {
		result = append(result, *transfer)
	}
	s.mu.Unlock()
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].UpdatedAt.After(result[j].UpdatedAt)
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func isTransferTerminal(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}
