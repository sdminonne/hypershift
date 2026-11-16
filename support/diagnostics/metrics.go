package diagnostics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var DiagnosticFallbackTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "hypershift_diagnostic_fallback_total",
		Help: "Number of times platform-specific diagnostic enrichment fell back to generic message due to missing or unparseable input.",
	},
	[]string{"platform", "condition", "case"},
)

// PlatformFailureTotal counts platform-specific failures classified during
// hosted cluster installation. Controllers increment this when they set a
// failure condition due to a platform API error (e.g. Azure
// RequestDisallowedByPolicy, AWS AccessDenied, CAPA InstanceProvisionFailed).
// The counter enables fleet-wide alerting and aggregation across management
// clusters.
var PlatformFailureTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "hypershift_install_platform_failure_total",
		Help: "Number of platform-specific failures classified during installation, labeled by condition, reason, and error code.",
	},
	[]string{"condition", "reason", "error_code"},
)

func init() {
	metrics.Registry.MustRegister(DiagnosticFallbackTotal)
	metrics.Registry.MustRegister(PlatformFailureTotal)
}

// RecordFallback increments the diagnostic fallback counter for a given
// platform, condition, and failure case. This is used to track when
// platform-specific diagnostic enrichment cannot produce a specific message
// and falls back to a generic one.
func RecordFallback(platform, condition, failureCase string) {
	DiagnosticFallbackTotal.WithLabelValues(platform, condition, failureCase).Inc()
}

// RecordPlatformFailure increments the fleet-wide platform failure counter
// and attaches a span event to the active OTel trace (if any). Call this
// when a controller classifies a platform-specific failure and sets a
// condition accordingly. When no tracer is configured the span event is
// a no-op.
func RecordPlatformFailure(ctx context.Context, condition, reason, errorCode string) {
	PlatformFailureTotal.WithLabelValues(condition, reason, errorCode).Inc()
	trace.SpanFromContext(ctx).AddEvent("platform.failure.classified",
		trace.WithAttributes(
			attribute.String("condition", condition),
			attribute.String("reason", reason),
			attribute.String("error_code", errorCode),
		),
	)
}
