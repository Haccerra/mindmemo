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

func TestSessionLifecycle(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	s, err := repo.CreateOpenSession(ctx, "s1", false,
			model.SessionModePermanent, "zsh", 123,
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if !s.IsOpen {
		t.Fatalf("expected open session")
	}

	active, err := repo.GetActiveSession(ctx)
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active == nil || active.ID != s.ID {
		t.Fatalf("unexpected active session: %#v", active)
	}

	if _, err := repo.CloseActiveSession(ctx); err != nil {
		t.Fatalf("close active: %v", err)
	}

	active, err = repo.GetActiveSession(ctx)
	if err != nil {
		t.Fatalf("get active after close: %v", err)
	}
	if active != nil {
		t.Fatalf("expected nil active session after close")
	}
}

func TestHistoryRevisions(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	s, err := repo.CreateOpenSession(ctx, "s1", false,
			model.SessionModePermanent, "zsh", 123,
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	base, err := repo.AddHistory(ctx, s.ID, "echo one",
			[]byte("one\n"), "cmd", 0,
	)
	if err != nil {
		t.Fatalf("add base: %v", err)
	}
	if base.DisplayAlias != "cmd" {
		t.Fatalf("unexpected base alias: %s", base.DisplayAlias)
	}

	rev, err := repo.AddHistory(ctx, s.ID, "echo one",
			[]byte("one\n"), "cmd", 1,
	)
	if err != nil {
		t.Fatalf("add rev: %v", err)
	}
	if rev.DisplayAlias != "cmd (1)" {
		t.Fatalf("unexpected revision alias: %s", rev.DisplayAlias)
	}

	latest, err := repo.GetAliasRevisionByOffset(ctx, s.ID, "cmd", 0)
	if err != nil {
		t.Fatalf("get latest revision: %v", err)
	}
	if latest.AliasRev != 1 {
		t.Fatalf("expected latest rev 1, got %d", latest.AliasRev)
	}

	previous, err := repo.GetAliasRevisionByOffset(ctx, s.ID, "cmd", -1)
	if err != nil {
		t.Fatalf("get previous revision: %v", err)
	}
	if previous.AliasRev != 0 {
		t.Fatalf("expected previous rev 0, got %d", previous.AliasRev)
	}
}

func TestProcSnapshotAndDraft(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if err := repo.UpsertProc(ctx, "build",
			"go test ./...", "run tests"); err != nil {
		t.Fatalf("upsert proc: %v", err)
	}

	s, err := repo.CreateOpenSession(ctx, "s1", false,
			model.SessionModePermanent, "zsh", 123,
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := repo.ReplaceSessionProcSnapshot(ctx, s.ID); err != nil {
		t.Fatalf("replace snapshot: %v", err)
	}

	proc, err := repo.GetSessionProc(ctx, s.ID, "build")
	if err != nil {
		t.Fatalf("get session proc: %v", err)
	}
	if proc.Name != "build" {
		t.Fatalf("unexpected proc name: %s", proc.Name)
	}

	draft := model.ProcDraft{
		Name:       "name",
		Definition: "echo {}",
		Desc:       "desc",
	}

	if err := repo.SaveProcDraft(ctx, draft); err != nil {
		t.Fatalf("save draft: %v", err)
	}

	loaded, err := repo.LoadProcDraft(ctx)
	if err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if loaded.Name != draft.Name || loaded.Definition != draft.Definition ||
			loaded.Desc != draft.Desc {
		t.Fatalf("draft mismatch: %#v vs %#v", loaded, draft)
	}

	if err := repo.ClearProcDraft(ctx); err != nil {
		t.Fatalf("clear draft: %v", err)
	}

	if _, err := repo.LoadProcDraft(ctx); err == nil {
		t.Fatalf("expected missing draft after clear")
	}
}
