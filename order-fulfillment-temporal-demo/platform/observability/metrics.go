package observability

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

// ---------------------------------------------------------------------------
// Prometheus metric definitions
// ---------------------------------------------------------------------------

var (
	WorkflowStartedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workflow_started_total",
		Help: "Total number of workflow executions started.",
	}, []string{"workflow_type"})

	WorkflowCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workflow_completed_total",
		Help: "Total number of workflow executions completed successfully.",
	}, []string{"workflow_type"})

	WorkflowFailedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "workflow_failed_total",
		Help: "Total number of workflow executions that failed.",
	}, []string{"workflow_type"})

	ActivityExecutionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "activity_execution_total",
		Help: "Total number of activity executions attempted.",
	}, []string{"activity_type"})

	ActivityRetryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "activity_retry_total",
		Help: "Total number of activity retries (attempt > 1).",
	}, []string{"activity_type"})

	ActivityDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "activity_duration_seconds",
		Help:    "Activity execution duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"activity_type", "status"})

	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests received.",
	}, []string{"method", "path", "status"})

	HTTPDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

// ---------------------------------------------------------------------------
// Temporal worker interceptor — instruments workflows and activities
// ---------------------------------------------------------------------------

// MetricsInterceptor implements interceptor.WorkerInterceptor.
type MetricsInterceptor struct {
	interceptor.WorkerInterceptorBase
}

func NewMetricsInterceptor() *MetricsInterceptor {
	return &MetricsInterceptor{}
}

// InterceptActivity wraps each activity execution to record metrics.
func (m *MetricsInterceptor) InterceptActivity(
	ctx context.Context,
	next interceptor.ActivityInboundInterceptor,
) interceptor.ActivityInboundInterceptor {
	return &metricsActivityInbound{ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next}}
}

// InterceptWorkflow wraps each workflow execution to record metrics.
func (m *MetricsInterceptor) InterceptWorkflow(
	ctx workflow.Context,
	next interceptor.WorkflowInboundInterceptor,
) interceptor.WorkflowInboundInterceptor {
	return &metricsWorkflowInbound{WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next}}
}

// ---------------------------------------------------------------------------
// Activity inbound interceptor
// ---------------------------------------------------------------------------

type metricsActivityInbound struct {
	interceptor.ActivityInboundInterceptorBase
}

func (a *metricsActivityInbound) ExecuteActivity(
	ctx context.Context,
	in *interceptor.ExecuteActivityInput,
) (interface{}, error) {
	info := activity.GetInfo(ctx)
	actType := info.ActivityType.Name

	ActivityExecutionTotal.WithLabelValues(actType).Inc()

	// Attempt > 1 means this is a retry
	if info.Attempt > 1 {
		ActivityRetryTotal.WithLabelValues(actType).Inc()
	}

	start := time.Now()
	result, err := a.Next.ExecuteActivity(ctx, in)
	duration := time.Since(start).Seconds()

	status := "success"
	if err != nil {
		status = "failure"
	}
	ActivityDurationSeconds.WithLabelValues(actType, status).Observe(duration)

	return result, err
}

// ---------------------------------------------------------------------------
// Workflow inbound interceptor
// ---------------------------------------------------------------------------

type metricsWorkflowInbound struct {
	interceptor.WorkflowInboundInterceptorBase
}

func (w *metricsWorkflowInbound) ExecuteWorkflow(
	ctx workflow.Context,
	in *interceptor.ExecuteWorkflowInput,
) (interface{}, error) {
	wfType := workflow.GetInfo(ctx).WorkflowType.Name

	WorkflowStartedTotal.WithLabelValues(wfType).Inc()

	result, err := w.Next.ExecuteWorkflow(ctx, in)

	if err != nil {
		WorkflowFailedTotal.WithLabelValues(wfType).Inc()
	} else {
		WorkflowCompletedTotal.WithLabelValues(wfType).Inc()
	}

	return result, err
}
