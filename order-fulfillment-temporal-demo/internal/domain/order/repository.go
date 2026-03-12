package order

import "context"

// Repository defines the interface for order persistence
// This is a domain interface (port) implemented in infrastructure layer
// Following Dependency Inversion Principle

type Repository interface {
	// Save persists a new order
	Save(ctx context.Context, order *Order) error

	// GetByID retrieves an order by its ID
	GetByID(ctx context.Context, id string) (*Order, error)

	// Update modifies an existing order
	Update(ctx context.Context, order *Order) error
}
