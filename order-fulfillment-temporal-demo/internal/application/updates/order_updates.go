package updates

import "fmt"

// SetPriorityUpdate is the update name registered on OrderWorkflow.
const SetPriorityUpdate = "set_priority"

// OrderPriority is the type-safe priority value.
type OrderPriority string

const (
	PriorityLow    OrderPriority = "LOW"
	PriorityNormal OrderPriority = "NORMAL"
	PriorityHigh   OrderPriority = "HIGH"
)

// Validate returns an error when the priority value is not one of the allowed constants.
func (p OrderPriority) Validate() error {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh:
		return nil
	default:
		return fmt.Errorf("invalid priority %q: must be LOW, NORMAL, or HIGH", p)
	}
}

// SetPriorityInput is the argument sent with the set_priority update.
type SetPriorityInput struct {
	Priority  OrderPriority `json:"priority"`
	UpdatedBy string        `json:"updated_by"`
}

// SetPriorityResult is returned to the caller after the update is applied.
type SetPriorityResult struct {
	OrderID     string        `json:"order_id"`
	OldPriority OrderPriority `json:"old_priority"`
	NewPriority OrderPriority `json:"new_priority"`
}
