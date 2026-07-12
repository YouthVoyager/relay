package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"429 限流", &APIError{Status: 429}, true},
		{"500 服务端", &APIError{Status: 500}, true},
		{"401 认证", &APIError{Status: 401}, false},
		{"400 请求错", &APIError{Status: 400}, false},
		{"用户取消", context.Canceled, false},
		{"超时截止", context.DeadlineExceeded, false},
		{"未知错误保守不重试", errors.New("mystery"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryable(tt.err); got != tt.want {
				t.Fatalf("retryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestWithRetry_EventualSuccess(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	calls := 0
	got, err := WithRetry(context.Background(), cfg, "test", func() (string, error) {
		calls++
		if calls < 3 {
			return "", &APIError{Status: 429}
		}
		return "ok", nil
	})
	if err != nil || got != "ok" || calls != 3 {
		t.Fatalf("got=%q err=%v calls=%d", got, err, calls)
	}
}

func TestWithRetry_NonRetryableStopsImmediately(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond}
	calls := 0
	_, err := WithRetry(context.Background(), cfg, "test", func() (string, error) {
		calls++
		return "", &APIError{Status: 400}
	})
	if err == nil || calls != 1 {
		t.Fatalf("expected immediate stop, calls=%d err=%v", calls, err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("error chain lost the APIError: %v", err)
	}
}