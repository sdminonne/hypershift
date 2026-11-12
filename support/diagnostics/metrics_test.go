package diagnostics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordFallback(t *testing.T) {
	// Reset counters for deterministic test results.
	DiagnosticFallbackTotal.Reset()

	// Verify RecordFallback increments without panicking.
	RecordFallback("azure", "PLS", "2.8")
	RecordFallback("aws", "ValidAWSIdentityProvider", "3.1")
	RecordFallback("azure", "PLS", "2.8") // second increment

	azureCounter := DiagnosticFallbackTotal.WithLabelValues("azure", "PLS", "2.8")
	if got := testutil.ToFloat64(azureCounter); got != 2 {
		t.Errorf("expected azure counter value 2, got %v", got)
	}

	awsCounter := DiagnosticFallbackTotal.WithLabelValues("aws", "ValidAWSIdentityProvider", "3.1")
	if got := testutil.ToFloat64(awsCounter); got != 1 {
		t.Errorf("expected aws counter value 1, got %v", got)
	}
}
