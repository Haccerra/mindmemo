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

func (r *Repository) GetActiveSession(ctx context.Context) (*model.Session, error) {
	idRaw, err := r.getState(ctx, "active_session_id")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	id, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil {
		return nil, err
	}

	s, err := r.GetSessionByID(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = r.clearState(ctx, "active_session_id")
			return nil, nil
		}
		return nil, err
	}
	if !s.IsOpen {
		_ = r.clearState(ctx, "active_session_id")
		return nil, nil
	}

	return s, nil
}

func (r *Repository) GetSessionByID(ctx context.Context, id int64) (*model.Session, error) {
	row := r.db.QueryRowContext(ctx,
			`select id, name, auto_named, mode, is_open,
				open_pid, shell, created_at, closed_at
			from sessions where id = ?
			`,
			id,
		)
	return scanSession(row)
}

func (r *Repository) CloseActiveSession(ctx context.Context) (*model.Session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	active, err := r.getActiveSessionTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if active == nil {
		return nil, ErrNoActiveSession
	}

	if _, err := tx.ExecContext(ctx,
			`update sessions
			set is_open = 0, open_pid = 0, closed_at = ?
			where id = ?
			`,
			nowText(), active.ID); err != nil {
				return nil, err
	}

	if err := r.clearStateTx(ctx, tx, "active_session_id"); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	closed, err := r.GetSessionByID(ctx, active.ID)
	if err != nil {
		return nil, err
	}
	if closed.Mode == model.SessionModeTemp {
		if err := r.DeleteSessionByID(ctx, closed.ID); err != nil {
			return closed, err
		}
	}
	return closed, nil
}
