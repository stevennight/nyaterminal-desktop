package zmodemstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReceiveStreamsToTemporaryFileAndCommits(t *testing.T) {
	store := New()
	defer store.Close()
	target := filepath.Join(t.TempDir(), "received.bin")
	id, err := store.Begin(target, 6)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(id, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(id, []byte("def")); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(id); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcdef" {
		t.Fatalf("unexpected data: %q", data)
	}
	if _, err := os.Stat(target + ".nyapart"); !os.IsNotExist(err) {
		t.Fatal("temporary receive file remains after commit")
	}
}

func TestReceiveAcceptsMoreThanAnnouncedSize(t *testing.T) {
	store := New()
	defer store.Close()
	target := filepath.Join(t.TempDir(), "received.bin")
	id, err := store.Begin(target, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(id, []byte("toolong")); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(id); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "toolong" {
		t.Fatalf("unexpected data: %q", data)
	}
}

func TestReceiveAcceptsLessThanAnnouncedSize(t *testing.T) {
	store := New()
	defer store.Close()
	target := filepath.Join(t.TempDir(), "received.bin")
	id, err := store.Begin(target, 6)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(id, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(id); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abc" {
		t.Fatalf("unexpected data: %q", data)
	}
}

func TestReceiveRejectsAbsoluteSizeOverflow(t *testing.T) {
	store := New()
	defer store.Close()
	id, err := store.Begin(filepath.Join(t.TempDir(), "received.bin"), 0)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.files[id].written = maxReceiveSize
	store.mu.Unlock()
	if err := store.Write(id, []byte("x")); err == nil {
		t.Fatal("absolute receive size overflow was accepted")
	}
}

func TestRecordTracksZmodemTransfers(t *testing.T) {
	store := New()
	defer store.Close()
	store.Record(TransferUpdate{
		SessionID:    "session-1",
		ConnectionID: "conn-1",
		Name:         "demo.bin",
		Direction:    "upload",
		Status:       "running",
		BytesDone:    0,
		TotalBytes:   10,
	})
	store.Record(TransferUpdate{
		SessionID:    "session-1",
		ConnectionID: "conn-1",
		Name:         "demo.bin",
		Direction:    "upload",
		Status:       "running",
		BytesDone:    5,
		TotalBytes:   10,
	})
	store.Record(TransferUpdate{
		SessionID:    "session-1",
		ConnectionID: "conn-1",
		Name:         "demo.bin",
		Direction:    "upload",
		Status:       "completed",
		BytesDone:    10,
		TotalBytes:   10,
	})
	transfers := store.ListTransfers()
	if len(transfers) != 1 {
		t.Fatalf("unexpected transfer count: %d", len(transfers))
	}
	transfer := transfers[0]
	if transfer.Mode != "zmodem" || transfer.SessionID != "session-1" ||
		transfer.Status != "completed" || transfer.BytesDone != 10 || transfer.TotalBytes != 10 {
		t.Fatalf("unexpected transfer record: %#v", transfer)
	}
}

func TestRecordCompletionUsesActualZeroSize(t *testing.T) {
	store := New()
	defer store.Close()
	store.Record(TransferUpdate{
		SessionID: "session-1", ConnectionID: "conn-1", Name: "empty.bin",
		Direction: "download", Status: "running", TotalBytes: 10,
	})
	store.Record(TransferUpdate{
		SessionID: "session-1", ConnectionID: "conn-1", Name: "empty.bin",
		Direction: "download", Status: "completed", BytesDone: 0, TotalBytes: 0,
	})
	transfers := store.ListTransfers()
	if len(transfers) != 1 || transfers[0].BytesDone != 0 || transfers[0].TotalBytes != 0 {
		t.Fatalf("unexpected completed transfer: %#v", transfers)
	}
}
