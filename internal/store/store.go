// Package store keeps one JSON file per diagnosis and an append-only audit
// log in JSON lines. It stores only what the model package holds, which is
// redacted text; it has no way to receive raw tool output or secrets.
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"hody/internal/model"
)

// Store writes under one directory. It is safe for concurrent use.
type Store struct {
	dir string
	mu  sync.Mutex
}

// Entry is one audit log line. Detail never holds tool output.
type Entry struct {
	Time    time.Time         `json:"time"`
	Session string            `json:"session"`
	Event   string            `json:"event"`
	Detail  map[string]string `json:"detail,omitempty"`
}

var idRE = regexp.MustCompile(`^[a-f0-9]{16}$`)

// Open creates the directory if needed.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir is the store's root directory.
func (s *Store) Dir() string { return s.dir }

// Save writes the session file atomically.
func (s *Store) Save(sess *model.Session) error {
	if !idRE.MatchString(sess.ID) {
		return errors.New("store: invalid session id")
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.dir, "sessions", sess.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads one session.
func (s *Store) Load(id string) (*model.Session, error) {
	if !idRE.MatchString(id) {
		return nil, errors.New("store: invalid session id")
	}
	data, err := os.ReadFile(filepath.Join(s.dir, "sessions", id+".json"))
	if err != nil {
		return nil, err
	}
	var sess model.Session
	return &sess, json.Unmarshal(data, &sess)
}

// List returns stored session IDs, newest file first.
func (s *Store) List() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(s.dir, "sessions", "*.json"))
	if err != nil {
		return nil, err
	}
	type f struct {
		id string
		t  time.Time
	}
	var fs []f
	for _, p := range files {
		if st, err := os.Stat(p); err == nil {
			fs = append(fs, f{filepath.Base(p[:len(p)-5]), st.ModTime()})
		}
	}
	sort.Slice(fs, func(i, j int) bool { return fs[i].t.After(fs[j].t) })
	ids := make([]string, len(fs))
	for i := range fs {
		ids[i] = fs[i].id
	}
	return ids, nil
}

// Audit appends one line to audit.jsonl.
func (s *Store) Audit(session, event string, detail map[string]string) error {
	line, err := json.Marshal(Entry{Time: time.Now().UTC(), Session: session, Event: event, Detail: detail})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, "audit.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
