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

func TestAllocateUnknownNameIncrements(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	name1, idx1, err := repo.AllocateUnknownSessionName(ctx)
	if err != nil {
		t.Fatalf("allocate #1: %v", err)
	}
	if name1 != "unknown session (1)" || idx1 != 1 {
		t.Fatalf("unexpected first allocation: %s %d", name1, idx1)
	}

	name2, idx2, err := repo.AllocateUnknownSessionName(ctx)
	if err != nil {
		t.Fatalf("allocate #2: %v", err)
	}
	if name2 != "unknown session (2)" || idx2 != 2 {
		t.Fatalf("unexpected second allocation: %s %d", name2, idx2)
	}
}
