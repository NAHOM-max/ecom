package domain

import "context"

// InventoryService defines the contract for inventory reservation operations.
type InventoryService interface {
	Reserve(ctx context.Context, orderID string, items []ReserveItem) (*ReserveResponse, error)
	Release(ctx context.Context, reservationID string) error
}

// ReserveItem is a single product line in a reservation request.
type ReserveItem struct {
	ProductID string
	Quantity  int
}

// ReserveResponse is returned by a successful inventory reservation.
type ReserveResponse struct {
	ReservationID string
}
