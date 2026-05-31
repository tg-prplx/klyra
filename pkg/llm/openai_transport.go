package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type openAIChatTransport struct {
	apiKey            string
	baseURL           string
	client            *http.Client
	retry             openAIRetryPolicy
	streamIdleTimeout time.Duration
}

type openAIRetryPolicy struct {
	MaxAttempts int
	Backoff     func(attempt int) time.Duration
}

func newOpenAIChatTransport(apiKey, baseURL string, retryTransient bool) openAIChatTransport {
	attempts := 1
	if retryTransient {
		attempts = 3
	}
	return openAIChatTransport{
		apiKey:            strings.TrimSpace(apiKey),
		baseURL:           normalizeOpenAICompatibleBaseURL(baseURL),
		client:            &http.Client{Timeout: 0},
		streamIdleTimeout: openAIStreamIdleTimeout(retryTransient),
		retry: openAIRetryPolicy{
			MaxAttempts: attempts,
			Backoff: func(attempt int) time.Duration {
				return time.Duration(attempt) * 200 * time.Millisecond
			},
		},
	}
}

func normalizeOpenAICompatibleBaseURL(raw string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	if trimmed == "" {
		return trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return trimmed
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/v1"
		return strings.TrimRight(parsed.String(), "/")
	}
	return trimmed
}

func openAIStreamIdleTimeout(localCompatible bool) time.Duration {
	raw := strings.TrimSpace(os.Getenv("KLYRA_OPENAI_STREAM_IDLE_TIMEOUT"))
	if raw != "" {
		if raw == "0" {
			return 0
		}
		if parsed, err := time.ParseDuration(raw); err == nil {
			return parsed
		}
		if seconds, err := strconv.Atoi(raw); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	if localCompatible {
		return 2 * time.Minute
	}
	return 0
}

func (t openAIChatTransport) doChat(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	var lastErr error
	for attempt := 1; attempt <= t.retry.MaxAttempts; attempt++ {
		resp, err := t.sendChat(ctx, body, stream)
		if err != nil {
			lastErr = err
			if t.shouldRetry(ctx, attempt, err) {
				if waitErr := t.sleepBeforeRetry(ctx, attempt); waitErr != nil {
					return nil, waitErr
				}
				continue
			}
			return nil, t.formatTransportError(err)
		}
		if isRetryableOpenAIStatus(resp.StatusCode) && attempt < t.retry.MaxAttempts {
			drainAndClose(resp.Body)
			if waitErr := t.sleepBeforeRetry(ctx, attempt); waitErr != nil {
				return nil, waitErr
			}
			continue
		}
		return resp, nil
	}
	return nil, t.formatTransportError(lastErr)
}

func (t openAIChatTransport) sendChat(ctx context.Context, body []byte, stream bool) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if t.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+t.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	return t.client.Do(httpReq)
}

func (t openAIChatTransport) shouldRetry(ctx context.Context, attempt int, err error) bool {
	if attempt >= t.retry.MaxAttempts || err == nil {
		return false
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return isTransientOpenAIError(err)
}

func (t openAIChatTransport) sleepBeforeRetry(ctx context.Context, attempt int) error {
	delay := t.retry.Backoff(attempt)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (t openAIChatTransport) formatTransportError(err error) error {
	if err == nil {
		return nil
	}
	if isTransientOpenAIError(err) {
		return fmt.Errorf("openai-compatible API transient connection error at %s: %w (local server closed/reset the connection; retry the request or check the local model server logs)", t.baseURL, err)
	}
	return err
}

func isLocalOpenAICompatibleBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

func isTransientOpenAIError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "connection reset by peer") ||
		strings.Contains(text, "connection refused") ||
		strings.Contains(text, "broken pipe") ||
		strings.Contains(text, "server closed idle connection") ||
		strings.Contains(text, "unexpected eof") ||
		strings.Contains(text, "eof")
}

func isRetryableOpenAIStatus(status int) bool {
	return status == http.StatusTooManyRequests ||
		status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4096))
	_ = body.Close()
}
