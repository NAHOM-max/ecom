package temporal

import (
	"log"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// Worker wraps Temporal SDK worker
type Worker struct {
	worker worker.Worker
	client client.Client
}

// WorkerConfig contains worker configuration
type WorkerConfig struct {
	MaxConcurrentWorkflows  int
	MaxConcurrentActivities int
}

// NewWorker creates a new Temporal worker
func NewWorker(c client.Client, config *WorkerConfig) *Worker {
	if config.MaxConcurrentWorkflows == 0 {
		config.MaxConcurrentWorkflows = 100
	}
	if config.MaxConcurrentActivities == 0 {
		config.MaxConcurrentActivities = 100
	}

	w := worker.New(c, OrderFulfillmentTaskQueue, worker.Options{
		MaxConcurrentWorkflowTaskExecutionSize: config.MaxConcurrentWorkflows,
		MaxConcurrentActivityExecutionSize:     config.MaxConcurrentActivities,
	})

	log.Printf("Worker created for task queue: %s", OrderFulfillmentTaskQueue)

	return &Worker{
		worker: w,
		client: c,
	}
}

// RegisterWorkflow registers a workflow with the worker
func (w *Worker) RegisterWorkflow(workflow interface{}) {
	w.worker.RegisterWorkflow(workflow)
}

// RegisterActivity registers an activity with the worker
func (w *Worker) RegisterActivity(activity interface{}) {
	w.worker.RegisterActivity(activity)
}

// Start starts the worker
func (w *Worker) Start() error {
	log.Println("Starting Temporal worker...")
	return w.worker.Start()
}

// Stop gracefully stops the worker
func (w *Worker) Stop() {
	log.Println("Stopping Temporal worker...")
	w.worker.Stop()
	log.Println("Worker stopped")
}
