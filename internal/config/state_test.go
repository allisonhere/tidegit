package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateRoundTripAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	state := DefaultState()
	state.TouchRepository("/a", 3)
	state.TouchRepository("/b", 3)
	state.TouchRepository("/c", 3)
	state.TouchRepository("/a", 3) // move to front
	if state.RecentRepositories[0] != "/a" || len(state.RecentRepositories) != 3 {
		t.Fatalf("order: %+v", state.RecentRepositories)
	}
	state.TouchRepository("/d", 3)
	if len(state.RecentRepositories) != 3 || state.RecentRepositories[0] != "/d" {
		t.Fatalf("bounding: %+v", state.RecentRepositories)
	}
	if state.LastRepository != "/d" {
		t.Fatalf("last repository: %q", state.LastRepository)
	}
	if err := state.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != StateVersion || loaded.RecentRepositories[0] != "/d" {
		t.Fatalf("round trip: %+v", loaded)
	}
}

func TestStateCorruptionFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	if err := os.WriteFile(path, []byte("this is not = = toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err == nil {
		t.Fatal("corrupt state did not report an error")
	}
	if state.Version != StateVersion || len(state.RecentRepositories) != 0 {
		t.Fatalf("corrupt state did not fall back: %+v", state)
	}
}

func TestStateFutureVersionFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.toml")
	if err := os.WriteFile(path, []byte("version = 99\nlast_repository = \"/x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := LoadState(path)
	if err == nil || state.LastRepository != "" {
		t.Fatalf("future state version: %+v %v", state, err)
	}
}

func TestStatePruneMissingRepositories(t *testing.T) {
	dir := t.TempDir()
	alive := filepath.Join(dir, "alive")
	if err := os.Mkdir(alive, 0o700); err != nil {
		t.Fatal(err)
	}
	state := DefaultState()
	state.TouchRepository(alive, 10)
	state.TouchRepository("/does/not/exist", 10)
	if removed := state.PruneRepositories(); removed != 1 {
		t.Fatalf("pruned %d, want 1", removed)
	}
	if len(state.RecentRepositories) != 1 || state.RecentRepositories[0] != alive {
		t.Fatalf("prune result: %+v", state.RecentRepositories)
	}
}

func TestStateRemoveRepository(t *testing.T) {
	state := DefaultState()
	state.TouchRepository("/a", 10)
	state.TouchRepository("/b", 10)
	state.RemoveRepository("/b")
	if state.LastRepository != "/a" || len(state.RecentRepositories) != 1 {
		t.Fatalf("remove: %+v", state)
	}
}

func TestStateMissingFileIsEmpty(t *testing.T) {
	state, err := LoadState(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || state.Version != StateVersion {
		t.Fatalf("missing state: %+v %v", state, err)
	}
}
