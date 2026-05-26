package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agentcli/pkg/llm"
)

type Session struct {
	ID        string        `json:"id"`
	CWD       string        `json:"cwd"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	Messages  []llm.Message `json:"messages"`
}

type Store struct {
	dir string
}

func NewStore(cwd string) (*Store, error) {
	root, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, ".agentcli", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) Load(id string) (Session, error) {
	id = cleanID(id)
	if id == "" {
		return Session{}, fmt.Errorf("session id cannot be empty")
	}
	data, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) LoadOrCreate(id, cwd string) (Session, error) {
	id = cleanID(id)
	if id == "" {
		id = time.Now().UTC().Format("20060102-150405")
	}
	session, err := s.Load(id)
	if err == nil {
		return session, nil
	}
	if !os.IsNotExist(err) {
		return Session{}, err
	}
	now := time.Now().UTC()
	return Session{ID: id, CWD: cwd, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) Save(session Session) error {
	session.ID = cleanID(session.ID)
	if session.ID == "" {
		return fmt.Errorf("session id cannot be empty")
	}
	session.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, session.ID+".json"), append(data, '\n'), 0o644)
}

func (s *Store) List() ([]Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

var unsafeID = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func cleanID(id string) string {
	id = strings.TrimSpace(id)
	id = unsafeID.ReplaceAllString(id, "-")
	return strings.Trim(id, ".-")
}
