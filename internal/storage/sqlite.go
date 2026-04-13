package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const driverName = "sqlite"

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open(driverName, path)
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	statements := []string {
		`create table if not exists sessions (
			id integer primary key autoincrement,
			name text not null,
			auto_named integer not null default 0,
			mode text not null,
			is_open integer not null default 0,
			open_pid integer not null default 0,
			shell text not null,
			created_at text not null,
			closed_at text
		)`,
		`create table if not exists history_entries (
			id integer primary key autoincrement,
			session_id integer not null,
			seq integer not null,
			source_command text not null,
			output blob not null,
			alias_root text,
			alias_revision integer not null default 0,
			created_at text not null,
			foreign key(session_id) references sessions(id) on delete cascade
		)`,
		`create index if not exists idx_history_session_seq
			on history_entries(session_id, seq)
		`,
		`create unique index if not exists uniq_alias_revision
			on history_entries(session_id, alias_root, alias_revision)
			where alias_root is not null
		`,
		`create table if not exists procs (
			name text primary key,
			definition text not null,
			description text not null,
			created_at text not null,
			updated_at text not null
		)`,
		`create table if not exists session_procs (
			session_id integer not null,
			name text not null,
			definition text not null,
			description text not null,
			updated_at text not null,
			primary key(session_id, name),
			foreign key(session_id) references sessions(id) on delete cascade
		)`,
		`create table if not exists runtime_state (
			key text primary key,
			value text not null
		)`,
	}

	return nil
}
