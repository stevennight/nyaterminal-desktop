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
	defer v.Close()
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	s := New(v)
	connection, err := s.PutConnection(ctx, model.Connection{
		Name: "server", Host: "example.test", Port: 22, Username: "root",
		Authentication: "agent", Encoding: "utf-8",
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
