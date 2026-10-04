package controlplane

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const schemaVersion = 4
const schema = `
CREATE TABLE entities(kind TEXT NOT NULL,id TEXT NOT NULL,data TEXT NOT NULL,PRIMARY KEY(kind,id));
CREATE TABLE credentials(ref TEXT PRIMARY KEY,identity TEXT NOT NULL,account TEXT NOT NULL,account_kind TEXT NOT NULL DEFAULT 'account' CHECK(account_kind='account'),FOREIGN KEY(account_kind,account) REFERENCES entities(kind,id));
CREATE INDEX credential_identity ON credentials(identity);
CREATE TABLE key_secrets(hash TEXT PRIMARY KEY,id TEXT NOT NULL UNIQUE,key_kind TEXT NOT NULL DEFAULT 'key' CHECK(key_kind='key'),FOREIGN KEY(key_kind,id) REFERENCES entities(kind,id));
CREATE TABLE quota_history(id INTEGER PRIMARY KEY,account TEXT NOT NULL,at INTEGER NOT NULL,data TEXT NOT NULL);
CREATE INDEX quota_account_time ON quota_history(account,at);
CREATE TABLE affinity(hash TEXT PRIMARY KEY,account TEXT NOT NULL,hard INTEGER NOT NULL,expires INTEGER NOT NULL);
CREATE TABLE requests(id TEXT PRIMARY KEY,at INTEGER NOT NULL,provider TEXT,model TEXT,account TEXT,pool TEXT,api_key TEXT,status INTEGER,tokens INTEGER,latency INTEGER,data TEXT NOT NULL);
CREATE INDEX request_time ON requests(at DESC);
CREATE INDEX request_account_time ON requests(account,at DESC);
CREATE INDEX request_key_time ON requests(api_key,at DESC);
CREATE TABLE rollups(day TEXT NOT NULL,provider TEXT NOT NULL,model TEXT NOT NULL,account TEXT NOT NULL,pool TEXT NOT NULL,api_key TEXT NOT NULL,requests INTEGER NOT NULL,failures INTEGER NOT NULL,tokens INTEGER NOT NULL,latency INTEGER NOT NULL,cost REAL NOT NULL,PRIMARY KEY(day,provider,model,account,pool,api_key));
CREATE TABLE audit(id INTEGER PRIMARY KEY,at INTEGER NOT NULL,action TEXT NOT NULL,target TEXT NOT NULL);
CREATE TABLE decisions(id INTEGER PRIMARY KEY,at INTEGER NOT NULL,data TEXT NOT NULL);
CREATE TABLE warm_runs(id INTEGER PRIMARY KEY,at INTEGER NOT NULL,account TEXT NOT NULL,reason TEXT NOT NULL,result TEXT NOT NULL);
PRAGMA user_version=1;
`

const schemaV2 = `
CREATE INDEX request_provider_model_time ON requests(provider,model,at DESC);
CREATE INDEX rollup_day ON rollups(day);
CREATE INDEX decisions_time ON decisions(at);
CREATE INDEX affinity_expiration ON affinity(expires);
PRAGMA user_version=2;
`

const schemaV3 = `
CREATE TABLE settlements(id TEXT PRIMARY KEY);
INSERT INTO settlements(id) SELECT id FROM requests;
CREATE INDEX warm_account_time ON warm_runs(account,at DESC);
PRAGMA user_version=3;
`

const schemaV4 = `
ALTER TABLE warm_runs ADD COLUMN pool TEXT NOT NULL DEFAULT '';
CREATE INDEX warm_pool_time ON warm_runs(pool,at DESC);
CREATE TABLE executions(id TEXT PRIMARY KEY,at INTEGER NOT NULL,account TEXT NOT NULL,provider TEXT NOT NULL,model TEXT NOT NULL,api_key TEXT NOT NULL,state TEXT NOT NULL);
CREATE INDEX execution_state_time ON executions(state,at DESC);
PRAGMA user_version=4;
`

type snapshot struct {
	Accounts        map[string]Account
	Pools           map[string]Pool
	Keys            map[string]APIKey
	Secrets         map[string]string
	Credentials     map[string]string
	Quotas          map[string]QuotaWindow
	QuotasByAccount map[string][]QuotaWindow
	Settings        Settings
	Schedules       map[string]Schedule
	WarmAccounts    map[string]WarmAccount
}

type Store struct {
	db               *sql.DB
	path             string
	mu               sync.Mutex
	state            atomic.Pointer[snapshot]
	closed           atomic.Bool
	now              func() time.Time
	offsets          sync.Map
	flights          sync.Map
	accountingFailed atomic.Bool
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = "data/controlplane.sqlite"
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(abs, 0600); err != nil {
		return nil, err
	}
	u := &url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "synchronous(FULL)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: abs, now: func() time.Time { return time.Now().UTC() }}
	if err = s.migrate(); err == nil {
		_, err = s.db.Exec("UPDATE executions SET state='unresolved_restart' WHERE state='running'")
	}
	if err == nil {
		err = s.reload()
	}
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("control-plane open: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > schemaVersion {
		return errors.New("database schema newer than this binary")
	}
	if version == schemaVersion {
		return nil
	}
	var existingTables int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&existingTables); err != nil {
		return err
	}
	if version > 0 || existingTables > 0 {
		if _, err := s.Backup(); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if version < 1 {
		if _, err = tx.Exec(schema); err != nil {
			return err
		}
	}
	if version < 2 {
		if _, err = tx.Exec(schemaV2); err != nil {
			return err
		}
	}
	if version < 3 {
		if _, err = tx.Exec(schemaV3); err != nil {
			return err
		}
	}
	if version < 4 {
		if _, err = tx.Exec(schemaV4); err != nil {
			return err
		}
		if err = migrateWarmSchedules(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Backup uses SQLite's online snapshot, including committed WAL pages.
func (s *Store) Backup() (string, error) {
	path := s.path + ".backup-" + uuid.NewString() + ".sqlite"
	_, err := s.db.Exec("VACUUM INTO ?", path)
	if err == nil {
		err = os.Chmod(path, 0600)
	}
	return filepath.Base(path), err
}

// Compact is operator-triggered: VACUUM temporarily serializes database access.
// Keep an online backup before reclaiming pages; rollback never depends on optimism.
func (s *Store) Compact() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return "", errors.New("control plane closed")
	}
	backup, err := s.Backup()
	if err != nil {
		return "", err
	}
	if _, err = s.db.Exec("VACUUM"); err != nil {
		return backup, err
	}
	_, err = s.db.Exec("INSERT INTO audit(at,action,target) VALUES(?,?,?)", s.now().Unix(), "maintenance.compact", backup)
	return backup, err
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed.Store(true)
	return s.db.Close()
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func encode(v any) (string, error) { b, err := json.Marshal(v); return string(b), err }

func put(tx *sql.Tx, kind, id string, v any) error {
	data, err := encode(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO entities(kind,id,data) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET data=excluded.data", kind, id, data)
	return err
}

func (s *Store) mutate(action, target string, fn func(*sql.Tx) error) error {
	return s.transaction(action, target, fn, s.reload)
}

func (s *Store) Audit(action, target string) error {
	return s.transaction(action, target, func(*sql.Tx) error { return nil }, func() error { return nil })
}

// Hot-path accounting only refreshes changed entities, not every quota and credential.
// The previous snapshot remains immutable for concurrent routing readers.
func (s *Store) mutateAccounting(fn func(*sql.Tx) error, accounts map[string]Account, keyUpdates map[string]APIKey) error {
	return s.transaction("", "", fn, func() error {
		st := *s.state.Load()
		if len(accounts) > 0 {
			st.Accounts = maps.Clone(st.Accounts)
			maps.Copy(st.Accounts, accounts)
		}
		if len(keyUpdates) > 0 {
			st.Keys = maps.Clone(st.Keys)
			maps.Copy(st.Keys, keyUpdates)
		}
		s.state.Store(&st)
		return nil
	})
}

func (s *Store) transaction(action, target string, fn func(*sql.Tx) error, refresh func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return errors.New("control plane closed")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = fn(tx); err != nil {
		return err
	}
	if action != "" {
		if _, err = tx.Exec("INSERT INTO audit(at,action,target) VALUES(?,?,?)", s.now().Unix(), action, target); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return refresh()
}

func (s *Store) reload() error {
	st := &snapshot{Accounts: map[string]Account{}, Pools: map[string]Pool{}, Keys: map[string]APIKey{}, Secrets: map[string]string{}, Credentials: map[string]string{}, Quotas: map[string]QuotaWindow{}, Settings: Settings{Strategy: "native", QuotaFreshSeconds: 900, StickySeconds: 3600, FailurePenalty: 1, InFlightPenalty: 1}}
	st.QuotasByAccount = make(map[string][]QuotaWindow)
	st.Schedules = make(map[string]Schedule)
	st.WarmAccounts = make(map[string]WarmAccount)
	rows, err := s.db.Query("SELECT kind,id,data FROM entities")
	if err != nil {
		return err
	}
	for rows.Next() {
		var kind, id, data string
		if err = rows.Scan(&kind, &id, &data); err != nil {
			break
		}
		switch kind {
		case "account":
			var v Account
			err = json.Unmarshal([]byte(data), &v)
			st.Accounts[id] = v
		case "pool":
			var v Pool
			err = json.Unmarshal([]byte(data), &v)
			normalizePool(&v)
			st.Pools[id] = v
		case "key":
			var v APIKey
			err = json.Unmarshal([]byte(data), &v)
			normalizeKey(&v)
			st.Keys[id] = v
		case "quota":
			var v QuotaWindow
			err = json.Unmarshal([]byte(data), &v)
			st.Quotas[id] = v
			st.QuotasByAccount[v.Account] = append(st.QuotasByAccount[v.Account], v)
		case "settings":
			err = json.Unmarshal([]byte(data), &st.Settings)
		case "schedule":
			var v Schedule
			err = json.Unmarshal([]byte(data), &v)
			st.Schedules[id] = v
		case "warm_account":
			var v WarmAccount
			err = json.Unmarshal([]byte(data), &v)
			st.WarmAccounts[id] = v
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return err
	}
	rows, err = s.db.Query("SELECT ref,account FROM credentials")
	if err != nil {
		return err
	}
	for rows.Next() {
		var ref, id string
		if err = rows.Scan(&ref, &id); err != nil {
			break
		}
		st.Credentials[ref] = id
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return err
	}
	rows, err = s.db.Query("SELECT hash,id FROM key_secrets")
	if err != nil {
		return err
	}
	for rows.Next() {
		var h, id string
		if err = rows.Scan(&h, &id); err != nil {
			break
		}
		st.Secrets[h] = id
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return err
	}
	s.state.Store(st)
	return nil
}

func (s *Store) entities(ctx context.Context, kind string) ([]json.RawMessage, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM entities WHERE kind=? ORDER BY id", kind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []json.RawMessage{}
	for rows.Next() {
		var data string
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(data))
	}
	return out, rows.Err()
}
