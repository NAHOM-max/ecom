package idempotency

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// MemoryStore is a thread-safe in-memory IdempotencyStore.
// It is suitable for development, tests, and single-instance deployments.
// Replace with RedisStore or PostgresStore for multi-instance production use.
type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]Record
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: make(map[string]Record)}
}

// Exists returns true when a completed record exists for key.
func (s *MemoryStore) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.records[key]
	return ok, nil
}

// Save persists result under key. First write wins — subsequent calls are no-ops.
func (s *MemoryStore) Save(ctx context.Context, key string, result interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// First write wins — never overwrite a committed record.
	if _, exists := s.records[key]; exists {
		return nil
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}

	s.records[key] = Record{
		Key:         key,
		CompletedAt: time.Now(),
		Result:      raw,
	}
	return nil
}

// Load retrieves the saved result into dst.
func (s *MemoryStore) Load(ctx context.Context, key string, dst interface{}) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec, ok := s.records[key]
	if !ok {
		return ErrNotFound{Key: key}
	}
	return json.Unmarshal(rec.Result, dst)
}
