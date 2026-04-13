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

func (r *Repository) AllocateUknownSessionName(ctx context.Context) (string, int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()

	counter, err := r.getStateIntTx(ctx, tx, "unknown_counter")
	if err != nil {
		return "", 0, err
	}
	counter++
	if err := r.setStateTx(ctx, tx, "unknown_counter", strconv.Itoa(counter)); err != nil {
		return "", 0, err
	}

	if err := tx.Commit(); err != nil {
		return "", 0, err
	}

	return fmt.Sprintf("unknown session (%d)", counter), counter, nil
}


