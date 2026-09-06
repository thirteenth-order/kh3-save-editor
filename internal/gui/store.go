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

// clear forgets every remembered folder and reports how many there were.
//
// Removing them one at a time is the only way there was, which is fine for a
// folder added by mistake and useless for starting over: the autodetected ones
// have no remove button at all, because they come back on the next scan, so a
// list that has drifted cannot be reset by hand. This empties the file the
// store is kept in; nothing on disk near a save is touched.
func (s *store) clear() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.Paths)
	if n == 0 {
		return 0
	}
	s.Paths = nil
	s.save()
	return n
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
