package shell

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var supportedShells = map[string]struct{} {
	"bash":       {},
	"zsh":        {},
	"sh":         {},
	"fish":       {},
	"powershell": {},
}

func IsSupported(name string) bool {
	_, ok := supportedShells[name]
	return ok
}

func Normalise(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = filepath.Base(name)
	name = strings.TrimSuffix(name, ".exe")
	if name == "pwsh" {
		return "powershell"
	}
	return name
}

func ResolveRequestedShell(requested string) (string, error) {
	if requested != "" {
		n := Normalise(requested)
		if !IsSupported(n) {
			return "", fmt.Errorf(
					"unsupported shell %q\n" +
					"supported: (bash, zsh, sh, fish, powershell)",
					requested,
			)
		}
		return n, nil
	}

	if runtime.GOOS == "windows" {
		return "powershell", nil
	}

	fromEnv := os.Getenv("SHELL")
	n := Normalise(fromEnv)
	if n == "" {
		return "sh", nil
	}
	if !IsSupported(n) {
		return "", fmt.Errorf(
				"unsupported shell %q\n" +
				"supported: (bash, zsh, sh, fish, powershell)",
				n,
		)
	}

	return n, nil
}
