package config

import (
	"fmt"
	"os"

	toml "github.com/pelletier/go-toml"
)

// StateVersion is the persistent-state schema version. It evolves separately
// from the configuration version.
const StateVersion = 1

// State is transient-but-persistent facts TideGit remembers between runs. It is
// deliberately separate from configuration: wiping state must never change how
// the application behaves, and config readability is not a concern here.
type State struct {
	Version            int      `toml:"version"`
	LastRepository     string   `toml:"last_repository"`
	LastScreen         string   `toml:"last_screen"`
	RecentRepositories []string `toml:"recent_repositories"`
	DismissedHints     []string `toml:"dismissed_hints"`
}

// DefaultState is an empty, valid state.
func DefaultState() *State {
	return &State{Version: StateVersion}
}

// LoadState reads persisted state. Corruption is never fatal: the caller gets a
// fresh state alongside the error and can carry on.
func LoadState(path string) (*State, error) {
	state := DefaultState()
	if path == "" {
		return state, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	tree, err := toml.LoadBytes(data)
	if err != nil {
		return state, fmt.Errorf("state file %s is unreadable; starting fresh: %w", path, err)
	}
	if err := tree.Unmarshal(state); err != nil {
		return DefaultState(), fmt.Errorf("state file %s is malformed; starting fresh: %w", path, err)
	}
	if state.Version > StateVersion {
		return DefaultState(), fmt.Errorf("state version %d is newer than this build supports; starting fresh", state.Version)
	}
	state.Version = StateVersion
	return state, nil
}

// Save writes state atomically, so a crash cannot corrupt it.
func (s *State) Save(path string) error {
	if path == "" {
		return fmt.Errorf("no state path is available")
	}
	s.Version = StateVersion
	s.compact()
	data, err := toml.Marshal(struct {
		Version            int      `toml:"version"`
		LastRepository     string   `toml:"last_repository,omitempty"`
		LastScreen         string   `toml:"last_screen,omitempty"`
		RecentRepositories []string `toml:"recent_repositories,omitempty"`
		DismissedHints     []string `toml:"dismissed_hints,omitempty"`
	}{s.Version, s.LastRepository, s.LastScreen, s.RecentRepositories, s.DismissedHints})
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// compact removes empty entries so a corrupt or manually emptied list cannot
// accumulate junk.
func (s *State) compact() {
	s.RecentRepositories = dedupe(s.RecentRepositories)
	s.DismissedHints = dedupe(s.DismissedHints)
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	out := list[:0]
	for _, item := range list {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// TouchRepository moves a path to the front of the recent list, removing any
// earlier copy, and bounds the list. A limit of zero keeps nothing.
func (s *State) TouchRepository(path string, limit int) {
	if path == "" {
		return
	}
	s.LastRepository = path
	if limit <= 0 {
		s.RecentRepositories = nil
		return
	}
	next := []string{path}
	for _, existing := range s.RecentRepositories {
		if existing == path || existing == "" {
			continue
		}
		next = append(next, existing)
	}
	if len(next) > limit {
		next = next[:limit]
	}
	s.RecentRepositories = next
}

// RemoveRepository drops a path from both the recent list and the last-used
// slot.
func (s *State) RemoveRepository(path string) {
	out := s.RecentRepositories[:0]
	for _, existing := range s.RecentRepositories {
		if existing != path {
			out = append(out, existing)
		}
	}
	s.RecentRepositories = out
	if s.LastRepository == path {
		s.LastRepository = ""
		if len(out) > 0 {
			s.LastRepository = out[0]
		}
	}
}

// PruneRepositories keeps only paths that still exist, so a deleted repository
// does not linger. It returns how many were removed.
func (s *State) PruneRepositories() int {
	removed := 0
	out := s.RecentRepositories[:0]
	for _, path := range s.RecentRepositories {
		if _, err := os.Stat(path); err == nil {
			out = append(out, path)
		} else {
			removed++
		}
	}
	s.RecentRepositories = out
	if s.LastRepository != "" {
		if _, err := os.Stat(s.LastRepository); err != nil {
			s.LastRepository = ""
			if len(out) > 0 {
				s.LastRepository = out[0]
			}
		}
	}
	return removed
}
