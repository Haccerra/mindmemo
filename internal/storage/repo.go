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

func (r *Repository) CreateOpenSession(ctx context.Context, name string, autoNamed bool, mode model.SessionMode, shell string, pid int) (model.Session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Session{}, err
	}
	defer tx.Rollback()

	if existing, err := r.getActiveSessionTx(ctx, tx); err != nil {
		return model.Session{}, err
	} else if existing != nil {
		return model.Session{}, fmt.Errorf("session %q is already active", existing.Name)
	}

	createdAt := nowText()
	res, err := tx.ExecContext(ctx,
			`insert into sessions (
					name, auto_named, mode, is_open,
					open_pid, shell, created_at)
			values (?, ?, ?, 1, ?, ?, ?)
			`,
			name, boolToInt(autoNamed), string(mode), pid, shell, createdAt,
	)
	if err != nil {
		return model.Session{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return model.Session{}, err
	}

	if err := r.setStateTx(ctx, tx, "active_session_id",
			strconv.FormatInt(id, 10)); err != nil {
		return model.Session{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.Session{}, err
	}

	created, err := parseTime(createdAt)
	if err != nil {
		return model.Session{}, err
	}

	return model.Session {
		ID:        id,
		Name:      name,
		AutoNamed: autoNamed,
		Mode:      mode,
		IsOpen:    true,
		OpenPID:   pid,
		Shell:     shell,
		CreatedAt: created,
	}, nil
}
