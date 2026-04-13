package model

import "time"

type SessionMode string

const (
	SessionModePermanent SessionMode = "permanent"
	SessionModeTemp      SessionMode = "temp"
	SessionModeAnon      SessionMode = "anon"
)

type Session struct {
	ID        int64
	Name      string
	AutoNamed bool
	Mode      SessionMode
	IsOpen    bool
	OpenPID   int
	Shell     string
	CreatedAt time.Time
	ClosedAt  *time.Time
}

type HistoryEntry struct {
	ID           int64
	SessionID    int64
	Seq          int64
	Source       string
	Output       []byte
	AliasRoot    string
	AliasRev     int
	CreatedAt    time.Time
	DisplayAlias string
}

type Proc struct {
	Name        string
	Definition  string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProcDraft struct {
	Name       string    `json:"name"`
	Definition string    `json:"definition"`
	Desc       string    `json:"desc"`
}

