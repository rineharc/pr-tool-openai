package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/rineharc/pr-tool-openai/internal/review"
)

const currentVersion = 1

type PRState struct {
	ViewedFiles map[string]bool       `json:"viewedFiles,omitempty"`
	Drafts      []review.DraftComment `json:"drafts,omitempty"`
	LastFile    string                `json:"lastFile,omitempty"`
}

type diskState struct {
	Version int                `json:"version"`
	PRs     map[string]PRState `json:"pullRequests"`
}

type Store struct {
	mu    sync.Mutex
	path  string
	state diskState
}

func DefaultPath() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "prr", "state.json"), nil
}

func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	s := &Store{path: path, state: diskState{Version: currentVersion, PRs: map[string]PRState{}}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	if s.state.Version != currentVersion {
		return nil, fmt.Errorf("unsupported state version %d", s.state.Version)
	}
	if s.state.PRs == nil {
		s.state.PRs = map[string]PRState{}
	}
	return s, nil
}

func (s *Store) Load(key string) PRState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state.PRs[key]
	return clone(state)
}

func (s *Store) Save(key string, state PRState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state.ViewedFiles == nil {
		state.ViewedFiles = map[string]bool{}
	}
	s.state.PRs[key] = clone(state)
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(s.path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, s.path)
}

func clone(state PRState) PRState {
	result := PRState{LastFile: state.LastFile, Drafts: append([]review.DraftComment(nil), state.Drafts...)}
	result.ViewedFiles = make(map[string]bool, len(state.ViewedFiles))
	for path, viewed := range state.ViewedFiles {
		result.ViewedFiles[path] = viewed
	}
	return result
}
