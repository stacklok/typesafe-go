package typesafe

import (
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy controls retries and their total HTTP call budget, including
// attempts, body reads, and backoff. It does not bound request preparation or
// response decoding. MaxRetries counts retries after the first attempt. A
// zero-value policy is invalid; start with DefaultRetryPolicy and modify fields,
// including setting MaxRetries to zero.
type RetryPolicy struct {
	MaxRetries            int
	BackoffInitial        time.Duration
	BackoffMax            time.Duration
	BackoffJitter         float64
	TotalBudget           time.Duration
	RetryConnectionErrors bool
	RetryTimeouts         bool
}

// DefaultRetryPolicy returns two retries, 500ms-to-5s subtractive-jitter
// backoff, a 30-second total budget, and connection/timeout retries enabled.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxRetries: 2, BackoffInitial: 500 * time.Millisecond, BackoffMax: 5 * time.Second,
		BackoffJitter: .25, TotalBudget: 30 * time.Second, RetryConnectionErrors: true, RetryTimeouts: true}
}

func (p RetryPolicy) validate() error {
	checks := []struct {
		invalid bool
		field   string
		reason  string
	}{
		{p.MaxRetries < 0, "retry.max_retries", "out of range"},
		{p.BackoffInitial < 0, "retry.backoff_initial", "out of range"},
		{p.BackoffMax < p.BackoffInitial, "retry.backoff_max", "out of range"},
		{math.IsNaN(p.BackoffJitter) || math.IsInf(p.BackoffJitter, 0) || p.BackoffJitter < 0 || p.BackoffJitter > 1, "retry.backoff_jitter", "out of range"},
		{p.TotalBudget <= 0, "retry.total_budget", "out of range"},
	}
	for _, check := range checks {
		if check.invalid {
			return &ValidationError{Field: check.field, Reason: check.reason}
		}
	}
	return nil
}

func (p RetryPolicy) backoff(retry int) time.Duration {
	d := p.BackoffInitial
	for range retry {
		if d >= p.BackoffMax || d > p.BackoffMax/2 {
			d = p.BackoffMax
			break
		}
		d *= 2
	}
	if d > p.BackoffMax {
		d = p.BackoffMax
	}
	if p.BackoffJitter > 0 {
		d -= time.Duration(rand.Float64() * p.BackoffJitter * float64(d))
	}
	return d
}

func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	if value := strings.TrimSpace(h.Get("retry-after-ms")); value != "" {
		if delay, ok := parseRetryDelay(value, time.Millisecond); ok {
			return delay, true
		}
	}
	value := strings.TrimSpace(h.Get("Retry-After"))
	if value == "" {
		return 0, false
	}
	if delay, ok := parseRetryDelay(value, time.Second); ok {
		return delay, true
	}
	if when, err := http.ParseTime(value); err == nil {
		d := when.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

func parseRetryDelay(value string, unit time.Duration) (time.Duration, bool) {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		if numErr, ok := err.(*strconv.NumError); ok && numErr.Err == strconv.ErrRange && n > 0 {
			return time.Duration(1<<63 - 1), true
		}
		return 0, false
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0, false
	}
	max := time.Duration(1<<63 - 1)
	if n >= float64(max)/float64(unit) {
		return max, true
	}
	return time.Duration(n * float64(unit)), true
}
