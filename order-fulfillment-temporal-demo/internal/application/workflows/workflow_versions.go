package workflows

// Versioning constants for OrderWorkflow.
//
// How Temporal GetVersion works:
//
//   v := workflow.GetVersion(ctx, changeID, minSupported, maxSupported)
//
//   - changeID   : a stable string that identifies this specific change point.
//                  Once recorded in history it never changes for that execution.
//   - minSupported: the oldest version this worker can still replay correctly.
//                  Set to workflow.DefaultVersion to allow replaying histories
//                  that pre-date this GetVersion call entirely.
//   - maxSupported: the newest version this worker knows about.
//
// On first execution GetVersion records maxSupported in the workflow history.
// On replay it reads the recorded value back — so old executions always get
// the version that was current when they started.
//
// Version history for OrderWorkflow:
//
//   workflow.DefaultVersion (-1)
//     Original flow: inventory → payment → shipment
//     No GetVersion marker in history (pre-dates versioning).
//
//   OrderWorkflowV2FraudCheck (1)
//     New flow:      inventory → fraud check → payment → shipment
//     Introduced GetVersion marker "fraud-check-between-inventory-and-payment".
const (
	// OrderWorkflowV2FraudCheck is the version that introduced the fraud-check
	// step between inventory reservation and payment charging.
	OrderWorkflowV2FraudCheck = 1

	// OrderWorkflowChangeIDFraudCheck is the stable change identifier passed to
	// workflow.GetVersion. It must never be renamed after it has been recorded
	// in any workflow history.
	OrderWorkflowChangeIDFraudCheck = "fraud-check-between-inventory-and-payment"
)
