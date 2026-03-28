package idempotency

import (
	"context"
	"encoding/json"
	"time"
)

// Store is the idempotency abstraction that activities depend on.
//
// Implementations:
//   - MemoryStore  — in-process map, used in development and tests
//   - RedisStore   — can be added later without changing any activity code
//   - PostgresStore — same
//
// Key convention used by activities:
//
//	"<activity-name>:<idempotency-key>"
//
// Example:
//
//	"ChargePayment:pay-act-abc123"
type Store interface {
	// Exists returns true when a record for key has already been committed.
	Exists(ctx context.Context, key string) (bool, error)

	// Save persists a completed result under key.
	// Calling Save on an existing key is a no-op (first write wins).
	Save(ctx context.Context, key string, result interface{}) error

	// Load retrieves the previously saved result into dst.
	// Returns ErrNotFound when the key does not exist.
	Load(ctx context.Context, key string, dst interface{}) error
}

// Record is the envelope stored for every completed operation.
type Record struct {
	Key         string          `json:"key"`
	CompletedAt time.Time       `json:"completed_at"`
	Result      json.RawMessage `json:"result"`
}

// ErrNotFound is returned by Load when the key has no record.
type ErrNotFound struct{ Key string }

func (e ErrNotFound) Error() string { return "idempotency: key not found: " + e.Key }
