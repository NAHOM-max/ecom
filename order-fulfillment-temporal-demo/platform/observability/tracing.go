package observability

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

const tracerName = "order-fulfillment"

// ---------------------------------------------------------------------------
// Provider setup
// ---------------------------------------------------------------------------

// InitTracer configures the global OTel tracer provider.
// endpoint is the OTLP HTTP collector address, e.g. "localhost:4318".
// Returns a shutdown function that must be called on process exit.
func InitTracer(serviceName, endpoint string) (func(context.Context) error, error) {
	exp, err := otlptracehttp.New(context.Background(),
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTel resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// Tracer returns the package-level tracer.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// ---------------------------------------------------------------------------
// HTTP middleware
// ---------------------------------------------------------------------------

// TraceHTTP wraps an http.Handler with OTel tracing.
// Each request gets a span named "<METHOD> <path>".
func TraceHTTP(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

// ---------------------------------------------------------------------------
// Temporal worker interceptor — propagates trace context across activities
// ---------------------------------------------------------------------------

// TracingInterceptor implements interceptor.WorkerInterceptor.
type TracingInterceptor struct {
	interceptor.WorkerInterceptorBase
}

func NewTracingInterceptor() *TracingInterceptor {
	return &TracingInterceptor{}
}

func (t *TracingInterceptor) InterceptActivity(
	ctx context.Context,
	next interceptor.ActivityInboundInterceptor,
) interceptor.ActivityInboundInterceptor {
	return &tracingActivityInbound{ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next}}
}

func (t *TracingInterceptor) InterceptWorkflow(
	ctx workflow.Context,
	next interceptor.WorkflowInboundInterceptor,
) interceptor.WorkflowInboundInterceptor {
	return &tracingWorkflowInbound{WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next}}
}

// ---------------------------------------------------------------------------
// Activity inbound — starts a child span for each activity execution
// ---------------------------------------------------------------------------

type tracingActivityInbound struct {
	interceptor.ActivityInboundInterceptorBase
}

func (a *tracingActivityInbound) ExecuteActivity(
	ctx context.Context,
	in *interceptor.ExecuteActivityInput,
) (interface{}, error) {
	info := activity.GetInfo(ctx)

	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "activity."+info.ActivityType.Name,
		trace.WithAttributes(
			attribute.String("temporal.activity_type", info.ActivityType.Name),
			attribute.String("temporal.workflow_id", info.WorkflowExecution.ID),
			attribute.String("temporal.run_id", info.WorkflowExecution.RunID),
			attribute.Int("temporal.attempt", int(info.Attempt)),
		),
	)
	defer span.End()

	result, err := a.Next.ExecuteActivity(ctx, in)
	if err != nil {
		span.RecordError(err)
	}
	return result, err
}

// ---------------------------------------------------------------------------
// Workflow inbound — starts a root span for each workflow execution
// ---------------------------------------------------------------------------

type tracingWorkflowInbound struct {
	interceptor.WorkflowInboundInterceptorBase
}

func (w *tracingWorkflowInbound) ExecuteWorkflow(
	ctx workflow.Context,
	in *interceptor.ExecuteWorkflowInput,
) (interface{}, error) {
	info := workflow.GetInfo(ctx)

	// workflow.Context cannot carry a real context.Context, so we use the
	// background context to start the root span. The span is ended when the
	// workflow function returns.
	_, span := otel.Tracer(tracerName).Start(context.Background(),
		"workflow."+info.WorkflowType.Name,
		trace.WithAttributes(
			attribute.String("temporal.workflow_type", info.WorkflowType.Name),
			attribute.String("temporal.workflow_id", info.WorkflowExecution.ID),
			attribute.String("temporal.run_id", info.WorkflowExecution.RunID),
			attribute.String("temporal.namespace", info.Namespace),
		),
	)
	defer span.End()

	result, err := w.Next.ExecuteWorkflow(ctx, in)
	if err != nil {
		span.RecordError(err)
	}
	return result, err
}
