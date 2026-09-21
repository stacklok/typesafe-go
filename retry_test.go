package typesafe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func fastRetry(max int) Option {
	p := DefaultRetryPolicy()
	p.MaxRetries = max
	p.BackoffInitial = time.Millisecond
	p.BackoffMax = time.Millisecond
	p.BackoffJitter = 0
	p.TotalBudget = time.Second
	return WithRetryPolicy(p)
}

func TestACREL01StatusRetriesAndDisable(t *testing.T) {
	for _, status := range []int{408, 429, 500, 529} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(status) }))
		c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), fastRetry(2))
		_, _ = c.ListModels(context.Background())
		srv.Close()
		if calls.Load() != 3 {
			t.Errorf("status %d got %d attempts", status, calls.Load())
		}
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer srv.Close()
	c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), noRetry())
	_, _ = c.ListModels(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("MaxRetries 0 made %d attempts", calls.Load())
	}
}

func TestACREL02BodyReplayAndMarshalOnce(t *testing.T) {
	var calls atomic.Int32
	var first []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		attempt := calls.Add(1)
		if r.Header.Get("X-Typesafe-Retry-Count") != strconv.Itoa(int(attempt-1)) {
			t.Errorf("wrong retry count header on attempt %d: %q", attempt, r.Header.Get("X-Typesafe-Retry-Count"))
		}
		if attempt == 1 {
			first = append([]byte(nil), body...)
			w.WriteHeader(500)
			return
		}
		if !bytes.Equal(first, body) {
			t.Error("retry body changed")
		}
		io.WriteString(w, `{"model":"m","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	m := &countMarshaler{}
	c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), fastRetry(1))
	_, err := c.SystemOne(context.Background(), SystemOneRequest{State: m, Model: "m", Questions: map[string]Question{"q": Noul(nil, nil)}})
	if err != nil || m.calls.Load() != 1 {
		t.Fatalf("err=%v marshals=%d", err, m.calls.Load())
	}
}

type countMarshaler struct{ calls atomic.Int32 }

func (m *countMarshaler) MarshalJSON() ([]byte, error) {
	m.calls.Add(1)
	return []byte(`{"safe":true}`), nil
}

func TestACREL03RetryAfterParsing(t *testing.T) {
	h := http.Header{"Retry-After": {"10"}, "Retry-After-Ms": {"7"}}
	if d, ok := retryAfter(h, time.Now()); !ok || d != 7*time.Millisecond {
		t.Fatalf("ms did not win: %v %v", d, ok)
	}
	h = http.Header{"Retry-After": {"2"}}
	if d, ok := retryAfter(h, time.Now()); !ok || d != 2*time.Second {
		t.Fatal("seconds failed")
	}
	now := time.Now().UTC().Truncate(time.Second)
	h.Set("Retry-After", now.Add(time.Second).Format(http.TimeFormat))
	if d, ok := retryAfter(h, now); !ok || d != time.Second {
		t.Fatal("date failed")
	}
	p := DefaultRetryPolicy()
	p.BackoffJitter = 0
	if p.backoff(0) != 500*time.Millisecond || p.backoff(9) != 5*time.Second {
		t.Fatal("backoff failed")
	}
}

func TestACREL04BudgetDoesNotRetryEarly(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("retry-after-ms", "500")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	p := DefaultRetryPolicy()
	p.TotalBudget = 50 * time.Millisecond
	p.MaxRetries = 2
	c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), WithRetryPolicy(p))
	start := time.Now()
	_, err := c.ListModels(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 || calls.Load() != 1 || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("budget handling: calls=%d err=%v elapsed=%v", calls.Load(), err, time.Since(start))
	}
}

func TestACREL05CancellationAndTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	p := DefaultRetryPolicy()
	p.MaxRetries = 0
	p.TotalBudget = time.Second
	c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), WithAttemptTimeout(10*time.Millisecond), WithRetryPolicy(p))
	_, err := c.ListModels(context.Background())
	if !errors.Is(err, ErrAttemptTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout chain lost: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.ListModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel lost: %v", err)
	}
}

func TestACREL06ProtocolAndOversizeNeverRetry(t *testing.T) {
	for _, body := range []string{`not json`, `xxxxxxxxxxxxxxxx`} {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, body) }))
		c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), WithResponseLimit(8), fastRetry(2))
		_, _ = c.ListModels(context.Background())
		srv.Close()
		if calls.Load() != 1 {
			t.Fatalf("body %q retried %d", body, calls.Load())
		}
	}
}

func TestACREL01InterruptedReadRetries(t *testing.T) {
	var calls atomic.Int32
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &brokenBody{}}, nil
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"models":[]}`))}, nil
	})
	c, _ := NewClient(WithAPIKey("x"), WithHTTPClient(&http.Client{Transport: rt}), fastRetry(1))
	if _, err := c.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type brokenBody struct{ read bool }

func (b *brokenBody) Read(p []byte) (int, error) {
	if !b.read {
		b.read = true
		copy(p, "{")
		return 1, nil
	}
	return 0, errors.New("interrupted")
}
func (*brokenBody) Close() error { return nil }

func TestPublicRetryConnectionAndAttemptTimeoutFlags(t *testing.T) {
	for _, failure := range []string{"connection", "timeout"} {
		for _, enabled := range []bool{false, true} {
			t.Run(failure+"/enabled="+strconv.FormatBool(enabled), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					var calls atomic.Int32
					rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
						attempt := calls.Add(1)
						if r.Header.Get("X-Typesafe-Retry-Count") != strconv.Itoa(int(attempt-1)) {
							t.Errorf("attempt %d counter %q", attempt, r.Header.Get("X-Typesafe-Retry-Count"))
						}
						if attempt == 1 {
							if failure == "connection" {
								return nil, errors.New("synthetic connection failure")
							}
							<-r.Context().Done()
							return nil, r.Context().Err()
						}
						return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"models":[]}`))}, nil
					})
					policy := DefaultRetryPolicy()
					policy.MaxRetries = 1
					policy.BackoffInitial, policy.BackoffMax, policy.BackoffJitter = time.Second, time.Second, 0
					policy.RetryConnectionErrors = enabled
					policy.RetryTimeouts = enabled
					client, err := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithAttemptTimeout(2*time.Second), WithRetryPolicy(policy))
					if err != nil {
						t.Fatal(err)
					}
					_, err = client.ListModels(context.Background())
					if enabled && (err != nil || calls.Load() != 2) {
						t.Fatalf("enabled retry: calls=%d err=%v", calls.Load(), err)
					}
					if !enabled && calls.Load() != 1 {
						t.Fatalf("disabled retry made %d calls", calls.Load())
					}
					if !enabled && failure == "connection" {
						var connection *ConnectionError
						if !errors.Is(err, ErrConnection) || !errors.As(err, &connection) {
							t.Fatalf("connection error not typed: %v", err)
						}
					}
					if !enabled && failure == "timeout" {
						var timeout *AttemptTimeoutError
						if !errors.Is(err, ErrAttemptTimeout) || !errors.As(err, &timeout) || !errors.Is(err, context.DeadlineExceeded) {
							t.Fatalf("attempt timeout not typed: %v", err)
						}
					}
				})
			})
		}
	}
}

func TestPublicRetryCountersDefaultMaximum(t *testing.T) {
	var counters []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		counters = append(counters, r.Header.Get("X-Typesafe-Retry-Count"))
		return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})
	policy := DefaultRetryPolicy()
	policy.BackoffInitial, policy.BackoffMax, policy.BackoffJitter = 0, 0, 0
	client, err := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithRetryPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.ListModels(context.Background())
	if got := strings.Join(counters, ","); got != "0,1,2" {
		t.Fatalf("retry counters = %s", got)
	}
}

func TestPublicRetryBackoffReturnsCallerCauseWithoutNextAttempt(t *testing.T) {
	var calls atomic.Int32
	first := make(chan struct{})
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(first)
		return &http.Response{StatusCode: 500, Header: http.Header{"Retry-After": {"30"}}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})
	policy := DefaultRetryPolicy()
	policy.TotalBudget = time.Minute
	client, _ := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithRetryPolicy(policy))
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("caller stopped during backoff")
	done := make(chan error, 1)
	go func() { _, err := client.ListModels(ctx); done <- err }()
	<-first
	cancel(cause)
	err := <-done
	if !errors.Is(err, cause) || calls.Load() != 1 {
		t.Fatalf("cause=%v calls=%d", err, calls.Load())
	}
}

type hiddenDeadlineContext struct{ context.Context }

func (hiddenDeadlineContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func TestPublicRetryCallerDeadlineDuringBackoffReturnsCauseWithoutNextAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			return &http.Response{StatusCode: 500, Header: http.Header{"Retry-After": {"30"}}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
		})
		policy := DefaultRetryPolicy()
		policy.TotalBudget = time.Minute
		client, _ := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithRetryPolicy(policy))
		cause := errors.New("caller deadline cause")
		deadlineCtx, cancel := context.WithTimeoutCause(context.Background(), 5*time.Second, cause)
		defer cancel()
		_, err := client.ListModels(hiddenDeadlineContext{deadlineCtx})
		if !errors.Is(err, cause) || calls.Load() != 1 {
			t.Fatalf("cause=%v calls=%d", err, calls.Load())
		}
	})
}

func TestRetryAfterPrecedenceAndBudgetsScheduleActualRetry(t *testing.T) {
	cases := []struct {
		name    string
		headers http.Header
		want    time.Duration
	}{
		{"milliseconds precedence", http.Header{"Retry-After-Ms": {"3000"}, "Retry-After": {"9"}}, 3 * time.Second},
		{"fractional milliseconds precedence", http.Header{"Retry-After-Ms": {"0.5"}, "Retry-After": {"9"}}, 500 * time.Microsecond},
		{"seconds", http.Header{"Retry-After": {"4"}}, 4 * time.Second},
		{"fractional seconds", http.Header{"Retry-After": {"0.25"}}, 250 * time.Millisecond},
		{"date", nil, 5 * time.Second},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				headers := test.headers.Clone()
				if headers == nil {
					headers = http.Header{"Retry-After": {start.UTC().Add(test.want).Format(http.TimeFormat)}}
				}
				if delay, ok := retryAfter(headers, start); !ok || delay != test.want {
					t.Fatalf("fixture retry delay=%v ok=%v header=%q", delay, ok, headers.Get("Retry-After"))
				}
				var calls int
				rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return &http.Response{StatusCode: 429, Header: headers, Body: io.NopCloser(bytes.NewReader(nil))}, nil
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"models":[]}`))}, nil
				})
				policy := DefaultRetryPolicy()
				policy.MaxRetries = 1
				policy.TotalBudget = 10 * time.Second
				policy.BackoffInitial, policy.BackoffMax, policy.BackoffJitter = time.Second, time.Second, 0
				client, _ := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithRetryPolicy(policy))
				if _, err := client.ListModels(context.Background()); err != nil {
					t.Fatal(err)
				}
				if elapsed := time.Since(start); elapsed != test.want || calls != 2 {
					t.Fatalf("elapsed=%v calls=%d", elapsed, calls)
				}
			})
		})
	}

	for _, value := range []string{"-0.25", "NaN", "Infinity"} {
		t.Run("invalid falls back/"+value, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				var calls int
				rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {value}}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"models":[]}`))}, nil
				})
				policy := DefaultRetryPolicy()
				policy.MaxRetries = 1
				policy.BackoffInitial, policy.BackoffMax, policy.BackoffJitter = time.Second, time.Second, 0
				client, _ := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithRetryPolicy(policy))
				if _, err := client.ListModels(context.Background()); err != nil {
					t.Fatal(err)
				}
				if elapsed := time.Since(start); elapsed != time.Second || calls != 2 {
					t.Fatalf("fallback elapsed=%v calls=%d", elapsed, calls)
				}
			})
		})
	}

	t.Run("enormous positive exceeds budget", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			headers := http.Header{"Retry-After": {"1e999"}}
			if delay, ok := retryAfter(headers, start); !ok || delay != time.Duration(1<<63-1) {
				t.Fatalf("enormous delay=%v ok=%v", delay, ok)
			}
			var calls int
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 429, Header: headers, Body: io.NopCloser(bytes.NewReader(nil))}, nil
			})
			policy := DefaultRetryPolicy()
			policy.MaxRetries = 1
			policy.TotalBudget = 10 * time.Second
			client, _ := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithRetryPolicy(policy))
			_, err := client.ListModels(context.Background())
			var api *APIError
			if !errors.As(err, &api) || calls != 1 || time.Since(start) != 0 {
				t.Fatalf("early retry/wait: elapsed=%v calls=%d err=%v", time.Since(start), calls, err)
			}
		})
	})

	for _, callerDeadline := range []bool{false, true} {
		t.Run("delay exceeds budget/caller="+strconv.FormatBool(callerDeadline), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var calls int
				rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"3"}}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
				})
				policy := DefaultRetryPolicy()
				policy.TotalBudget = 2 * time.Second
				client, _ := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: rt}), WithRetryPolicy(policy))
				ctx := context.Background()
				if callerDeadline {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, time.Second)
					defer cancel()
				}
				start := time.Now()
				_, err := client.ListModels(ctx)
				var api *APIError
				if !errors.As(err, &api) || calls != 1 || time.Since(start) != 0 {
					t.Fatalf("early retry/wait: elapsed=%v calls=%d err=%v", time.Since(start), calls, err)
				}
			})
		})
	}
}

func TestRetryPolicyValidationErrorsArePerField(t *testing.T) {
	base := DefaultRetryPolicy()
	cases := map[string]RetryPolicy{}
	p := base
	p.MaxRetries = -1
	cases["retry.max_retries"] = p
	p = base
	p.BackoffInitial = -1
	cases["retry.backoff_initial"] = p
	p = base
	p.BackoffMax = p.BackoffInitial - 1
	cases["retry.backoff_max"] = p
	for name, jitter := range map[string]float64{"negative": -1, "above one": 2, "NaN": math.NaN(), "positive infinity": math.Inf(1), "negative infinity": math.Inf(-1)} {
		p = base
		p.BackoffJitter = jitter
		cases["retry.backoff_jitter/"+name] = p
	}
	p = base
	p.TotalBudget = 0
	cases["retry.total_budget"] = p
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewClient(WithAPIKey("key"), WithRetryPolicy(policy))
			var validation *ValidationError
			if !errors.As(err, &validation) || validation.Reason != "out of range" || !strings.HasPrefix(name, validation.Field) {
				t.Fatalf("error=%#v", err)
			}
		})
	}
}

type cancelBody struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
	closes  atomic.Int32
}

func (b *cancelBody) Read([]byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.closed
	return 0, errors.New("hostile body text")
}
func (b *cancelBody) Close() error {
	b.closes.Add(1)
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

func TestCancellationAndTimeoutCloseResponseBody(t *testing.T) {
	for _, cancelCall := range []bool{true, false} {
		t.Run(strconv.FormatBool(cancelCall), func(t *testing.T) {
			body := &cancelBody{started: make(chan struct{}), closed: make(chan struct{})}
			rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
			})
			p := DefaultRetryPolicy()
			p.MaxRetries = 0
			c, err := NewClient(WithAPIKey("x"), WithHTTPClient(&http.Client{Transport: rt}), WithAttemptTimeout(20*time.Millisecond), WithRetryPolicy(p))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := c.ListModels(ctx); done <- err }()
			<-body.started
			if cancelCall {
				cancel()
			}
			err = <-done
			if cancelCall && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if !cancelCall && (!errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrAttemptTimeout)) {
				t.Fatalf("timeout lost: %v", err)
			}
			select {
			case <-body.closed:
			default:
				t.Fatal("response body was not closed")
			}
			if body.closes.Load() != 1 {
				t.Fatalf("response body closed %d times", body.closes.Load())
			}
		})
	}
}

func TestSystemOneUsesSerializedQuestionSnapshot(t *testing.T) {
	responseReady := make(chan struct{})
	release := make(chan struct{})
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(responseReady)
		<-release
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"model":"m","answers":{"q":{"type":"choice","choice":"yes","probabilities":{"yes":1},"confidence":1}},"usage":{"input_tokens":1,"output_tokens":1}}`))}, nil
	})
	client, err := NewClient(WithAPIKey("x"), WithHTTPClient(&http.Client{Transport: rt}), noRetry())
	if err != nil {
		t.Fatal(err)
	}
	questions := map[string]Question{"q": Choice(nil, map[string]Content{"yes": "yes"})}
	done := make(chan error, 1)
	go func() {
		_, err := client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: questions})
		done <- err
	}()
	<-responseReady
	questions["q"] = Noul(nil, nil)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("response was correlated against mutated caller map: %v", err)
	}
}
