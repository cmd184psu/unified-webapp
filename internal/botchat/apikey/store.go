package apikey

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var errNotFound = errors.New("not found")

type store struct {
	path string
	keys []Key
}

func newStore(path string) (*store, error) {
	s := &store{path: path, keys: []Key{}}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		var payload struct {
			Keys []Key `json:"keys"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("parse api keys file: %w", err)
		}
		s.keys = payload.Keys
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, fmt.Errorf("read api keys file: %w", err)
	}
	return s, nil
}

func (s *store) put(k Key) error {
	replaced := false
	for i := range s.keys {
		if s.keys[i].ID == k.ID {
			s.keys[i] = k
			replaced = true
			break
		}
	}
	if !replaced {
		s.keys = append(s.keys, k)
	}
	return s.save()
}

func (s *store) getByID(id string) (Key, error) {
	for _, k := range s.keys {
		if k.ID == id {
			return k, nil
		}
	}
	return Key{}, errNotFound
}

func (s *store) getByHash(hash string) (Key, error) {
	for _, k := range s.keys {
		if k.Hash == hash {
			return k, nil
		}
	}
	return Key{}, errNotFound
}

func (s *store) list() []Key {
	out := make([]Key, len(s.keys))
	copy(out, s.keys)
	return out
}

func (s *store) save() error {
	if s.path == "" {
		return nil
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create api keys dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp api keys file: %w", err)
	}
	tmpPath := tmp.Name()
	payload := struct {
		Keys []Key `json:"keys"`
	}{Keys: s.keys}
	if err := json.NewEncoder(tmp).Encode(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode api keys: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename api keys file: %w", err)
	}
	return nil
}
