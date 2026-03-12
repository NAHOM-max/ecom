package order

import (
	"context"
	"errors"
)

// Service encapsulates business logic for order operations
// Pure domain service with no infrastructure dependencies
// Does NOT call Temporal - only coordinates domain objects

type Service struct {
	repo Repository
}

// NewService creates a new order domain service
func NewService(repo Repository) *Service {
	return &Service{
		repo: repo,
	}
}

// CreateOrder creates a new order with validation
func (s *Service) CreateOrder(ctx context.Context, customerID string, items []OrderItem) (*Order, error) {
	order, err := NewOrder(customerID, items)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Save(ctx, order); err != nil {
		return nil, err
	}

	return order, nil
}

// GetOrder retrieves an order by ID
func (s *Service) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	if orderID == "" {
		return nil, errors.New("order ID is required")
	}
	return s.repo.GetByID(ctx, orderID)
}

// ReserveInventory reserves inventory for an order
func (s *Service) ReserveInventory(ctx context.Context, orderID string) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}

	if err := order.ReserveInventory(); err != nil {
		return err
	}

	return s.repo.Update(ctx, order)
}

// MarkPaymentCharged marks payment as charged
func (s *Service) MarkPaymentCharged(ctx context.Context, orderID, paymentID string) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}

	if err := order.MarkPaymentCharged(paymentID); err != nil {
		return err
	}

	return s.repo.Update(ctx, order)
}

// StartShipment starts shipment for an order
func (s *Service) StartShipment(ctx context.Context, orderID, shipmentID string) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}

	if err := order.StartShipment(shipmentID); err != nil {
		return err
	}

	return s.repo.Update(ctx, order)
}

// CompleteOrder marks order as completed
func (s *Service) CompleteOrder(ctx context.Context, orderID string) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}

	if err := order.CompleteOrder(); err != nil {
		return err
	}

	return s.repo.Update(ctx, order)
}

// CancelOrder cancels an order
func (s *Service) CancelOrder(ctx context.Context, orderID string) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}

	if err := order.CancelOrder(); err != nil {
		return err
	}

	return s.repo.Update(ctx, order)
}

// MarkOrderFailed marks order as failed
func (s *Service) MarkOrderFailed(ctx context.Context, orderID string) error {
	order, err := s.repo.GetByID(ctx, orderID)
	if err != nil {
		return err
	}

	if err := order.MarkFailed(); err != nil {
		return err
	}

	return s.repo.Update(ctx, order)
}
