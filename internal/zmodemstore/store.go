package zmodemstore

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

type Store struct {
	mu    sync.Mutex
	files map[string]*pendingFile
}

type pendingFile struct {
	file      *os.File
	finalPath string
	tempPath  string
	expected  int64
	written   int64
}

const maxReceiveSize = int64(100 << 30)

func New() *Store {
	return &Store{files: make(map[string]*pendingFile)}
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
	return os.Rename(pending.tempPath, pending.finalPath)
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
