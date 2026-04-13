package config

import (
	"errors"
	"os"
	"path/filepath"
)

const (
	AppDirName = ".mindmemo"
	DBFileName = "mindmemo.db"
)

type Paths struct {
	BaseDir string
	DBPath  string
}

func ResolvePaths() (Paths, error) {
	if custom := os.Getenv("MINDMEMO_HOME"); custom != "" {
		base := filepath.Clean(custom)
		return Paths{BaseDir: base, DBPath: filepath.Join(base, DBFileName)}, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	if home == "" {
		return Paths{}, errors.New("Unable to determine home directory")
	}

	base := filepath.Join(home, AppDirName)
	return Paths{BaseDir: base, DBPath: filepath.Join(base, DBFileName)}, nil
}

func EnsureDataDir(paths Paths) error {
	return os.MkdirAll(paths.BaseDir, 0o755)
}

