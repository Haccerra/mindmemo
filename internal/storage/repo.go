package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"mindmemo/internal/model"
)

var (
	ErrNoActiveSession = errors.New("no active session")
	ErrNotFound = errors.New("not found")
)

type Repository struct {
	db *sql.DB
}

func New(path string) (*Repository, error) {
	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *Repository) DB() *sql.DB {
	return r.db
}


