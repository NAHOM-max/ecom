package workflows

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/testsuite"

	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/activities"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/queries"
	"github.com/yourorg/order-fulfillment-temporal-demo/internal/application/updates"
)

// priorityUpdateCallbacks implements internal.UpdateCallbacks via structural typing.
// env.UpdateWorkflow accepts any type with Accept(), Reject(error), Complete(interface{}, error)
// — no import of go.temporal.io/sdk/internal required.
type priorityUpdateCallbacks struct {
	onAccept   func()
	onReject   func(error)
	onComplete func(interface{}, error)
}

func (c *priorityUpdateCallbacks) Accept()                            { c.onAccept() }
func (c *priorityUpdateCallbacks) Reject(err error)                  { c.onReject(err) }
func (c *priorityUpdateCallbacks) Complete(v interface{}, err error) { c.onComplete(v, err) }

// sendPriorityUpdate delivers a set_priority update and returns the decoded result.
// Must be called from inside a RegisterDelayedCallback with delay > 0 so the first
// workflow task has already run and registered the update handler.
func sendPriorityUpdate(t *testing.T, env *testsuite.TestWorkflowEnvironment, input updates.SetPriorityInput) updates.SetPriorityResult {
	t.Helper()
	var result updates.SetPriorityResult
	env.UpdateWorkflow(updates.SetPriorityUpdate, "", &priorityUpdateCallbacks{
		onAccept: func() {},
		onReject: func(err error) { t.Fatalf("set_priority update rejected: %v", err) },
		onComplete: func(v interface{}, err error) {
			if err != nil {
				t.Fatalf("set_priority update failed: %v", err)
			}
			// The SDK passes a converter.EncodedValue to Complete, not the decoded struct.
			type decodable interface {
				Get(valuePtr interface{}) error
			}
			if enc, ok := v.(decodable); ok {
				if err := enc.Get(&result); err != nil {
					t.Fatalf("failed to decode set_priority result: %v", err)
				}
				return
			}
			switch r := v.(type) {
			case updates.SetPriorityResult:
				result = r
			case *updates.SetPriorityResult:
				result = *r
			}
		},
	}, input)
	return result
}

// mockSuccessfulOrder registers the three activity mocks for a happy-path run.
func mockSuccessfulOrder(env *testsuite.TestWorkflowEnvironment) {
	inv := activities.NewInventoryActivity(0.0)
	pay := activities.NewPaymentActivity(0.0)
	ship := activities.NewShippingActivity(0.0)
	env.OnActivity(inv.ReserveInventory, mock.Anything, mock.Anything).Return(
		&activities.ReserveInventoryResult{ReservationID: "res-1", Success: true}, nil)
	env.OnActivity(pay.ChargePayment, mock.Anything, mock.Anything).Return(
		&activities.ChargePaymentResult{PaymentID: "pay-1", Status: "charged"}, nil)
	env.OnActivity(ship.CreateShipment, mock.Anything, mock.Anything).Return(
		&activities.CreateShipmentResult{ShipmentID: "ship-1", TrackingNumber: "TRK1", Carrier: "UPS", Success: true}, nil)
}

func queryPriority(t *testing.T, env *testsuite.TestWorkflowEnvironment) string {
	t.Helper()
	val, err := env.QueryWorkflow(queries.OrderStatusQuery)
	if err != nil {
		t.Fatalf("order_status query failed: %v", err)
	}
	var s queries.OrderStatusResult
	if err := val.Get(&s); err != nil {
		t.Fatalf("decode order_status failed: %v", err)
	}
	return s.Priority
}

func defaultOrderInput() OrderWorkflowInput {
	return OrderWorkflowInput{
		OrderID:    "order-1",
		CustomerID: "cust-1",
		Items:      []OrderItemInput{{ProductID: "p1", Quantity: 1, Price: 10}},
	}
}

// TestOrderWorkflow_SetPriority_DefaultIsNormal confirms that a workflow that
// completes without any update reports NORMAL priority.
func TestOrderWorkflow_SetPriority_DefaultIsNormal(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ShipmentWorkflow)
	mockSuccessfulOrder(env)

	env.ExecuteWorkflow(OrderWorkflow, defaultOrderInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	got := queryPriority(t, env)
	if got != string(updates.PriorityNormal) {
		t.Errorf("expected default priority NORMAL, got %s", got)
	}
	t.Logf("✅ Default priority is NORMAL: %s", got)
}

// TestOrderWorkflow_SetPriority_UpdatePersisted sends a HIGH priority update and
// confirms the new value is visible via the order_status query after completion.
func TestOrderWorkflow_SetPriority_UpdatePersisted(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ShipmentWorkflow)
	mockSuccessfulOrder(env)

	// Deliver after the first workflow task so the update handler is registered.
	env.RegisterDelayedCallback(func() {
		sendPriorityUpdate(t, env, updates.SetPriorityInput{
			Priority:  updates.PriorityHigh,
			UpdatedBy: "ops-team",
		})
	}, time.Millisecond)

	env.ExecuteWorkflow(OrderWorkflow, defaultOrderInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}

	// Verify the update was persisted in workflow state via the order_status query.
	got := queryPriority(t, env)
	if got != string(updates.PriorityHigh) {
		t.Errorf("expected persisted priority HIGH, got %s", got)
	}
	t.Logf("✅ Priority updated and persisted in state: %s", got)
}

// TestOrderWorkflow_SetPriority_AllValidValues confirms LOW, NORMAL, and HIGH
// are each accepted and persisted correctly.
func TestOrderWorkflow_SetPriority_AllValidValues(t *testing.T) {
	for _, p := range []updates.OrderPriority{
		updates.PriorityLow,
		updates.PriorityNormal,
		updates.PriorityHigh,
	} {
		p := p
		t.Run(string(p), func(t *testing.T) {
			testSuite := &testsuite.WorkflowTestSuite{}
			env := testSuite.NewTestWorkflowEnvironment()
			env.RegisterWorkflow(ShipmentWorkflow)
			mockSuccessfulOrder(env)

			env.RegisterDelayedCallback(func() {
				sendPriorityUpdate(t, env, updates.SetPriorityInput{Priority: p, UpdatedBy: "test"})
			}, time.Millisecond)

			env.ExecuteWorkflow(OrderWorkflow, defaultOrderInput())

			if !env.IsWorkflowCompleted() {
				t.Fatal("workflow did not complete")
			}

			got := queryPriority(t, env)
			if got != string(p) {
				t.Errorf("expected priority %s, got %s", p, got)
			}
			t.Logf("✅ Priority %s accepted and persisted", p)
		})
	}
}

// TestOrderWorkflow_SetPriority_InvalidValueRejected confirms that an unknown
// priority string is rejected by the validator before the handler runs,
// leaving state unchanged.
func TestOrderWorkflow_SetPriority_InvalidValueRejected(t *testing.T) {
	testSuite := &testsuite.WorkflowTestSuite{}
	env := testSuite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(ShipmentWorkflow)
	mockSuccessfulOrder(env)

	rejected := false
	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflow(updates.SetPriorityUpdate, "", &priorityUpdateCallbacks{
			onAccept: func() { t.Error("expected update to be rejected, but it was accepted") },
			onReject: func(err error) {
				rejected = true
				t.Logf("✅ Invalid priority correctly rejected: %v", err)
			},
			onComplete: func(v interface{}, err error) {},
		}, updates.SetPriorityInput{Priority: "URGENT", UpdatedBy: "test"})
	}, time.Millisecond)

	env.ExecuteWorkflow(OrderWorkflow, defaultOrderInput())

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if !rejected {
		t.Error("expected invalid priority URGENT to be rejected by validator")
	}

	// State must remain at the default after a rejected update.
	got := queryPriority(t, env)
	if got != string(updates.PriorityNormal) {
		t.Errorf("expected priority unchanged at NORMAL after rejection, got %s", got)
	}
}
