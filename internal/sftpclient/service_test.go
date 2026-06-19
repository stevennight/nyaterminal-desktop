package sftpclient

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalDirectoryGrantBlocksTraversal(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(workingDirectory, "sftp-grant-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New(nil)
	location, err := service.GrantLocalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(location.Items) != 1 || location.Items[0].Name != "file.txt" {
		t.Fatalf("unexpected directory listing: %#v", location.Items)
	}
	if _, err := service.ListLocal(location.Token, "../"); err == nil {
		t.Fatal("directory traversal was accepted")
	}
	if _, err := service.ListLocal("invalid", "."); err == nil {
		t.Fatal("invalid capability token was accepted")
	}
}

func TestCopyTransferReportsProgressAndCancellation(t *testing.T) {
	job := &transferJob{value: Transfer{Status: "running", TotalBytes: 1024}}
	ctx, cancel := context.WithCancel(context.Background())
	source := &cancellingReader{
		reader: bytes.NewReader(make([]byte, 1024)),
		cancel: cancel,
	}
	var destination bytes.Buffer
	_, err := copyTransfer(ctx, &destination, source, job)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if job.snapshot().BytesDone == 0 {
		t.Fatal("transfer progress was not recorded")
	}
}

func FuzzCleanRemote(f *testing.F) {
	for _, seed := range []string{".", "/", "/tmp/file", "../secret", "a/../../b", "文件.txt"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		cleaned, err := cleanRemote(value)
		if err == nil {
			if cleaned == ".." || strings.HasPrefix(cleaned, "../") ||
				strings.ContainsRune(cleaned, '\x00') {
				t.Fatalf("unsafe path accepted: %q -> %q", value, cleaned)
			}
		}
	})
}

type cancellingReader struct {
	reader *bytes.Reader
	cancel context.CancelFunc
	read   bool
}

func (r *cancellingReader) Read(value []byte) (int, error) {
	if r.read {
		return r.reader.Read(value)
	}
	r.read = true
	count, err := r.reader.Read(value[:min(len(value), 128)])
	r.cancel()
	return count, err
}
