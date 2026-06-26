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

func TestReceiveRejectsAnnouncedSizeOverflow(t *testing.T) {
	store := New()
	defer store.Close()
	target := filepath.Join(t.TempDir(), "received.bin")
	id, err := store.Begin(target, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(id, []byte("toolong")); err == nil {
		t.Fatal("announced file size overflow was accepted")
	}
	if err := store.Cancel(id); err != nil {
		t.Fatal(err)
	}
}

func TestReceiveCleansTemporaryFileOnSizeMismatch(t *testing.T) {
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
	if err := store.Finish(id); err == nil {
		t.Fatal("size mismatch was accepted")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("mismatched receive was committed")
	}
	if _, err := os.Stat(target + ".nyapart"); !os.IsNotExist(err) {
		t.Fatal("temporary receive file remains after mismatch")
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
