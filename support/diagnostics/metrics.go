package diagnostics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var DiagnosticFallbackTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "hypershift_diagnostic_fallback_total",
		Help: "Number of times platform-specific diagnostic enrichment fell back to generic message due to missing or unparseable input.",
	},
	[]string{"platform", "condition", "case"},
)

func init() {
	metrics.Registry.MustRegister(DiagnosticFallbackTotal)
}

// RecordFallback increments the diagnostic fallback counter for a given
// platform, condition, and failure case. This is used to track when
// platform-specific diagnostic enrichment cannot produce a specific message
// and falls back to a generic one.
func RecordFallback(platform, condition, failureCase string) {
	DiagnosticFallbackTotal.WithLabelValues(platform, condition, failureCase).Inc()
}
