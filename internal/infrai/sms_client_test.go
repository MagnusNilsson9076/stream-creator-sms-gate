package infrai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSMSClientRequestBoundary(t *testing.T) {
	tests := []struct {
		name       string
		call       func(*SMSClient) error
		wantPath   string
		wantBody   string
		responses  []string
		wantCalls  int
		wantSleeps int
	}{
		{
			name: "request code",
			call: func(c *SMSClient) error {
				return c.RequestCode(context.Background(), "+14155550100", "login-7-request")
			},
			wantPath:  "/v1/sms/otp",
			wantBody:  `{"to":"+14155550100"}`,
			responses: []string{`{"ok":true,"data":{},"metadata":{}}`},
			wantCalls: 1,
		},
		{
			name: "verify code after rate limit",
			call: func(c *SMSClient) error {
				return c.VerifyCode(context.Background(), "+14155550100", "123456", "login-7-verify")
			},
			wantPath:   "/v1/sms/verify",
			wantBody:   `{"code":"123456","to":"+14155550100"}`,
			responses:  []string{`{"ok":false,"error":{"code":"RATE_LIMITED"}}`, `{"ok":true,"data":{}}`},
			wantCalls:  2,
			wantSleeps: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls, sleeps := 0, 0
			client := NewSMSClient("test-key")
			client.sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
			client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, _ := io.ReadAll(req.Body)
				if req.Method != http.MethodPost || req.URL.Path != tt.wantPath || string(body) != tt.wantBody {
					t.Fatalf("request = %s %s %s", req.Method, req.URL.Path, body)
				}
				if req.Header.Get("Authorization") != "Bearer test-key" || req.Header.Get("Idempotency-Key") == "" {
					t.Fatal("missing authorization or idempotency header")
				}
				status := http.StatusOK
				if calls == 0 && len(tt.responses) > 1 {
					status = http.StatusTooManyRequests
				}
				response := tt.responses[calls]
				calls++
				return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			if err := tt.call(client); err != nil {
				t.Fatal(err)
			}
			if calls != tt.wantCalls || sleeps != tt.wantSleeps {
				t.Fatalf("calls/sleeps = %d/%d, want %d/%d", calls, sleeps, tt.wantCalls, tt.wantSleeps)
			}
		})
	}
}
