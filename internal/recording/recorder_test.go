package recording

import (
	"context"
	"path/filepath"
	"testing"

	"mindmemo/internal/model"
	"mindmemo/internal/storage"
)

type fakeExec struct {
	output []byte
	code   int
	err    error
}

func (f fakeExec) Execute(command string) ([]byte, int, error) {
	return f.output, f.code, f.err
}

func testRepo(t *testing.T) *storage.Repository {
	t.Helper()

	repo, err := storage.New(filepath.Join(t.TempDir(), "mm.db"))
	if err != nil {
		t.Fatalf("new repo: %v", err)
	}

	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestRecorderPersistsSuccessfulNonMindmemo(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()

	s, err := repo.CreateOpenSession(ctx, "s", false,
			model.SessionModePermanent, "sh", 999,
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	recorder := Recorder{
		Repo:       repo,
		SessionID: s.ID,
		Executor:  fakeExec{ output: []byte("ok\n"), code: 0 },
	}

	res, err := recorder.RunCommand(ctx, "echo ok", "echo ok")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if !res.Persisted {
		t.Fatalf("expected persisted result")
	}

	entries, err := repo.ListHistory(ctx, s.ID)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(entries))
	}
}

func TestRecorderDoesNotPersistFailuresOrMindmemo(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()

	s, err := repo.CreateOpenSession(ctx, "s", false,
			model.SessionModePermanent, "sh", 999,
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	failed := Recorder{
		Repo:      repo,
		SessionID: s.ID,
		Executor:  fakeExec{ output: []byte("err\n"), code: 7 },
	}

	res, err := failed.RunCommand(ctx, "false", "false")
	if err != nil {
		t.Fatalf("run failed command: %v", err)
	}
	if res.Persisted {
		t.Fatalf("failed command should not persist")
	}

	mindmemoCmd := Recorder{
		Repo:      repo,
		SessionID: s.ID,
		Executor:  fakeExec{ output: []byte("help\n"), code: 0 },
	}

	res, err = mindmemoCmd.RunCommand(ctx, "mindmemo help", "mindmemo help")
	if err != nil {
		t.Fatalf("run mindmemo command: %v", err)
	}
	if res.Persisted {
		t.Fatalf("mindmemo command should not persist")
	}

	entries, err := repo.ListHistory(ctx, s.ID)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

