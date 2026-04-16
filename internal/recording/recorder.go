package recording

import (
	"context"
	"strings"

	"mindmemo/internal/storage"
)

type Executor interface {
	Execute(command string) ([]byte, int, error)
}

type Recorder struct {
	Repo      *storage.Repository
	SessionID int64
	Executor  Executor
}

type RunResult struct {
	Output    []byte
	ExitCode  int
	Persisted bool
}

func (r *Recorder) RunCommand(
		ctx context.Context,
		inputText string,
		resolvedShellCommand string,
) (RunResult, error) {
	command := strings.TrimSpace(resolvedShellCommand)
	if command == "" {
		command = strings.TrimSpace(inputText)
	}

	out, code, err := r.Executor.Execute(command)
	if err != nil {
		return RunResult{ Output: out, ExitCode: code }, err
	}

	result := RunResult{ Output: out, ExitCode: code }
	if code != 0 || isMindmemoCommand(inputText) {
		return result, nil
	}

	if _, err := r.Repo.AddHistory(ctx, r.SessionID, command,
			out, "", 0); err != nil {
		return result, err
	}

	result.Persisted = true
	return result, nil
}

func isMindmemoCommand(inputText string) bool {
	trimmed := strings.TrimSpace(inputText)
	if trimmed == "mindmemo" {
		return true
	}
	return strings.HasPrefix(trimmed, "mindmemo ")
}

