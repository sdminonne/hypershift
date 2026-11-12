package azureutil

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

// newReadCloser creates an io.ReadCloser from a string for test HTTP responses.
func newReadCloser(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

func TestParseRetryAfterDuration(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedOK     bool
		expectedMinDur time.Duration
		expectedMaxDur time.Duration
	}{
		{
			name:       "When error is nil it should return false",
			err:        nil,
			expectedOK: false,
		},
		{
			name:       "When error is not an Azure error it should return false",
			err:        fmt.Errorf("some random error"),
			expectedOK: false,
		},
		{
			name: "When RawResponse is nil it should return false",
			err: &azcore.ResponseError{
				StatusCode:  429,
				RawResponse: nil,
			},
			expectedOK: false,
		},
		{
			name: "When Retry-After header is absent it should return false",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{},
				},
			},
			expectedOK: false,
		},
		{
			name: "When Retry-After header is empty it should return false",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {""},
					},
				},
			},
			expectedOK: false,
		},
		{
			name: "When Retry-After is seconds it should parse correctly",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {"120"},
					},
				},
			},
			expectedOK:     true,
			expectedMinDur: 120 * time.Second,
			expectedMaxDur: 120 * time.Second,
		},
		{
			name: "When Retry-After is 30 seconds it should parse correctly",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {"30"},
					},
				},
			},
			expectedOK:     true,
			expectedMinDur: 30 * time.Second,
			expectedMaxDur: 30 * time.Second,
		},
		{
			name: "When Retry-After is zero seconds it should return false",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {"0"},
					},
				},
			},
			expectedOK: false,
		},
		{
			name: "When Retry-After is negative seconds it should return false",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {"-5"},
					},
				},
			},
			expectedOK: false,
		},
		{
			name: "When Retry-After is a future HTTP-date it should return positive duration",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)},
					},
				},
			},
			expectedOK:     true,
			expectedMinDur: 1 * time.Minute, // Allow some tolerance
			expectedMaxDur: 3 * time.Minute, // Allow some tolerance
		},
		{
			name: "When Retry-After is a past HTTP-date it should return false",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {time.Now().Add(-5 * time.Minute).UTC().Format(http.TimeFormat)},
					},
				},
			},
			expectedOK: false,
		},
		{
			name: "When Retry-After is an unparsable string it should return false",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {"not-a-number-or-date"},
					},
				},
			},
			expectedOK: false,
		},
		{
			name: "When error wraps an Azure ResponseError it should still parse",
			err: fmt.Errorf("outer error: %w", &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {"60"},
					},
				},
			}),
			expectedOK:     true,
			expectedMinDur: 60 * time.Second,
			expectedMaxDur: 60 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, ok := ParseRetryAfterDuration(tt.err)
			if ok != tt.expectedOK {
				t.Errorf("expected ok=%v, got ok=%v (duration=%v)", tt.expectedOK, ok, d)
			}
			if tt.expectedOK {
				if d < tt.expectedMinDur || d > tt.expectedMaxDur {
					t.Errorf("expected duration in [%v, %v], got %v", tt.expectedMinDur, tt.expectedMaxDur, d)
				}
			}
		})
	}
}

func TestClassifyAzureError(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		expectedRequeue time.Duration
		messageContains string
	}{
		{
			name: "When Azure returns 429 without Retry-After it should use default 5 minutes",
			err: &azcore.ResponseError{
				StatusCode:  429,
				RawResponse: &http.Response{Header: http.Header{}},
			},
			expectedRequeue: 5 * time.Minute,
			messageContains: "Azure API rate limit exceeded, retrying",
		},
		{
			name: "When Azure returns 429 with Retry-After seconds it should use that duration",
			err: &azcore.ResponseError{
				StatusCode: 429,
				RawResponse: &http.Response{
					Header: http.Header{
						"Retry-After": {"120"},
					},
				},
			},
			expectedRequeue: 120 * time.Second,
			messageContains: "Azure API rate limit exceeded, retrying",
		},
		{
			name: "When Azure returns 429 with nil RawResponse it should use default 5 minutes",
			err: &azcore.ResponseError{
				StatusCode:  429,
				RawResponse: nil,
			},
			expectedRequeue: 5 * time.Minute,
			messageContains: "Azure API rate limit exceeded, retrying",
		},
		{
			name: "When Azure returns 429 with OperationNotAllowed ErrorCode it should still classify as rate limit",
			err: &azcore.ResponseError{
				StatusCode:  429,
				ErrorCode:   ErrorCodeOperationNotAllowed,
				RawResponse: &http.Response{Header: http.Header{}},
			},
			expectedRequeue: 5 * time.Minute,
			messageContains: "Azure API rate limit exceeded, retrying",
		},
		{
			name:            "When Azure returns 403 without ErrorCode it should requeue after 10 minutes",
			err:             &azcore.ResponseError{StatusCode: 403},
			expectedRequeue: 10 * time.Minute,
			messageContains: "Azure API permission denied, check service principal permissions",
		},
		{
			name:            "When Azure returns 409 without ErrorCode it should requeue after 30 seconds",
			err:             &azcore.ResponseError{StatusCode: 409},
			expectedRequeue: 30 * time.Second,
			messageContains: "Azure resource conflict, retrying",
		},
		{
			name: "When Azure returns 500 it should requeue after 2 minutes",
			err: &azcore.ResponseError{
				StatusCode: 500,
				ErrorCode:  "InternalServerError",
			},
			expectedRequeue: 2 * time.Minute,
			messageContains: "Azure API error (HTTP 500): InternalServerError",
		},
		{
			name:            "When error is not Azure it should requeue after 2 minutes",
			err:             errors.New("network timeout"),
			expectedRequeue: 2 * time.Minute,
			messageContains: "Unexpected error: network timeout",
		},
		// ErrorCode-based classification tests
		{
			name: "When ErrorCode is RequestDisallowedByPolicy it should return blocked by policy message",
			err: &azcore.ResponseError{
				StatusCode: 403,
				ErrorCode:  ErrorCodeRequestDisallowedByPolicy,
			},
			expectedRequeue: 10 * time.Minute,
			messageContains: "blocked by policy",
		},
		{
			name: "When ErrorCode is QuotaExceeded it should return quota exceeded message",
			err: &azcore.ResponseError{
				StatusCode: 403,
				ErrorCode:  ErrorCodeQuotaExceeded,
			},
			expectedRequeue: 10 * time.Minute,
			messageContains: "quota exceeded",
		},
		{
			name: "When ErrorCode is OperationNotAllowed (non-429) it should return operation not allowed message",
			err: &azcore.ResponseError{
				StatusCode: 403,
				ErrorCode:  ErrorCodeOperationNotAllowed,
			},
			expectedRequeue: 10 * time.Minute,
			messageContains: "operation not allowed",
		},
		{
			name: "When ErrorCode is AllocationFailed it should return allocation failed message",
			err: &azcore.ResponseError{
				StatusCode: 409,
				ErrorCode:  ErrorCodeAllocationFailed,
			},
			expectedRequeue: 5 * time.Minute,
			messageContains: "allocation failed",
		},
		{
			name: "When ErrorCode is ConflictingResourceName it should return name conflict message",
			err: &azcore.ResponseError{
				StatusCode: 409,
				ErrorCode:  ErrorCodeConflictingResourceName,
			},
			expectedRequeue: 10 * time.Minute,
			messageContains: "name conflict",
		},
		{
			name: "When ErrorCode is RoleAssignmentExists it should return role assignment message",
			err: &azcore.ResponseError{
				StatusCode: 409,
				ErrorCode:  ErrorCodeRoleAssignmentExists,
			},
			expectedRequeue: 30 * time.Second,
			messageContains: "role assignment",
		},
		{
			name: "When ErrorCode is SkuNotAvailable it should return SKU not available message",
			err: &azcore.ResponseError{
				StatusCode: 409,
				ErrorCode:  ErrorCodeSkuNotAvailable,
			},
			expectedRequeue: 10 * time.Minute,
			messageContains: "SKU not available",
		},
		{
			name: "When ErrorCode is unrecognized it should fall through to StatusCode handling",
			err: &azcore.ResponseError{
				StatusCode: 500,
				ErrorCode:  "SomethingUnexpected",
			},
			expectedRequeue: 2 * time.Minute,
			messageContains: "Azure API error (HTTP 500): SomethingUnexpected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requeue, message := ClassifyAzureError(tt.err)
			if requeue != tt.expectedRequeue {
				t.Errorf("expected requeue %v, got %v", tt.expectedRequeue, requeue)
			}
			if !strings.Contains(message, tt.messageContains) {
				t.Errorf("expected message to contain %q, got %q", tt.messageContains, message)
			}
		})
	}
}

func TestExtractPolicyDetails(t *testing.T) {
	tests := []struct {
		name             string
		err              error
		expectedPolicy   string
		expectedAssignID string
	}{
		{
			name:             "When error is not an Azure error it should return empty strings",
			err:              errors.New("not an azure error"),
			expectedPolicy:   "",
			expectedAssignID: "",
		},
		{
			name: "When RawResponse is nil it should return empty strings",
			err: &azcore.ResponseError{
				StatusCode:  403,
				RawResponse: nil,
			},
			expectedPolicy:   "",
			expectedAssignID: "",
		},
		{
			name: "When body is nil it should return empty strings",
			err: &azcore.ResponseError{
				StatusCode: 403,
				RawResponse: &http.Response{
					Body: nil,
				},
			},
			expectedPolicy:   "",
			expectedAssignID: "",
		},
		{
			name: "When body contains malformed JSON it should return empty strings",
			err: &azcore.ResponseError{
				StatusCode: 403,
				RawResponse: &http.Response{
					Body: newReadCloser("not json"),
				},
			},
			expectedPolicy:   "",
			expectedAssignID: "",
		},
		{
			name: "When body contains empty details array it should return empty strings",
			err: &azcore.ResponseError{
				StatusCode: 403,
				RawResponse: &http.Response{
					Body: newReadCloser(`{"error":{"code":"RequestDisallowedByPolicy","message":"denied","details":[]}}`),
				},
			},
			expectedPolicy:   "",
			expectedAssignID: "",
		},
		{
			name: "When body contains policy details it should extract them",
			err: &azcore.ResponseError{
				StatusCode: 403,
				RawResponse: &http.Response{
					Body: newReadCloser(`{"error":{"code":"RequestDisallowedByPolicy","message":"Resource blocked by policy.","details":[{"code":"RequestDisallowedByPolicy","message":"/subscriptions/xxx/providers/Microsoft.Authorization/policyAssignments/deny-public-ip"},{"code":"DenyPublicIP","message":"Public IP addresses are not allowed."}]}}`),
				},
			},
			expectedPolicy:   "DenyPublicIP",
			expectedAssignID: "/subscriptions/xxx/providers/Microsoft.Authorization/policyAssignments/deny-public-ip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, assignID := ExtractPolicyDetails(tt.err)
			if policy != tt.expectedPolicy {
				t.Errorf("expected policy %q, got %q", tt.expectedPolicy, policy)
			}
			if assignID != tt.expectedAssignID {
				t.Errorf("expected assignmentID %q, got %q", tt.expectedAssignID, assignID)
			}
		})
	}
}
