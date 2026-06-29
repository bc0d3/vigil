// Package store persiste las huellas de Vigil y reporta cambios entre corridas.
//
// Es la capa con estado, deliberadamente separada del núcleo puro
// (internal/fingerprint): el comando `scan` no la toca; solo `watch` la usa.
// Un único backend basado en database/sql sirve a SQLite (modernc, Go puro) y a
// Postgres (pgx), sin CGO.
package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // driver "pgx" (Postgres)
	_ "modernc.org/sqlite"             // driver "sqlite" (Go puro, sin CGO)
)

// Status describe el resultado de observar una URL respecto de la corrida previa.
type Status string

// Estados posibles que devuelve Observe.
const (
	StatusNew       Status = "new"       // primera vez que se ve la URL
	StatusChanged   Status = "changed"   // el sha256 difiere del anterior
	StatusUnchanged Status = "unchanged" // mismo sha256 que la última vez
)

// Observation es lo que se guarda de una huella.
type Observation struct {
	URL         string
	SHA256      string
	Status      int
	Size        int64
	ContentType string
	At          time.Time
}

// Change es lo que Observe reporta tras registrar una observación.
type Change struct {
	Status         Status
	PreviousSHA256 string // vacío si es nueva
}

// Store guarda huellas y detecta cambios.
type Store interface {
	// Observe registra la huella de una URL y reporta si cambió respecto de la
	// última vez que se vio.
	Observe(o Observation) (Change, error)
	// Close libera la conexión.
	Close() error
}

// Config selecciona el backend. Si DSN apunta a Postgres (postgres:// o
// postgresql://) se usa pgx; en cualquier otro caso, SQLite con DSN como ruta.
type Config struct {
	// SQLitePath es la ruta del archivo .db (modo SQLite).
	SQLitePath string
	// PostgresDSN, si no está vacío, fuerza el backend Postgres.
	PostgresDSN string
}

// Open abre (y migra) el store según la config.
func Open(cfg Config) (Store, error) {
	if cfg.PostgresDSN != "" {
		return openSQL("pgx", cfg.PostgresDSN, dialectPostgres)
	}
	if cfg.SQLitePath == "" {
		return nil, fmt.Errorf("store: falta SQLitePath o PostgresDSN")
	}
	// busy_timeout evita 'database is locked' bajo concurrencia liviana.
	dsn := "file:" + cfg.SQLitePath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	return openSQL("sqlite", dsn, dialectSQLite)
}

type dialect int

const (
	dialectSQLite dialect = iota
	dialectPostgres
)

type sqlStore struct {
	db      *sql.DB
	dialect dialect
}

const schema = `
CREATE TABLE IF NOT EXISTS observations (
	url          TEXT PRIMARY KEY,
	domain       TEXT NOT NULL,
	sha256       TEXT NOT NULL,
	status       INTEGER,
	size         BIGINT,
	content_type TEXT,
	first_seen   TEXT NOT NULL,
	last_seen    TEXT NOT NULL,
	last_changed TEXT NOT NULL,
	runs         BIGINT NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_observations_domain ON observations(domain);
CREATE TABLE IF NOT EXISTS changes (
	url         TEXT NOT NULL,
	domain      TEXT NOT NULL,
	sha256      TEXT NOT NULL,
	previous    TEXT NOT NULL DEFAULT '',
	observed_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_changes_url ON changes(url);
`

func openSQL(driver, dsn string, d dialect) (Store, error) {
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", driver, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping %s: %w", driver, err)
	}
	// SQLite es un único archivo: una sola conexión de escritura evita locks.
	if d == dialectSQLite {
		db.SetMaxOpenConns(1)
	}
	s := &sqlStore{db: db, dialect: d}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migración: %w", err)
	}
	return s, nil
}

// rebind adapta los placeholders '?' al dialecto ($1, $2... en Postgres).
func (s *sqlStore) rebind(q string) string {
	if s.dialect != dialectPostgres {
		return q
	}
	var b strings.Builder
	n := 0
	for _, r := range q {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *sqlStore) Observe(o Observation) (Change, error) {
	dom := domainOf(o.URL)
	at := o.At.UTC().Format(time.RFC3339)

	tx, err := s.db.Begin()
	if err != nil {
		return Change{}, err
	}
	defer func() { _ = tx.Rollback() }() // no-op si ya se hizo Commit

	var prev string
	err = tx.QueryRow(s.rebind(`SELECT sha256 FROM observations WHERE url = ?`), o.URL).Scan(&prev)

	switch {
	case err == sql.ErrNoRows:
		if _, err := tx.Exec(s.rebind(`
			INSERT INTO observations
				(url, domain, sha256, status, size, content_type, first_seen, last_seen, last_changed, runs)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`),
			o.URL, dom, o.SHA256, o.Status, o.Size, o.ContentType, at, at, at); err != nil {
			return Change{}, err
		}
		if err := s.recordChange(tx, o.URL, dom, o.SHA256, "", at); err != nil {
			return Change{}, err
		}
		if err := tx.Commit(); err != nil {
			return Change{}, err
		}
		return Change{Status: StatusNew}, nil

	case err != nil:
		return Change{}, err
	}

	if prev == o.SHA256 {
		if _, err := tx.Exec(s.rebind(`
			UPDATE observations SET last_seen = ?, runs = runs + 1 WHERE url = ?`),
			at, o.URL); err != nil {
			return Change{}, err
		}
		if err := tx.Commit(); err != nil {
			return Change{}, err
		}
		return Change{Status: StatusUnchanged, PreviousSHA256: prev}, nil
	}

	if _, err := tx.Exec(s.rebind(`
		UPDATE observations
		SET sha256 = ?, status = ?, size = ?, content_type = ?, last_seen = ?, last_changed = ?, runs = runs + 1
		WHERE url = ?`),
		o.SHA256, o.Status, o.Size, o.ContentType, at, at, o.URL); err != nil {
		return Change{}, err
	}
	if err := s.recordChange(tx, o.URL, dom, o.SHA256, prev, at); err != nil {
		return Change{}, err
	}
	if err := tx.Commit(); err != nil {
		return Change{}, err
	}
	return Change{Status: StatusChanged, PreviousSHA256: prev}, nil
}

func (s *sqlStore) recordChange(tx *sql.Tx, url, domain, sha, previous, at string) error {
	_, err := tx.Exec(s.rebind(`
		INSERT INTO changes (url, domain, sha256, previous, observed_at) VALUES (?, ?, ?, ?, ?)`),
		url, domain, sha, previous, at)
	return err
}

func (s *sqlStore) Close() error { return s.db.Close() }

// domainOf extrae el host (sin puerto) de una URL. Si no parsea, devuelve la URL
// cruda para no perder la fila.
func domainOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return u.Hostname()
}
