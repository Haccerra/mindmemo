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

func (r *Repository) CleanupActiveSession(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	active, err := r.getActiveSessionTx(ctx, tx)
	if err != nil {
		return err
	}
	if active == nil {
		return ErrNoActiveSession
	}

	if _, err := tx.ExecContext(ctx,
			`delete from sessions where id = ?`,
			active.ID); err != nil {
		return err
	}

	if err := r.clearStateTx(ctx, tx, "active_session_id"); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repository) DeleteSessionByID(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx,
			`delete from sessions where id = ?`, id,
	)
	return err
}

func (r *Repository) RenameSession(ctx context.Context, id int64, newName string) error {
	_, err := r.db.ExecContext(ctx,
			`update sessions
			set name = ?, auto_named = 0
			where id = ?
			`, newName, id,
	)
	return err
}

func (r *Repository) LatestUnnamedClosedSession(ctx context.Context) (*model.Session, error) {
	row := r.db.QueryRowContext(ctx,
			`select id, name, auto_named, mode, is_open,
				open_pid, shell, created_at, closed_at,
			from sessions
			where auto_named = 1 and is_open = 0
			order by id desc
			limit 1
	`)

	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

func (r *Repository) SetSessionMode(ctx context.Context, id int64, mode model.SessionMode) error {
	_, err := r.db.ExecContext(ctx,
			`update sessions set mode = ? where id = ?`,
			string(mode), id,
	)
	return err
}

func (r *Repository) ListSessions(ctx context.Context) ([]model.Session, error) {
	rows, err := r.db.QueryContext(ctx,
		`select id, name, auto_named, mode, is_open,
			open_pid, shell, created_at, closed_at
		from sessions
		ordered by id desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Session
	for rows.Next() {
		s, err := scanSessionRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ListClosedSessions(ctx context.Context) ([]model.Session, error) {
	rows, err := r.db.QueryContext(ctx,
			`select id, name, auto_named, mode, is_open,
				open_pid, shell, created_at, closed-at
			from sessions
			where is_open = 0
			order by id desc
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Session
	for rows.Next() {
		s, err := scanSessionRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *Repository) ResumeClosedSession(ctx context.Context, id int64, pid int) (*model.Session, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	active, err := r.getActiveSessionTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return nil, fmt.Errorf("session %q is already active", active.Name)
	}

	row := tx.QueryRowContext(ctx,
			`select id, name, auto_named, mode, is_open,
				open_pid, shell, created_at, closed_at
			from sessions where id = ?
			`, id,
	)

	session, err := scanSession(row)
	if err != nil {
		return nil, err
	}
	if session.IsOpen {
		return nil, fmt.Errorf("selected session is already open")
	}

	if _, err := tx.ExecContext(ctx,
			`update sessions
			set is_open = 1, open_pid = ?, closed_at = NULL
			where id = ?
			`, pid, id); err != nil {
		return nil, err
	}

	if err := r.setStateTx(ctx, tx, "active_session_id",
			strconv.FormatInt(id, 10)); err != nil {
		return nil, err
	}

	return r.GetSessionByID(ctx, id)
}

func (r *Repository) ReconcileStaleOpenSessions(
		ctx context.Context,
		aliveFn func(int) bool,
) (int, error) {
	rows, err := r.db.QueryContext(ctx,
			`select id, open_pid from sessions where is_open = 1`,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var staleIDs []int64
	for rows.Next() {
		var id int64
		var pid int

		if err := rows.Scan(&id, &pid); err != nil {
			return 0, err
		}
		if pid <= 0 || !aliveFn(pid) {
			staleIDs = append(staleIDs, id)
		}
	}

	if err := rows.Err(); err != nil {
		return 0, nil
	}

	if len(staleIDs) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	for _, id := range staleIDs {
		if _, err := tx.ExecContext(ctx,
				`update sessions
				set is_open = 0, open_pid = 0,
					closed_at = coalesce(closed_at, ?)
				where id = ?
				`, nowText(), id); err != nil {
			return 0, err
		}
	}

	activeRaw, err := r.getStateTx(ctx, tx, "active_session_id")
	if err == nil {
		activeID, convErr := strconv.ParseInt(activeRaw, 10, 64)
		if convErr == nil {
			for _, id := range staleIDs {
				if id == activeID {
					if err := r.clearStateTx(ctx, tx, "active_session_id"); err != nil {
						return 0, err
					}
					break
				}
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return len(staleIDs), nil
}

func (r *Repository) NextSeq(ctx context.Context, sessionID int64) (int64, error) {
	row := r.db.QueryRowContext(ctx,
			`select coalesce(max(seq), 0) + 1
			from history_entries
			where session_id = ?`,
			sessionID,
	)

	var seq int64
	if err := row.Scan(&seq); err != nil {
		return 0, err
	}
	return seq, nil
}

func (r *Repository) AddHistory(
		ctx context.Context,
		sessionID int64,
		source string,
		output []byte,
		aliasRoot string,
		aliasRevision int,
) (model.HistoryEntry, error) {
	seq, err := r.NextSeq(ctx, sessionID)
	if err != nil {
		return model.HistoryEntry{}, err
	}

	created := nowText()
	res, err := r.db.ExecContext(ctx,
			`insert into history_entries(session_id, seq, source_command,
				output, alias_root, alias_revision, created_at)
			values(?, ?, ?, ?, nullif(?, ''), ?, ?)`,
			sessionID, seq, source, output, aliasRoot, aliasRevision, created,
	)

	if err != nil {
		return model.HistoryEntry{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return model.HistoryEntry{}, err
	}

	t, err := parseTime(created)
	if err != nil {
		return model.HistoryEntry{}, err
	}

	e := model.HistoryEntry{
		ID:        id,
		SessionID: sessionID,
		Seq:       seq,
		Source:    source,
		Output:    output,
		AliasRoot: aliasRoot,
		AliasRev:  aliasRevision,
		CreatedAt: t,
	}
	e.DisplayAlias = displayAlias(aliasRoot, aliasRevision)

	return e, nil
}

func (r *Repository) LastHistoryEntry(ctx context.Context, sessionID int64) (*model.HistoryEntry, error) {
	row := r.db.QueryRowContext(ctx,
			`select id, session_id, seq, source_command, output,
				coalesce(alias_root, ''), alias_revision, created_at
			from history_entries
			where session_id = ?
			order by seq desc
			limit 1`,
			sessionID,
	)

	entry, err := scanHistory(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return entry, nil
}

func (r *Repository) ForgetLastHistoryEntry(ctx context.Context, sessionID int64) (*model.HistoryEntry, error) {
	entry, err := r.LastHistoryEntry(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if _, err := r.db.ExecContext(ctx,
			`delete from history_entries where id = ?`,
			entry.ID); err != nil {
		return nil, err
	}

	return entry, nil
}

func (r *Repository) NameLastHistoryEntry(ctx context.Context, sessionID int64, alias string) (*model.HistoryEntry, error) {
	entry, err := r.LastHistoryEntry(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
			`update history_entries
			set alias_root = ?, alias_revision = 0
			where id = ?`,
			alias, entry.ID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	updated, err := r.GetHistoryEntry(ctx, entry.ID)
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (r *Repository) GetHistoryEntry(ctx context.Context, id int64) (*model.HistoryEntry, error) {
	row := r.db.QueryRowContext(ctx,
			`select id, session_id, seq, source_command, output,
				coalesce(alias_root, ''), alias_revision, created_at
			from history_entries
			where id = ?`,
			id,
	)
	return scanHistory(row)
}

func (r *Repository) ListHistory(ctx context.Context, sessionID int64) ([]model.HistoryEntry, error) {
	rows, err := r.db.QueryContext(ctx,
			`select id, session_id, seq, source_command, output,
				coalesce(alias_root, ''), alias_revision, created_at
			from history_entries
			where session_id = ?
			order by seq asc`,
			sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.HistoryEntry
	for rows.Next() {
		e, err := scanHistoryRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *Repository) SearchHistory(
		ctx context.Context,
		sessionID int64,
		pattern string,
) ([]model.HistoryEntry, error) {
	q := "%" + strings.ToLower(pattern) + "%"
	rows, err := r.db.QueryContext(ctx,
			`select id, session_id, seq, source_command, output,
				coalesce(alias_root, ''), alias_revision, created_at
			from history_entries
			where session_id = ? and (
				lower(source_command) like ? or
				lower(coalesce(alias_root, '')) like ?
			)
			order by seq asc`,
			sessionID, q, q,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.HistoryEntry
	for rows.Next() {
		e, err := scanHistoryRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *Repository) DeleteHistoryEntry(ctx context.Context, sessionID, entryID int64) error {
	_, err := r.db.ExecContext(ctx,
			`delete from history_entries where id = ? and session_id = ?`,
			entryID, sessionID,
	)
	return err
}

func (r *Repository) RenameHistoryAlias(ctx context.Context, sessionID, entryID int64, alias string) error {
	entry, err := r.GetHistoryEntry(ctx, entryID)
	if err != nil {
		return err
	}
	if entry.SessionID != sessionID {
		return ErrNotFound
	}
	if entry.AliasRoot == "" {
		_, err := r.db.ExecContext(ctx,
				`update history_entries
				set alias_root = ?, alias_revision = 0
				where id = ? and session_id = ?`,
				alias, entryID, sessionID,
		)
		return err
	}

	_, err = r.db.ExecContext(ctx,
			`update history_entries
			set alias_root = ?
			where session_id = ? and alias_root = ?`,
			alias, sessionID, entry.AliasRoot,
	)
	return err
}

func (r *Repository) GetNameBaseCommand(
		ctx context.Context,
		sessionID int64,
		alias string,
) (*model.HistoryEntry, error) {
	row := r.db.QueryRowContext(ctx,
			`select id, session_id, seq, source_command, output,
				coalesce(alias_root, ''), alias_revision, created_at
			from history_entries
			where session_id = ? and
				alias_root = ? and
				alias_revision = 0
			limit 1`,
			sessionID, alias,
	)
	return scanHistory(row)
}

func (r *Repository) NextAliasRevision(ctx context.Context, sessionID int64, alias string) (int, error) {
	row := r.db.QueryRowContext(ctx,
			`select coalesce(max(alias_revision), 0) + 1
			from history_entries
			where session_id = ? and alias_root = ?`,
			sessionID, alias,
	)

	var rev int
	if err := row.Scan(&rev); err != nil {
		return 0, err
	}

	return rev, nil
}

func (r *Repository) GetAliasRevisionByOffset(
		ctx context.Context,
		sessionID int64,
		alias string,
		offset int,
) (*model.HistoryEntry, error) {
	entries, err := r.ListAliasRevisions(ctx, sessionID, alias)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, ErrNotFound
	}

	index := 0
	if offset < 0 {
		index = -offset
	}
	if index >= len(entries) {
		return nil, ErrNotFound
	}
	return &entries[index], nil
}

func (r *Repository) ListAliasRevisions(
		ctx context.Context,
		sessionID int64,
		alias string,
) ([]model.HistoryEntry, error) {
	rows, err := r.db.QueryContext(ctx,
			`select id, session_id, seq, source_command, output,
				coalesce(alias_root, ''), alias_revision, created_at
			from history_entries
			where session_id = ? and alias_root = ?
			order by alias_revision desc`,
			sessionID, alias,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.HistoryEntry
	for rows.Next() {
		e, err := scanHistoryRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *Repository) SaveProc(ctx context.Context, proc model.Proc) error {
	now := nowText()
	created := now
	existing, err := r.GetProc(ctx, proc.Name)
	if err == nil {
		created = existing.CreatedAt.UTC().Format(timeFormat)
	}
	_, err = r.db.ExecContext(ctx,
			`insert into procs(name, definition, description,
				created_at, updated_at)
			values(?, ?, ?, ?, ?)
			on conflict(name) do update set
				definition = excluded.definition,
				description = excluded.description,
				updated_at = excluded.updated_at`,
			proc.Name, proc.Definition, proc.Description, created, now,
	)
	return err
}

func (r *Repository) UpsertProc(ctx context.Context, name, definition, description string) error {
	row := r.db.QueryRowContext(ctx,
			`select created_at from procs where name = ?`,
			name,
	)

	var created string
	err := row.Scan(&created)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		created = nowText()
	}

	_, err = r.db.ExecContext(ctx,
			`insert into procs(name, definition, description,
				created_at, updated_at)
			values(?, ?, ?, ?, ?)
			on conflict(name) do update set
				definition = excluded.definition,
				description = excluded.description,
				updated_at = excluded.updated_at`,
			name, definition, description, created, nowText(),
	)
	return err
}

func (r *Repository) DeleteProc(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx,
			`delete from procs where name = ?`,
			name,
	)
	return err
}

func (r *Repository) GetProc(ctx context.Context, name string) (*model.Proc, error) {
	row := r.db.QueryRowContext(ctx,
			`select name, definition, description,
				created_at, updated_at
			from procs where name = ?`,
			name,
	)

	var p model.Proc
	var created, updated string
	if err := row.Scan(&p.Name, &p.Definition, &p.Description,
			&created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	createdAt, err := parseTime(created)
	if err != nil {
		return nil, err
	}

	updatedAt, err := parseTime(updated)
	if err != nil {
		return nil, err
	}

	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return &p, nil
}

func (r *Repository) ListProcs(ctx context.Context) ([]model.Proc, error) {
	rows, err := r.db.QueryContext(ctx,
			`select name, definition, description,
				created_at, updated_at
			from procs
			order by name asc`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Proc
	for rows.Next() {
		var p model.Proc
		var created, updated string
		if err := rows.Scan(&p.Name, &p.Definition, &p.Description,
				&created, &updated); err != nil {
			return nil, err
		}

		createdAt, err := parseTime(updated)
		if err != nil {
			return nil, err
		}

		updatedAt, err := parseTime(updated)
		if err != nil {
			return nil, err
		}

		p.CreatedAt = createdAt
		p.UpdatedAt = updatedAt
		out = append(out, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *Repository) ReplaceSessionProcSnapshot(ctx context.Context, sessionID int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
			`delete from session_procs where session_id = ?`,
			sessionID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
			`insert into session_procs(session_id, name, definition, description, updated_at)
			select ?, name, definition, description, updated_at from procs`,
			sessionID); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repository) UpsertSessionProc(
		ctx context.Context,
		sessionID int64,
		name, definition, description string,
) error {
	_, err := r.db.ExecContext(ctx,
			`insert into session_procs(session_id, name,
				definition, description, updated_at)
			values(?, ?, ?, ?, ?)
			on conflict(session_id, name) do update set
				definition = excluded.definition,
				description = excluded.description,
				updated_at = excluded.updated_at`,
			sessionID, name, definition, description, nowText(),
	)
	return err
}

func (r *Repository) GetSessionProc(ctx context.Context, sessionID int64, name string) (*model.Proc, error) {
	row := r.db.QueryRowContext(ctx,
			`select name, definition, description, updated_at
			from session_procs
			where session_id = ? and name = ?`,
			sessionID, name,
	)

	var p model.Proc
	var updated string
	if err := row.Scan(&p.Name, &p.Definition, &p.Description, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	updatedAt, err := parseTime(updated)
	if err != nil {
		return nil, err
	}

	p.UpdatedAt = updatedAt
	p.CreatedAt = updatedAt

	return &p, nil
}

func (r *Repository) ListSessionProcs(ctx context.Context, sessionID int64) ([]model.Proc, error) {
	rows, err := r.db.QueryContext(ctx,
			`select name, definition, description, updated_at
			from session_procs
			where session_id = ?
			order by name asc`,
			sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Proc
	for rows.Next() {
		var p model.Proc
		var updated string

		if err := rows.Scan(&p.Name, &p.Definition, &p.Description, &updated); err != nil {
			return nil, err
		}

		updatedAt, err := parseTime(updated)
		if err != nil {
			return nil, err
		}

		p.CreatedAt = updatedAt
		p.UpdatedAt = updatedAt
		out = append(out, p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}

func (r *Repository) DeleteSessionProc(ctx context.Context, sessionID int64, name string) error {
	_, err := r.db.ExecContext(ctx,
			`delete from session_procs where session_id = ? and name = ?`,
			sessionID, name,
	)
	return err
}

func (r *Repository) ImportProcsTransactional(
		ctx context.Context,
		incoming []model.Proc,
		replace map[string]bool,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, p := range incoming {
		if p.Name == "" {
			return fmt.Errorf("invalid proc with empty name")
		}

		exists := false
		row := tx.QueryRowContext(ctx,
				`select 1 from procs where name = ?`,
				p.Name,
		)

		var one int
		err := row.Scan(&one)
		if err == nil {
			exists = true
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		if exists && !replace[p.Name] {
			continue
		}

		created := nowText()

		if exists {
			row2 := tx.QueryRowContext(ctx,
					`select created_at from procs where name = ?`,
					p.Name,
			)
			_ = row2.Scan(&created)
		}

		if _, err := tx.ExecContext(ctx,
				`insert into procs(name, definition, description, created_at, updated_at)
				values (?, ?, ?, ?, ?)
				on conflict(name) do update set
					definition = excluded.definition
					description = excluded.description
					updated_at = excluded.updated_at`,
				p.Name, p.Definition, p.Description, created, nowText()); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *Repository) SaveProcDraft(ctx context.Context, draft model.ProcDraft) error {
	payload, err := json.Marshal(draft)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(ctx,
			`insert into runtime_state(key, value)
			values('proc_draft', ?)
			on conflict(key) do update set
				value = excluded.value`,
			string(payload),
	)

	return err
}

func (r *Repository) LoadProcDraft(ctx context.Context) (*model.ProcDraft, error) {
	value, err := r.getState(ctx, "proc_draft")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var draft model.ProcDraft
	if err := json.Unmarshal([]byte(value), &draft); err != nil {
		return nil, err
	}

	return &draft, nil
}

func (r *Repository) ClearProcDraft(ctx context.Context) error {
	return r.clearState(ctx, "proc_draft")
}

func (r *Repository) SaveLastOpenedEntry(ctx context.Context, sessionID, entryID int64) error {
	key := fmt.Sprintf("show_last_entry_%d", sessionID)
	_, err := r.db.ExecContext(ctx,
			`insert into runtime_state(key, value)
			values(?, ?)
			on conflict(key) do update set
				value = excluded.value`,
			key, strconv.FormatInt(entryID, 10),
	)

	return err
}

func (r *Repository) GetLastOpenedEntry(ctx context.Context, sessionID int64) (int64, error) {
	key := fmt.Sprintf("show_last_entry_%d", sessionID)

	value, err := r.getState(ctx, key)
	if err != nil {
		return 0, err
	}

	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (r *Repository) getActiveSessionTx(ctx context.Context, tx *sql.Tx) (*model.Session, error) {
	idRaw, err := r.getStateTx(ctx, tx, "active_session_id")
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

	row := tx.QueryRowContext(ctx,
			`select id, name, auto_named, mode, is_open,
				open_pid, shell, created_at, closed_at
			from sessions where id = ?`,
			id,
	)

	s, err := scanSession(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if clearErr := r.clearStateTx(ctx, tx, "active_session_id"); clearErr != nil {
				return nil, clearErr
			}
			return nil, nil
		}
		return nil, err
	}

	if !s.IsOpen {
		if err := r.clearStateTx(ctx, tx, "active_session_id"); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return s, nil
}

func (r *Repository) getState(ctx context.Context, key string) (string, error) {
	row := r.db.QueryRowContext(ctx,
			`select value from runtime_state where key = ?`,
			key,
	)

	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}

	return value, nil
}

func (r *Repository) getStateTx(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	row := tx.QueryRowContext(ctx,
			`select value from runtime_state where key = ?`,
			key,
	)

	var value string
	if err := row.Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}

	return value, nil
}

func (r *Repository) getStateIntTx(ctx context.Context, tx *sql.Tx, key string) (int, error) {
	value, err := r.getStateTx(ctx, tx, key)
	if err != nil {
		return 0, err
	}

	i, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}

	return i, nil
}

func (r *Repository) setStateTx(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx,
			`insert into runtime_state(key, value)
			values (?, ?)
			on conflict(key) do update set
				value = excluded.value`,
			key, value,
	)
	return err
}

func (r *Repository) clearState(ctx context.Context, key string) error {
	_, err := r.db.ExecContext(ctx,
			`delete from runtime_state where key = ?`,
			key,
	)
	return err
}

func (r *Repository) clearStateTx(ctx context.Context, tx *sql.Tx, key string) error {
	_, err := tx.ExecContext(ctx,
			`delete from runtime_state where key = ?`,
			key,
	)
	return err
}

func scanSession(
		row interface{ Scan(dest ...any) error},
) (*model.Session, error) {
	var s         model.Session
	var autoNamed int
	var mode      string
	var isOpen    int
	var created   string
	var closed    sql.NullString

	if err := row.Scan(&s.ID, &s.Name, &autoNamed, &mode, &isOpen,
			&s.OpenPID, &s.Shell, &created, &closed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	t, err := parseTime(created)
	if err != nil {
		return nil, err
	}

	s.CreatedAt = t
	s.AutoNamed = autoNamed == 1
	s.Mode = model.SessionMode(mode)
	s.IsOpen = isOpen == 1

	if closed.Valid {
		ct, err := parseTime(closed.String)
		if err != nil {
			return nil, err
		}
		s.ClosedAt = &ct
	}

	return &s, nil
}

func scanSessionRows(rows *sql.Rows) (*model.Session, error) {
	return scanSession(rows)
}

func scanHistory(
		row interface{
			Scan(dest ...any) error
		},
) (*model.HistoryEntry, error) {
	var e model.HistoryEntry
	var created string

	if err := row.Scan(&e.ID, &e.SessionID, &e.Seq, &e.Source,
			&e.Output, &e.AliasRoot, &e.AliasRev, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	t, err := parseTime(created)
	if err != nil {
		return nil, err
	}

	e.CreatedAt = t
	e.DisplayAlias = displayAlias(e.AliasRoot, e.AliasRev)

	return &e, nil
}

func scanHistoryRows(rows *sql.Rows) (*model.HistoryEntry, error) {
	return scanHistory(rows)
}

func displayAlias(root string, rev int) string {
	if root == "" {
		return ""
	}
	if rev == 0 {
		return root
	}
	return fmt.Sprintf("%s (%d)", root, rev)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func SortHistoryByRevision(entries []model.HistoryEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].AliasRev < entries[j].AliasRev
	})
}

