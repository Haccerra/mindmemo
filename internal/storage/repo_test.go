package storage

import (
	"context"
	"path/filepath"
	"testing"

	"mindmemo/internal/model"
)

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "mindmemo.db")

	repo, err := New(dbPath)
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	t.Cleanup(func() {
		_ = repo.Close()
	})

	return repo
}

