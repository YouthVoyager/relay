package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/url"
	"time"
)

type RetryConfig struct {
	MaxAttempts int           // 总尝试次数(含首次)
	BaseDelay   time.Duration // 首次重试前的基础等待
	MaxDelay    time.Duration // 单次等待上限
}

var DefaultRetry = RetryConfig{
	MaxAttempts: 5,
	BaseDelay:   1 * time.Second,
	MaxDelay:    30 * time.Second,
}

// retryable 判断错误是否值得重试。这是整个重试层的大脑。
func retryable(err error) bool {
	// 调用方主动取消/超时:重试是违抗命令,立即停
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// API 错误按状态码分类
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == 429: // 限流:稍后再来,最典型的可重试
			return true
		case apiErr.Status >= 500: // 服务端故障:大概率瞬态
			return true
		default: // 4xx:请求本身有问题,重试无意义
			return false
		}
	}
	// URL 格式/协议错误:配置问题,重试无意义
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// url.Error 包装了几乎所有客户端错误,需要看内层:
		// 超时和连接拒绝是瞬态的,可重试;其余(如 scheme 错误)不可
		if urlErr.Timeout() {
			return true
		}
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return true // 连接层错误(拒绝、重置):可重试
		}
		return false // 其余(协议错误、URL 解析错误):不可重试
	}

	// 网络层错误(连接被重置、DNS 抖动等):可重试
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	// 未知错误:保守起见不重试,让它暴露出来
	return false
}

// WithRetry 执行 fn,失败且可重试时按"指数退避+全抖动"重试。
func WithRetry[T any](ctx context.Context, cfg RetryConfig, op string, fn func() (T, error)) (T, error) {
	var zero T
	var lastErr error

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err

		if !retryable(err) {
			return zero, fmt.Errorf("%s: %w", op, err)
		}
		if attempt == cfg.MaxAttempts {
			break
		}

		// 指数退避:base × 2^(attempt-1),封顶 MaxDelay
		backoff := cfg.BaseDelay << (attempt - 1)
		if backoff > cfg.MaxDelay {
			backoff = cfg.MaxDelay
		}
		// 全抖动(full jitter):在 [0, backoff) 里随机取,最大化摊开
		wait := time.Duration(rand.Int64N(int64(backoff)))

		slog.Warn("retrying after error",
			"op", op, "attempt", attempt, "max", cfg.MaxAttempts,
			"wait", wait.Round(time.Millisecond).String(), "error", err)

		select {
		case <-time.After(wait):
		case <-ctx.Done(): // 等待期间被取消,立即响应
			return zero, ctx.Err()
		}
	}
	return zero, fmt.Errorf("%s: exhausted %d attempts: %w", op, cfg.MaxAttempts, lastErr)
}