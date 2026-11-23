package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrInvalidKey = errors.New("invalid or revoked API key")

const tokenBytes = 32

type Key struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Hash       string     `json:"hash"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

type MintResult struct {
	Key   Key
	Token string
}

type Service struct {
	mu    sync.RWMutex
	store *store
}

func NewService(path string) (*Service, error) {
	s, err := newStore(path)
	if err != nil {
		return nil, err
	}
	return &Service{store: s}, nil
}

func (svc *Service) Mint(_ context.Context, name string) (MintResult, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return MintResult{}, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])

	id := newID("key")
	prefix := token[:8]
	k := Key{
		ID:        id,
		Name:      name,
		Prefix:    prefix,
		Hash:      hash,
		CreatedAt: time.Now(),
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if err := svc.store.put(k); err != nil {
		return MintResult{}, err
	}
	return MintResult{Key: k, Token: token}, nil
}

func (svc *Service) Revoke(_ context.Context, id string) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	k, err := svc.store.getByID(id)
	if err != nil {
		return ErrInvalidKey
	}
	now := time.Now()
	k.RevokedAt = &now
	return svc.store.put(k)
}

func (svc *Service) Authenticate(_ context.Context, token string) (Key, error) {
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])

	svc.mu.Lock()
	defer svc.mu.Unlock()
	k, err := svc.store.getByHash(hash)
	if err != nil || k.RevokedAt != nil {
		return Key{}, ErrInvalidKey
	}
	now := time.Now()
	k.LastUsedAt = &now
	_ = svc.store.put(k)
	return k, nil
}

func (svc *Service) List(_ context.Context) ([]Key, error) {
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	return svc.store.list(), nil
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}
