package shell

import (
	"bytes"
	"runtime"
	"testing"
)

func defaultTestShell(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return "powershell"
	}
	return "sh"
}

func TestRunnerPersistsState(t *testing.T) {
	runner, err := NewRunner(defaultTestShell(t))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	t.Cleanup(func() {
		_ = runner.Close()
	})

	shell := defaultTestShell(t)
	if shell == "powershell" {
		if _, code, err := runner.Execute("$x = 'memo'");
				err != nil || code != 0 {
			t.Fatalf("set var: code=%d err=%v", code, err)
		}

		out, code, err := runner.Execute("Write-Output $x")
		if err != nil {
			t.Fatalf("read var: %v", err)
		}
		if code != 0 {
			t.Fatalf("expected zero code, got %d", code)
		}
		if !bytes.Contains(out, []byte("memo")) {
			t.Fatalf("expected persisted value in output, got %q", string(out))
		}
		return
	}

	if _, code, err := runner.Execute("x=memo");
			err != nil || code != 0 {
		t.Fatalf("set var: code=%d err=%v", code, err)
	}

	out, code, err := runner.Execute("echo $x")
	if err != nil {
		t.Fatalf("echo var: %v", err)
	}
	if code != 0 {
		t.Fatalf("expected zero code, got %d", code)
	}
	if !bytes.Contains(out, []byte("memo")) {
		t.Fatalf("expected persisted value in output, got %q", string(out))
	}
}

func TestRunnerExitCode(t *testing.T) {
	runner, err := NewRunner(defaultTestShell(t))
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	t.Cleanup(func() {
		_ = runner.Close()
	})

	cmd := "false"

	if defaultTestShell(t) == "powershell" {
		cmd = "exit 7"
	}

	_, code, err := runner.Execute(cmd)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if defaultTestShell(t) == "powershell" {
		if code == 0 {
			t.Fatalf("expected non-zero exit code")
		}
		return
	}
	if code == 0 {
		t.Fatalf("expected non-zero exit code")
	}
}

func TestShellResolution(t *testing.T) {
	name, err := ResolveRequestedShell("zsh")
	if err != nil {
		t.Fatalf("resolve zsh: %v", err)
	}
	if name != "zsh" {
		t.Fatalf("expected zsh, got %q", name)
	}
	if _, err := ResolveRequestedShell("cmd"); err == nil {
		t.Fatalf("expected unsupported shell error")
	}
}
