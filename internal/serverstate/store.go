package serverstate

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gooog1111/orcheroute/internal/subscriptions"
	_ "modernc.org/sqlite"
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

type Snapshot struct {
	UpdatedAt int64          `json:"updated_at"`
	State     map[string]any `json:"state"`
}

type Event struct {
	ID        int64          `json:"id"`
	CreatedAt int64          `json:"created_at"`
	EventType string         `json:"event_type"`
	Severity  string         `json:"severity"`
	FromNode  *string        `json:"from_node"`
	ToNode    *string        `json:"to_node"`
	Pool      *string        `json:"pool"`
	Reason    *string        `json:"reason"`
	Details   map[string]any `json:"details"`
}

type EventInput struct {
	EventType string
	Severity  string
	FromNode  *string
	ToNode    *string
	Pool      *string
	Reason    *string
	Details   map[string]any
}

type Control struct {
	Mode        string  `json:"mode"`
	ManualNode  *string `json:"manual_node"`
	ManualUntil int64   `json:"manual_until"`
	Enabled     bool    `json:"enabled"`
	UpdatedAt   int64   `json:"updated_at"`
}

const schema = `
CREATE TABLE IF NOT EXISTS snapshot (
    id INTEGER PRIMARY KEY CHECK (id = 1), updated_at INTEGER NOT NULL, payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT, created_at INTEGER NOT NULL,
    event_type TEXT NOT NULL, severity TEXT NOT NULL, from_node TEXT, to_node TEXT,
    pool TEXT, reason TEXT, details TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS events_created_at_idx ON events(created_at DESC);
CREATE TABLE IF NOT EXISTS control (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    mode TEXT NOT NULL CHECK (mode IN ('auto', 'manual', 'emergency')), manual_node TEXT,
    manual_until INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)), updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS subscriptions (
    id TEXT PRIMARY KEY, name TEXT NOT NULL,
    group_name TEXT NOT NULL CHECK (group_name IN ('primary', 'emergency')),
    parser TEXT NOT NULL CHECK (parser IN ('standard', 'blacktemple', 'inline', 'wireguard')),
    secret TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    interval_seconds INTEGER NOT NULL DEFAULT 3600, last_attempt INTEGER NOT NULL DEFAULT 0,
    last_success INTEGER NOT NULL DEFAULT 0, last_status TEXT NOT NULL DEFAULT 'pending',
    last_error TEXT, last_links INTEGER NOT NULL DEFAULT 0,
    last_tested INTEGER NOT NULL DEFAULT 0, last_available INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS subscriptions_group_idx ON subscriptions(group_name, enabled);
`

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, now: time.Now}
	if err := store.initialize(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() error { return store.db.Close() }

func (store *Store) initialize(ctx context.Context) error {
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA busy_timeout=15000"} {
		if _, err := store.db.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	if _, err := store.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	if err := store.migrateSubscriptionParsers(ctx); err != nil {
		return err
	}
	if err := store.migrateSubscriptionQualification(ctx); err != nil {
		return err
	}
	rows, err := store.db.QueryContext(ctx, "PRAGMA table_info(control)")
	if err != nil {
		return err
	}
	hasEnabled := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "enabled" {
			hasEnabled = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasEnabled {
		if _, err := store.db.ExecContext(ctx, "ALTER TABLE control ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1))"); err != nil {
			return err
		}
	}
	if err := store.migrateControlModes(ctx); err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, "INSERT OR IGNORE INTO control(id, mode, manual_node, manual_until, enabled, updated_at) VALUES (1, 'auto', NULL, 0, 1, 0)")
	return err
}

func (store *Store) migrateSubscriptionQualification(ctx context.Context) error {
	rows, err := store.db.QueryContext(ctx, "PRAGMA table_info(subscriptions)")
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, name := range []string{"last_tested", "last_available"} {
		if columns[name] {
			continue
		}
		if _, err := store.db.ExecContext(ctx, "ALTER TABLE subscriptions ADD COLUMN "+name+" INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) migrateControlModes(ctx context.Context) error {
	var definition string
	if err := store.db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='control'").Scan(&definition); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(definition), "'emergency'") {
		return nil
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE control_new (
            id INTEGER PRIMARY KEY CHECK (id = 1),
            mode TEXT NOT NULL CHECK (mode IN ('auto', 'manual', 'emergency')), manual_node TEXT,
            manual_until INTEGER NOT NULL DEFAULT 0,
            enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)), updated_at INTEGER NOT NULL
        )`,
		`INSERT INTO control_new(id,mode,manual_node,manual_until,enabled,updated_at)
         SELECT id,mode,manual_node,manual_until,enabled,updated_at FROM control`,
		`DROP TABLE control`,
		`ALTER TABLE control_new RENAME TO control`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (store *Store) migrateSubscriptionParsers(ctx context.Context) error {
	var definition string
	if err := store.db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE type='table' AND name='subscriptions'").Scan(&definition); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(definition), "'wireguard'") {
		return nil
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE subscriptions_new (
            id TEXT PRIMARY KEY, name TEXT NOT NULL,
            group_name TEXT NOT NULL CHECK (group_name IN ('primary', 'emergency')),
            parser TEXT NOT NULL CHECK (parser IN ('standard', 'blacktemple', 'inline', 'wireguard')),
            secret TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
            interval_seconds INTEGER NOT NULL DEFAULT 3600, last_attempt INTEGER NOT NULL DEFAULT 0,
            last_success INTEGER NOT NULL DEFAULT 0, last_status TEXT NOT NULL DEFAULT 'pending',
            last_error TEXT, last_links INTEGER NOT NULL DEFAULT 0,
            created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
        )`,
		`INSERT INTO subscriptions_new(id,name,group_name,parser,secret,enabled,interval_seconds,last_attempt,last_success,last_status,last_error,last_links,created_at,updated_at)
         SELECT id,name,group_name,parser,secret,enabled,interval_seconds,last_attempt,last_success,last_status,last_error,last_links,created_at,updated_at FROM subscriptions`,
		`DROP TABLE subscriptions`,
		`ALTER TABLE subscriptions_new RENAME TO subscriptions`,
		`CREATE INDEX subscriptions_group_idx ON subscriptions(group_name, enabled)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (store *Store) SetSnapshot(ctx context.Context, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO snapshot(id, updated_at, payload) VALUES(1, ?, ?)
ON CONFLICT(id) DO UPDATE SET updated_at=excluded.updated_at, payload=excluded.payload`, store.now().Unix(), string(encoded))
	return err
}

func (store *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	var result Snapshot
	var payload string
	err := store.db.QueryRowContext(ctx, "SELECT updated_at, payload FROM snapshot WHERE id=1").Scan(&result.UpdatedAt, &payload)
	if err == sql.ErrNoRows {
		return Snapshot{State: map[string]any{}}, nil
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(payload), &result.State)
	return result, err
}

func (store *Store) AddEvent(ctx context.Context, input EventInput) error {
	severity := input.Severity
	if severity == "" {
		severity = "info"
	}
	details := input.Details
	if details == nil {
		details = map[string]any{}
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO events(created_at,event_type,severity,from_node,to_node,pool,reason,details) VALUES(?,?,?,?,?,?,?,?)`,
		store.now().Unix(), input.EventType, severity, input.FromNode, input.ToNode, input.Pool, input.Reason, string(payload))
	return err
}

func (store *Store) Events(ctx context.Context, limit int) ([]Event, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id,created_at,event_type,severity,from_node,to_node,pool,reason,details FROM events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Event{}
	for rows.Next() {
		var event Event
		var fromNode, toNode, pool, reason sql.NullString
		var details string
		if err := rows.Scan(&event.ID, &event.CreatedAt, &event.EventType, &event.Severity, &fromNode, &toNode, &pool, &reason, &details); err != nil {
			return nil, err
		}
		event.FromNode = nullable(fromNode)
		event.ToNode = nullable(toNode)
		event.Pool = nullable(pool)
		event.Reason = nullable(reason)
		if err := json.Unmarshal([]byte(details), &event.Details); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func (store *Store) Control(ctx context.Context) (Control, error) {
	var result Control
	var node sql.NullString
	var enabled int
	err := store.db.QueryRowContext(ctx, "SELECT mode,manual_node,manual_until,enabled,updated_at FROM control WHERE id=1").Scan(&result.Mode, &node, &result.ManualUntil, &enabled, &result.UpdatedAt)
	result.ManualNode = nullable(node)
	result.Enabled = enabled != 0
	return result, err
}

func (store *Store) SetEnabled(ctx context.Context, enabled bool) error {
	_, err := store.db.ExecContext(ctx, "UPDATE control SET enabled=?,updated_at=? WHERE id=1", boolInt(enabled), store.now().Unix())
	return err
}
func (store *Store) SetAuto(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, "UPDATE control SET mode='auto',manual_node=NULL,manual_until=0,updated_at=? WHERE id=1", store.now().Unix())
	return err
}
func (store *Store) SetEmergency(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, "UPDATE control SET mode='emergency',manual_node=NULL,manual_until=0,updated_at=? WHERE id=1", store.now().Unix())
	return err
}
func (store *Store) SetManual(ctx context.Context, node string, until int64) error {
	_, err := store.db.ExecContext(ctx, "UPDATE control SET mode='manual',manual_node=?,manual_until=?,updated_at=? WHERE id=1", node, until, store.now().Unix())
	return err
}

func (store *Store) List(ctx context.Context, includeSecret bool) ([]subscriptions.Subscription, error) {
	rows, err := store.db.QueryContext(ctx, subscriptionSelect+" ORDER BY group_name,created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []subscriptions.Subscription{}
	for rows.Next() {
		item, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		if !includeSecret {
			item.Secret = ""
			item.SecretConfigured = true
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) Get(ctx context.Context, id string, includeSecret bool) (*subscriptions.Subscription, error) {
	item, err := scanSubscription(store.db.QueryRowContext(ctx, subscriptionSelect+" WHERE id=?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !includeSecret {
		item.Secret = ""
		item.SecretConfigured = true
	}
	return &item, nil
}

func (store *Store) Create(ctx context.Context, item subscriptions.Subscription) (*subscriptions.Subscription, error) {
	if item.ID == "" {
		var err error
		item.ID, err = randomID()
		if err != nil {
			return nil, err
		}
	}
	now := store.now().Unix()
	if item.IntervalSeconds == 0 {
		item.IntervalSeconds = 3600
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO subscriptions(id,name,group_name,parser,secret,enabled,interval_seconds,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Name, item.GroupName, item.Parser, item.Secret, boolInt(item.Enabled), item.IntervalSeconds, now, now)
	if err != nil {
		return nil, err
	}
	return store.Get(ctx, item.ID, true)
}

func (store *Store) Update(ctx context.Context, id string, changes map[string]any) (*subscriptions.Subscription, error) {
	allowed := map[string]bool{"name": true, "group_name": true, "parser": true, "secret": true, "enabled": true, "interval_seconds": true}
	assignments := []string{}
	values := []any{}
	reset := false
	for _, key := range []string{"name", "group_name", "parser", "secret", "enabled", "interval_seconds"} {
		value, ok := changes[key]
		if !ok || !allowed[key] {
			continue
		}
		if key == "enabled" {
			if enabled, ok := value.(bool); ok {
				value = boolInt(enabled)
			}
		}
		if key == "group_name" || key == "parser" || key == "secret" {
			reset = true
		}
		assignments = append(assignments, key+"=?")
		values = append(values, value)
	}
	if len(assignments) == 0 {
		return store.Get(ctx, id, true)
	}
	assignments = append(assignments, "updated_at=?")
	values = append(values, store.now().Unix())
	if reset {
		assignments = append(assignments, "last_success=0", "last_status='pending'", "last_error=NULL", "last_links=0", "last_tested=0", "last_available=0")
	}
	values = append(values, id)
	result, err := store.db.ExecContext(ctx, "UPDATE subscriptions SET "+strings.Join(assignments, ",")+" WHERE id=?", values...)
	if err != nil {
		return nil, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, nil
	}
	return store.Get(ctx, id, true)
}

func (store *Store) Delete(ctx context.Context, id string) (bool, error) {
	result, err := store.db.ExecContext(ctx, "DELETE FROM subscriptions WHERE id=?", id)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows != 0, err
}

func (store *Store) UpdateSubscriptionStatus(ctx context.Context, id, status string, links *int, statusError *string, success bool) error {
	now := store.now().Unix()
	fields := []string{"last_attempt=?", "last_status=?", "last_error=?", "updated_at=?"}
	values := []any{now, status, statusError, now}
	if links != nil {
		fields = append(fields, "last_links=?")
		values = append(values, *links)
	}
	if success {
		fields = append(fields, "last_success=?")
		values = append(values, now)
	}
	values = append(values, id)
	_, err := store.db.ExecContext(ctx, "UPDATE subscriptions SET "+strings.Join(fields, ",")+" WHERE id=?", values...)
	return err
}

func (store *Store) UpdateSubscriptionQualification(ctx context.Context, id, status string, tested, available int, statusError *string) error {
	now := store.now().Unix()
	_, err := store.db.ExecContext(ctx, `UPDATE subscriptions
SET last_attempt=?,last_status=?,last_error=?,last_tested=?,last_available=?,updated_at=? WHERE id=?`,
		now, status, statusError, tested, available, now, id)
	return err
}

const subscriptionSelect = `SELECT id,name,group_name,parser,secret,enabled,interval_seconds,last_attempt,last_success,last_status,last_error,last_links,last_tested,last_available,created_at,updated_at FROM subscriptions`

type scanner interface{ Scan(...any) error }

func scanSubscription(row scanner) (subscriptions.Subscription, error) {
	var item subscriptions.Subscription
	var enabled int
	var lastError sql.NullString
	err := row.Scan(&item.ID, &item.Name, &item.GroupName, &item.Parser, &item.Secret, &enabled, &item.IntervalSeconds, &item.LastAttempt, &item.LastSuccess, &item.LastStatus, &lastError, &item.LastLinks, &item.LastTested, &item.LastAvailable, &item.CreatedAt, &item.UpdatedAt)
	item.Enabled = enabled != 0
	item.LastError = nullable(lastError)
	return item, err
}
func nullable(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func randomID() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "sub-" + hex.EncodeToString(value), nil
}
