package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestACAUTH01AuthenticationSelection(t *testing.T) {
	ordinary := &http.Client{}
	authenticated := &http.Client{}
	tests := []struct {
		name string
		opts []Option
		ok   bool
		want []string
	}{
		{"missing", nil, false, []string{"WithAPIKey", "WithAuthenticatedHTTPClient"}},
		{"ordinary alone", []Option{WithHTTPClient(ordinary)}, false, []string{"WithAPIKey", "WithAuthenticatedHTTPClient", "WithHTTPClient"}},
		{"blank key", []Option{WithAPIKey(" ")}, false, nil},
		{"newline key", []Option{WithAPIKey("key\nvalue")}, false, nil},
		{"nil ordinary client", []Option{WithAPIKey("key"), WithHTTPClient(nil)}, false, []string{"WithHTTPClient"}},
		{"nil authenticated client", []Option{WithAuthenticatedHTTPClient(nil)}, false, []string{"WithAuthenticatedHTTPClient"}},
		{"duplicate key", []Option{WithAPIKey("key"), WithAPIKey("key")}, false, []string{"WithAPIKey"}},
		{"duplicate ordinary client", []Option{WithAPIKey("key"), WithHTTPClient(ordinary), WithHTTPClient(ordinary)}, false, []string{"WithHTTPClient"}},
		{"duplicate authenticated client", []Option{WithAuthenticatedHTTPClient(authenticated), WithAuthenticatedHTTPClient(authenticated)}, false, []string{"WithAuthenticatedHTTPClient"}},
		{"key then authenticated", []Option{WithAPIKey("key"), WithAuthenticatedHTTPClient(authenticated)}, false, []string{"WithAPIKey", "WithAuthenticatedHTTPClient"}},
		{"authenticated then key", []Option{WithAuthenticatedHTTPClient(authenticated), WithAPIKey("key")}, false, []string{"WithAPIKey", "WithAuthenticatedHTTPClient"}},
		{"ordinary then authenticated", []Option{WithHTTPClient(ordinary), WithAuthenticatedHTTPClient(authenticated)}, false, []string{"WithHTTPClient", "WithAuthenticatedHTTPClient"}},
		{"authenticated then ordinary", []Option{WithAuthenticatedHTTPClient(authenticated), WithHTTPClient(ordinary)}, false, []string{"WithHTTPClient", "WithAuthenticatedHTTPClient"}},
		{"key then ordinary", []Option{WithAPIKey("key"), WithHTTPClient(ordinary)}, true, nil},
		{"ordinary then key", []Option{WithHTTPClient(ordinary), WithAPIKey("key")}, true, nil},
		{"authenticated", []Option{WithAuthenticatedHTTPClient(authenticated)}, true, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(test.opts...)
			if (err == nil) != test.ok || test.ok && client == nil {
				t.Fatalf("client=%v error=%v", client, err)
			}
			for _, option := range test.want {
				if !strings.Contains(err.Error(), option) {
					t.Errorf("error %q does not name %s", err, option)
				}
			}
			if test.name == "blank key" {
				var validation *ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("expected ValidationError, got %T", err)
				}
			}
		})
	}
}

type rotatingAuthTransport struct {
	base   http.RoundTripper
	calls  atomic.Int32
	tokens []string
	t      *testing.T
}

func (r *rotatingAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if got := req.Header.Get("Authorization"); got != "" {
		r.t.Errorf("SDK supplied Authorization to authenticated transport: %q", got)
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	index := int(r.calls.Add(1)) - 1
	if index >= len(r.tokens) {
		r.t.Errorf("authenticated transport received unexpected attempt %d", index+1)
		return nil, fmt.Errorf("unexpected authenticated transport attempt %d", index+1)
	}
	clone.Header.Set("Authorization", "Bearer "+r.tokens[index])
	return r.base.RoundTrip(clone)
}

func TestACAUTH02AuthenticatedTransportRunsForEveryAttemptAndRoute(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			var firstBody string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				attempt := int(calls.Add(1))
				want := fmt.Sprintf("Bearer token-%d", attempt)
				if got := req.Header.Get("Authorization"); got != want {
					t.Errorf("Authorization=%q, want %q", got, want)
				}
				if req.Method == http.MethodPost {
					body, err := io.ReadAll(req.Body)
					if err != nil {
						t.Errorf("read request body: %v", err)
					}
					if attempt == 1 {
						firstBody = string(body)
					} else if string(body) != firstBody {
						t.Errorf("POST retry body changed: got %q, want %q", body, firstBody)
					}
				}
				if attempt == 1 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if req.Method == http.MethodGet {
					_, _ = io.WriteString(w, `{"models":[]}`)
					return
				}
				_, _ = io.WriteString(w, `{"model":"m","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer server.Close()

			transport := &rotatingAuthTransport{base: http.DefaultTransport, tokens: []string{"token-1", "token-2"}, t: t}
			client, err := NewClient(WithAuthenticatedHTTPClient(&http.Client{Transport: transport}), WithBaseURL(server.URL), fastRetry(1))
			if err != nil {
				t.Fatal(err)
			}
			if method == http.MethodGet {
				_, err = client.ListModels(context.Background())
			} else {
				_, err = client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: map[string]Question{"q": Noul(nil, nil)}})
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := calls.Load(); got != 2 || transport.calls.Load() != 2 {
				t.Fatalf("server calls=%d transport calls=%d, want 2", got, transport.calls.Load())
			}
		})
	}
}

func TestACAUTH02APIKeyModesAndAuthenticationFailuresDoNotRetry(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, transportAuth := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/transport=%t", http.StatusText(status), method, transportAuth), func(t *testing.T) {
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						calls.Add(1)
						if req.Header.Get("Authorization") != "Bearer credential" {
							t.Errorf("authorization=%q", req.Header.Get("Authorization"))
						}
						w.WriteHeader(status)
					}))
					defer server.Close()
					opts := []Option{WithBaseURL(server.URL), fastRetry(2)}
					if transportAuth {
						opts = append(opts, WithAuthenticatedHTTPClient(&http.Client{Transport: &rotatingAuthTransport{base: http.DefaultTransport, tokens: []string{"credential"}, t: t}}))
					} else {
						opts = append(opts, WithAPIKey("credential"))
					}
					client, err := NewClient(opts...)
					if err != nil {
						t.Fatal(err)
					}
					if method == http.MethodGet {
						_, err = client.ListModels(context.Background())
					} else {
						_, err = client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: map[string]Question{"q": Noul(nil, nil)}})
					}
					var apiErr *APIError
					if !errors.As(err, &apiErr) || calls.Load() != 1 {
						t.Fatalf("calls=%d error=%v", calls.Load(), err)
					}
				})
			}
		}
	}
}

func TestACAUTH02APIKeyWithHTTPClientAddsAuthorizationForGETAndPOST(t *testing.T) {
	for _, keyFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("key-first=%t", keyFirst), func(t *testing.T) {
			var calls atomic.Int32
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				if got := req.Header.Get("Authorization"); got != "Bearer key" {
					t.Errorf("Authorization=%q, want API-key header", got)
				}
				body := `{"models":[]}`
				if req.Method == http.MethodPost {
					body = `{"model":"m","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}}`
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})
			opts := []Option{WithHTTPClient(&http.Client{Transport: transport}), WithAPIKey("key"), noRetry()}
			if keyFirst {
				opts[0], opts[1] = opts[1], opts[0]
			}
			client, err := NewClient(opts...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ListModels(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: map[string]Question{"q": Noul(nil, nil)}}); err != nil {
				t.Fatal(err)
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("calls=%d, want 2", got)
			}
		})
	}
}

func TestACAUTH03HTTPClientCopiedAndRedirectDisabled(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	redirectCalls := atomic.Int32{}
	redirect := func(*http.Request, []*http.Request) error { redirectCalls.Add(1); return nil }
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://example.invalid"}}, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})
	for _, authenticated := range []bool{false, true} {
		supplied := &http.Client{Transport: transport, CheckRedirect: redirect, Jar: jar, Timeout: 17 * time.Second}
		opts := []Option{WithAPIKey("key"), WithHTTPClient(supplied), noRetry()}
		if authenticated {
			opts = []Option{WithAuthenticatedHTTPClient(supplied), noRetry()}
		}
		client, err := NewClient(opts...)
		if err != nil {
			t.Fatal(err)
		}
		if reflect.ValueOf(client.httpClient.Transport).Pointer() != reflect.ValueOf(transport).Pointer() || client.httpClient.Jar != jar || client.httpClient.Timeout != supplied.Timeout {
			t.Fatal("supplied HTTP client fields were not retained")
		}
		if reflect.ValueOf(client.httpClient.CheckRedirect).Pointer() == reflect.ValueOf(redirect).Pointer() || reflect.ValueOf(supplied.CheckRedirect).Pointer() != reflect.ValueOf(redirect).Pointer() {
			t.Fatal("redirect policy was not overridden only on the copy")
		}
		_, _ = client.ListModels(context.Background())
	}
	if redirectCalls.Load() != 0 {
		t.Fatalf("original redirect callback called %d times", redirectCalls.Load())
	}
}

func TestACAUTH03RedirectMatrixNeverForwardsCredentialsOrBodies(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, authenticated := range []bool{false, true} {
				t.Run(fmt.Sprintf("%d/%s/transport=%t", status, method, authenticated), func(t *testing.T) {
					var targetCalls atomic.Int32
					target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
						targetCalls.Add(1)
						if req.Header.Get("Authorization") != "" {
							t.Error("credential reached redirect target")
						}
						if body, _ := io.ReadAll(req.Body); len(body) != 0 {
							t.Error("body reached redirect target")
						}
					}))
					defer target.Close()
					origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						w.Header().Set("Location", target.URL)
						w.WriteHeader(status)
					}))
					defer origin.Close()
					opts := []Option{WithAPIKey("credential"), WithBaseURL(origin.URL), fastRetry(2)}
					if authenticated {
						opts = []Option{WithAuthenticatedHTTPClient(&http.Client{Transport: &rotatingAuthTransport{base: http.DefaultTransport, tokens: []string{"credential"}, t: t}}), WithBaseURL(origin.URL), fastRetry(2)}
					}
					client, err := NewClient(opts...)
					if err != nil {
						t.Fatal(err)
					}
					if method == http.MethodGet {
						_, err = client.ListModels(context.Background())
					} else {
						_, err = client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: map[string]Question{"q": Noul(nil, nil)}})
					}
					var apiErr *APIError
					if !errors.As(err, &apiErr) || apiErr.StatusCode != status || targetCalls.Load() != 0 {
						t.Fatalf("target calls=%d error=%v", targetCalls.Load(), err)
					}
				})
			}
		}
	}
}

func TestACAUTH05TransportClientAndErrorsRemainRedacted(t *testing.T) {
	secret := "transport-owned-secret"
	client, err := NewClient(WithAuthenticatedHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New(secret)
	})}), noRetry())
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	slog.New(slog.NewJSONHandler(&log, nil)).Info("client", "value", client)
	encoded, err := json.Marshal(client)
	if err != nil {
		t.Fatal(err)
	}
	_, callErr := client.ListModels(context.Background())
	slog.New(slog.NewJSONHandler(&log, nil)).Info("error", "value", callErr)
	encodedError, err := json.Marshal(callErr)
	if err != nil {
		t.Fatal(err)
	}
	values := []string{fmt.Sprint(client), fmt.Sprintf("%+v", client), fmt.Sprintf("%#v", client), log.String(), string(encoded), string(encodedError), fmt.Sprint(callErr), fmt.Sprintf("%+v", callErr), fmt.Sprintf("%#v", callErr)}
	for _, value := range values {
		if strings.Contains(value, secret) {
			t.Fatalf("transport credential leaked through SDK formatting: %q", value)
		}
	}
	if cause := errors.Unwrap(callErr); cause == nil || !strings.Contains(cause.Error(), secret) {
		t.Fatal("custom transport cause was not available to an explicit unwrap")
	}
}
