package diagnostics

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordFallback(t *testing.T) {
	// Reset counters for deterministic test results.
	DiagnosticFallbackTotal.Reset()

	// Verify RecordFallback increments without panicking.
	RecordFallback("azure", "PLS", "status_code_fallback")
	RecordFallback("aws", "ValidAWSIdentityProvider", "missing_role_arn")
	RecordFallback("azure", "PLS", "status_code_fallback") // second increment

	azureCounter := DiagnosticFallbackTotal.WithLabelValues("azure", "PLS", "status_code_fallback")
	if got := testutil.ToFloat64(azureCounter); got != 2 {
		t.Errorf("expected azure counter value 2, got %v", got)
	}

	awsCounter := DiagnosticFallbackTotal.WithLabelValues("aws", "ValidAWSIdentityProvider", "missing_role_arn")
	if got := testutil.ToFloat64(awsCounter); got != 1 {
		t.Errorf("expected aws counter value 1, got %v", got)
	}
}

func TestRecordPlatformFailure(t *testing.T) {
	PlatformFailureTotal.Reset()

	ctx := context.Background()
	RecordPlatformFailure(ctx, "AzurePLSCreated", "ReconcileError", "RequestDisallowedByPolicy")
	RecordPlatformFailure(ctx, "ValidAWSIdentityProvider", "InvalidIdentityProvider", "AccessDenied")
	RecordPlatformFailure(ctx, "AzurePLSCreated", "ReconcileError", "RequestDisallowedByPolicy") // second increment

	azureCounter := PlatformFailureTotal.WithLabelValues("AzurePLSCreated", "ReconcileError", "RequestDisallowedByPolicy")
	if got := testutil.ToFloat64(azureCounter); got != 2 {
		t.Errorf("expected AzurePLSCreated counter value 2, got %v", got)
	}

	awsCounter := PlatformFailureTotal.WithLabelValues("ValidAWSIdentityProvider", "InvalidIdentityProvider", "AccessDenied")
	if got := testutil.ToFloat64(awsCounter); got != 1 {
		t.Errorf("expected ValidAWSIdentityProvider counter value 1, got %v", got)
	}
}
