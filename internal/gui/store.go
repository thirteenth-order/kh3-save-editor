package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// store remembers folders the user pointed us at, so a save outside the
// standard locations only has to be found once.
type store struct {
	mu    sync.RWMutex
	path  string
	Paths []string `json:"paths"`
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "kh3save", "folders.json")
}

func loadStore() *store {
	s := &store{path: configPath()}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return s
	}
	_ = json.Unmarshal(data, s)
	return s
}

func (s *store) list() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]string(nil), s.Paths...)
	sort.Strings(out)
	return out
}

func (s *store) add(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.Paths {
		if e == p {
			return
		}
	}
	s.Paths = append(s.Paths, p)
	s.save()
}

func (s *store) remove(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Paths[:0]
	for _, e := range s.Paths {
		if e != p {
			out = append(out, e)
		}
	}
	s.Paths = out
	s.save()
}

// save assumes the caller holds the lock.
func (s *store) save() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, data, 0o644)
}
