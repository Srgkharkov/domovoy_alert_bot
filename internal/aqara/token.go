package aqara

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrNoToken means the bot was never authorized; run `domovoy auth`.
var ErrNoToken = errors.New("aqara: no token stored, run `domovoy auth request` first")

// Token is an Aqara OAuth token pair.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	OpenID       string    `json:"open_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// TokenStore persists tokens between restarts.
type TokenStore interface {
	Load() (*Token, error)
	Save(*Token) error
}

// FileTokenStore keeps the token in a JSON file (mounted Docker volume).
type FileTokenStore struct {
	Path string
}

func NewFileTokenStore(path string) *FileTokenStore {
	return &FileTokenStore{Path: path}
}

func (s *FileTokenStore) Load() (*Token, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoToken
	}
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.Path, err)
	}
	return &t, nil
}

// Save writes atomically (tmp file + rename) with 0600 permissions.
func (s *FileTokenStore) Save(t *Token) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}
