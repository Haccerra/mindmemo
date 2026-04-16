package shell

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

var markerRegex = regexp.MustCompile(
		`__MM_EXIT__([A-Za-z0-9_\-]+)__(-?[0-9]+)_`,
)

type Runner struct {
	shellName string
	cmd       *exec.Cmd
	ptmx      *os.File
	reader    *bufio.Reader
	mu        sync.Mutex
	closed    bool
	counter   int64
}

type Result struct {
	Output   []byte
	ExitCode int
}

func NewRunner(shellName string) (*Runner, error) {
	cmd, err := commandForShell(shellName)
	if err != nil {
		return nil, err
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	r := &Runner {
		shellName: shellName,
		cmd:       cmd,
		ptmx:      ptmx,
		reader:    bufio.NewReader(ptmx),
	}

	if err := r.initShell(); err != nil {
		_ = r.Close()
		return nil, err
	}

	return r, nil
}

func (r *Runner) initShell() error {
	var bootstrap string

	switch r.shellName {
	case "fish":
		bootstrap = "function fish_prompt; end\nstty -echo\n"
	case "powershell":
		bootstrap = "$ErrorActionPreference='Continue'\nfunction prompt { '' }\n"
	default:
		bootstrap = "export PS1=''\nexport PROMPT_COMMAND=''\nstty -echo\n"
	}

	_, err := r.ptmx.Write([]byte(bootstrap))
	if err != nil {
		return err
	}

	_, _, execErr := r.Execute("true")
	if execErr != nil {
		_, _, execErr = r.Execute("$null")
	}

	return execErr
}

func (r *Runner) Execute(command string) ([]byte, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, 0, fmt.Errorf("shell runner closed")
	}
	r.counter ++

	marker := fmt.Sprintf("mm-%d-%d", time.Now().UnixNano(), r.counter)
	wrapped := r.wrapCommand(command, marker)
	if _, err := r.ptmx.Write([]byte(wrapped)); err != nil {
		return nil, 0, err
	}

	buf := bytes.Buffer{}
	for {
		line, err := r.reader.ReadBytes('\n')
		if err != nil {
			return nil, 0, err
		}

		matches := markerRegex.FindSubmatch(line)
		if len(matches) == 3 {
			mToken := string(matches[1])
			if mToken != marker {
				buf.Write(line)
				continue
			}

			code, convErr := strconv.Atoi(strings.TrimSpace(string(matches[2])))
			if convErr != nil {
				return nil, 0, convErr
			}

			return trimLeadingShellNoise(buf.Bytes()), code, nil
		}
		buf.Write(line)
	}
}

func trimLeadingShellNoise(raw []byte) []byte {
	if len(raw) == 0 {
		return raw
	}
	if bytes.HasPrefix(raw, []byte("\r\n")) {
		return raw[2:]
	}
	if bytes.HasPrefix(raw, []byte("\n")) {
		return raw[1:]
	}
	return raw
}

func (r *Runner) wrapCommand(command, marker string) string {
	if r.shellName == "powershell" {
		return fmt.Sprintf("%s\nWrite-Output \"__MM_EXIT__%s__$LASTEXITCODE__\"\n",
				command, marker,
		)
	}
	return fmt.Sprintf("%s\nprintf '\\n__MM_EXIT__%s__%%d__\\n' $?\n",
			command, marker,
	)
}

func (r *Runner) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true

	_, _ = r.ptmx.Write([]byte("exit\n"))
	_ = r.ptmx.Close()

	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
	}
	if r.cmd != nil {
		_ = r.cmd.Wait()
	}

	return nil
}

func commandForShell(shellName string) (*exec.Cmd, error) {
	shellName = Normalise(shellName)
	switch shellName {
	case "bash":
		return exec.Command("bash", "--noprofile", "--norc", "-i"), nil
	case "zsh":
		return exec.Command("zsh", "-f", "-i"), nil
	case "sh":
		return exec.Command("sh", "-i"), nil
	case "fish":
		return exec.Command("fish", "--private"), nil
	case "powershell":
		return exec.Command("powershell", "-NoLogo", "-NoProfile"), nil
	default:
		return nil, fmt.Errorf("unsupported shell %q", shellName)
	}
}

