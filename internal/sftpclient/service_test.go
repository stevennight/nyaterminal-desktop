package sftpclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDirectoryGrantBlocksTraversal(t *testing.T) {
	root, err := os.MkdirTemp(".", "sftp-grant-test-")
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
