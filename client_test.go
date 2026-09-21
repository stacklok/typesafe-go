package typesafe

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func noRetry() Option {
	p := DefaultRetryPolicy()
	p.MaxRetries = 0
	return WithRetryPolicy(p)
}

func TestPublicClientContractFixturesAndHeaders(t *testing.T) {
	requestFixture, err := os.ReadFile("testdata/systemone-request.json")
	if err != nil {
		t.Fatal(err)
	}
	responseFixture, err := os.ReadFile("testdata/systemone-response.json")
	if err != nil {
		t.Fatal(err)
	}
	modelsFixture, err := os.ReadFile("testdata/models-response.json")
	if err != nil {
		t.Fatal(err)
	}
	handlerErrors := make(chan error, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != userAgent || r.Header.Get("X-Typesafe-Retry-Count") != "0" {
			handlerErrors <- fmt.Errorf("incorrect common headers: %v", r.Header)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/v1/systemone":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				handlerErrors <- fmt.Errorf("incorrect POST metadata")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				handlerErrors <- readErr
				return
			}
			var got, want any
			if decodeOne(body, &got) != nil || decodeOne(requestFixture, &want) != nil || !reflect.DeepEqual(got, want) {
				handlerErrors <- fmt.Errorf("request does not match fixture: %s", body)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("x-typesafe-request-id", "fixture-systemone")
			_, _ = w.Write(responseFixture)
		case "/v1/models":
			if r.Method != http.MethodGet || r.Header.Get("Content-Type") != "" {
				handlerErrors <- fmt.Errorf("incorrect GET metadata")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("x-typesafe-request-id", "fixture-models")
			_, _ = w.Write(modelsFixture)
		default:
			handlerErrors <- fmt.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		handlerErrors <- nil
	}))
	defer server.Close()
	client, err := NewClient(WithAPIKey("fixture-key"), WithBaseURL(server.URL), noRetry())
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.SystemOne(context.Background(), SystemOneRequest{
		State: map[string]any{"synthetic": true, "items": []any{1, nil}}, Model: "demo-1.0.0",
		Questions: map[string]Question{
			"noul":   Noul("Decide", &NoulCriteria{True: map[string]any{"nested": true}, False: nil}),
			"choice": Choice([]any{"Choose", 2}, map[string]Content{"a": "A", "b": map[string]any{"nested": true}}),
			"score":  Score(map[string]any{"kind": "rank"}, "low", map[string]any{"nested": []any{true, nil}}),
		},
	})
	if handlerErr := <-handlerErrors; handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if err != nil || response.RequestID != "fixture-systemone" || len(response.Answers) != 3 {
		t.Fatalf("SystemOne fixture call: %#v, %v", response, err)
	}
	models, err := client.ListModels(context.Background())
	if handlerErr := <-handlerErrors; handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if err != nil || models.RequestID != "fixture-models" || len(models.Models) != 1 {
		t.Fatalf("models fixture call: %#v, %v", models, err)
	}
}

func TestACAPI02EndToEndAllVariants(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/systemone" || r.Method != http.MethodPost {
			t.Errorf("wrong endpoint: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing auth")
		}
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got["model"] != "pinned-1.0.0" {
			t.Errorf("unexpected body: %s", body)
		}
		w.Header().Set("x-typesafe-request-id", "request-1")
		io.WriteString(w, `{"model":"pinned-1.0.0","answers":{"n":{"type":"noul","noul":0},"c":{"type":"choice","choice":"a","probabilities":{"a":0.6,"b":0.4},"confidence":0},"s":{"type":"score","score":0.5,"probabilities":{"0":0.5,"1":0.5},"legend":{"0":{"nested":true},"1":[1,null]},"confidence":0.2}},"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	defer srv.Close()
	client, err := NewClient(WithAPIKey("secret"), WithBaseURL(srv.URL), noRetry())
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.SystemOne(context.Background(), SystemOneRequest{State: map[string]any{"nested": []any{1, true, nil}}, Model: "pinned-1.0.0", Questions: map[string]Question{
		"n": Noul(nil, &NoulCriteria{True: nil, False: "no"}),
		"c": Choice("pick", map[string]Content{"a": map[string]any{"x": true}, "b": nil}),
		"s": Score([]any{"rate"}, "low", map[string]any{"high": true}),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.RequestID != "request-1" || calls.Load() != 1 {
		t.Fatalf("bad response: %#v", resp)
	}
	if resp.Answers["n"].(NoulAnswer).Noul != 0 || resp.Usage.InputTokens != 0 {
		t.Error("zero values not preserved")
	}
}

func TestACAPI03And04SchemaLeniencyAndValidation(t *testing.T) {
	levels := make([]Content, 11)
	for i := range levels {
		levels[i] = fmt.Sprintf("level-%d", i)
	}
	choices := make(map[string]Content, 256)
	for i := range 256 {
		choices[fmt.Sprint(i)] = nil
	}
	payload, err := json.Marshal(struct {
		State     Content             `json:"state"`
		Model     string              `json:"model"`
		Questions map[string]Question `json:"questions"`
	}{
		State: "state", Model: "m", Questions: map[string]Question{"one": Score(nil, "only"), "many": Score(nil, levels...), "choice": Choice(nil, choices)},
	})
	if err != nil || validateRequestJSON(payload) != nil {
		t.Fatalf("documented service limits must not be local wire limits: %v", err)
	}
	for _, req := range []SystemOneRequest{
		{State: nil, Questions: map[string]Question{"q": Score(nil, "x")}},
		{State: 3, Questions: map[string]Question{"q": Score(nil, "x")}},
		{State: "x", Questions: map[string]Question{"q": Score(nil, nil)}},
	} {
		c, _ := NewClient(WithAPIKey("x"), WithBaseURL("http://localhost:1"), noRetry())
		if _, err := c.SystemOne(context.Background(), req); err == nil {
			t.Error("invalid shape accepted")
		}
	}
}

func TestACAPI05InputFailuresDoNotCallTransport(t *testing.T) {
	if _, err := NewClient(WithAPIKey(" \n")); err == nil {
		t.Fatal("blank key accepted")
	}
	if _, err := NewClient(WithAPIKey("x"), WithDefaultModel(" ")); err == nil {
		t.Fatal("blank model accepted")
	}
	var calls atomic.Int32
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, errors.New("called") })
	c, _ := NewClient(WithAPIKey("x"), WithHTTPClient(&http.Client{Transport: rt}), noRetry())
	var typedNil *NoulQuestion
	bad := []SystemOneRequest{
		{State: "x"}, {State: "x", Questions: map[string]Question{"": Noul(nil, nil)}},
		{State: "x", Questions: map[string]Question{"q": typedNil}},
		{State: func() {}, Questions: map[string]Question{"q": Noul(nil, nil)}},
		{State: "x", Questions: map[string]Question{"q": Score(nil, mathNaN())}},
		{State: "x", Questions: map[string]Question{"q": Noul(nil, &NoulCriteria{True: true, False: "no"})}},
	}
	for _, req := range bad {
		if _, err := c.SystemOne(context.Background(), req); err == nil {
			t.Error("invalid request accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("transport called %d times", calls.Load())
	}
	_, err := c.SystemOne(context.Background(), SystemOneRequest{State: rejectingMarshaler{}, Questions: map[string]Question{"q": Noul(nil, nil)}})
	if err == nil || strings.Contains(err.Error(), "attacker payload") {
		t.Fatalf("custom marshaler message leaked: %v", err)
	}
}

type rejectingMarshaler struct{}

func (rejectingMarshaler) MarshalJSON() ([]byte, error) {
	return nil, errors.New("attacker payload")
}

func mathNaN() float64 { var z float64; return z / z }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func expectedForTest(t *testing.T, questions map[string]Question) map[string]expectedAnswer {
	t.Helper()
	payload, err := json.Marshal(struct {
		State     Content             `json:"state"`
		Model     string              `json:"model"`
		Questions map[string]Question `json:"questions"`
	}{State: "state", Model: "model", Questions: questions})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := snapshotRequestJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	return expected
}

func TestACAPI06And07ProtocolFailures(t *testing.T) {
	questions := map[string]Question{"q": Choice(nil, map[string]Content{"yes": nil})}
	cases := []string{
		`{"model":"m","answers":{},"usage":{"input_tokens":0,"output_tokens":0}}`,
		`{"model":"m","answers":{"q":{"type":"noul","noul":0}},"usage":{"input_tokens":0,"output_tokens":0}}`,
		`{"model":"m","answers":{"q":{"type":"future"}},"usage":{"input_tokens":0,"output_tokens":0}}`,
		`{"model":"m","answers":{"q":{"type":"choice","choice":"no","probabilities":{"no":1},"confidence":1}},"usage":{"input_tokens":0,"output_tokens":0}}`,
		`{"model":"m","model":"evil","answers":{},"usage":{"input_tokens":0,"output_tokens":0}}`,
		`{"model":"m","answers":{"q":{"type":"choice","choice":"yes","probabilities":{"yes":2},"confidence":1}},"usage":{"input_tokens":0,"output_tokens":0}}`,
	}
	for _, body := range cases {
		if _, err := decodeSystemOne([]byte(body), "id", expectedForTest(t, questions)); !errors.Is(err, ErrProtocol) {
			t.Errorf("wanted protocol error for %s: %v", body, err)
		}
	}
	missing := []string{
		`{"answers":{},"usage":{"input_tokens":0,"output_tokens":0}}`,
		`{"model":"m","usage":{"input_tokens":0,"output_tokens":0}}`,
		`{"model":"m","answers":{},"usage":{"output_tokens":0}}`,
		`{"model":"m","answers":{},"usage":{"input_tokens":0}}`,
	}
	for _, body := range missing {
		if _, err := decodeSystemOne([]byte(body), "id", map[string]expectedAnswer{}); !errors.Is(err, ErrProtocol) {
			t.Errorf("missing required accepted: %s", body)
		}
	}
	pointerQuestion := Choice(nil, map[string]Content{"yes": nil})
	valid := `{"model":"m","answers":{"q":{"type":"choice","choice":"yes","probabilities":{"yes":1},"confidence":1}},"usage":{"input_tokens":0,"output_tokens":0}}`
	if _, err := decodeSystemOne([]byte(valid), "id", expectedForTest(t, map[string]Question{"q": &pointerQuestion})); err != nil {
		t.Fatalf("non-nil pointer question failed: %v", err)
	}
}

// TestACAPI08ValuesPreserved deliberately uses a schema-decodable but not
// live-conformant Score relationship to prove the decoder preserves service values
// rather than normalizing probabilities or recomputing Score/legend content.
func TestACAPI08ValuesPreserved(t *testing.T) {
	q := map[string]Question{"q": Score(nil, "a", "b")}
	body := `{"model":"unlisted-version","answers":{"q":{"type":"score","score":0.333333333333,"probabilities":{"0":0.2,"1":0.7},"legend":{"0":{"a":9007199254740993},"1":[true,null]},"confidence":0.123}},"usage":{"input_tokens":1,"output_tokens":2}}`
	resp, err := decodeSystemOne([]byte(body), "", expectedForTest(t, q))
	if err != nil {
		t.Fatal(err)
	}
	a := resp.Answers["q"].(ScoreAnswer)
	large, ok := a.Legend["0"].(map[string]any)["a"].(json.Number)
	if !ok || large.String() != "9007199254740993" {
		t.Fatalf("structured legend integer lost precision: %#v", a.Legend)
	}
	if a.Score != 0.333333333333 || a.Probabilities["0"] != .2 || a.Confidence != .123 || resp.Model != "unlisted-version" {
		t.Fatalf("values changed: %#v", a)
	}
}

func TestProbabilityNullRejectedEndToEnd(t *testing.T) {
	for _, answer := range []string{
		`{"type":"choice","choice":"yes","probabilities":{"yes":null},"confidence":0}`,
		`{"type":"score","score":0,"probabilities":{"0":null},"legend":{"0":"only"},"confidence":0}`,
	} {
		t.Run(answer[9:14], func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, `{"model":"m","answers":{"q":`+answer+`},"usage":{"input_tokens":0,"output_tokens":0}}`)
			}))
			defer srv.Close()
			client, err := NewClient(WithAPIKey("key"), WithBaseURL(srv.URL), noRetry())
			if err != nil {
				t.Fatal(err)
			}
			var question Question = Choice(nil, map[string]Content{"yes": nil})
			if strings.Contains(answer, `"score"`) {
				question = Score(nil, "only")
			}
			_, err = client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: map[string]Question{"q": question}})
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("null probability accepted: %v", err)
			}
		})
	}
}

func TestRequestIDEchoingAPIKeyIsRemoved(t *testing.T) {
	for _, response := range []struct {
		status int
		body   string
	}{
		{200, `{"models":[]}`},
		{422, `{}`},
		{200, `{"models":null}`},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("x-typesafe-request-id", "prefix-secret-key-suffix")
			w.WriteHeader(response.status)
			_, _ = io.WriteString(w, response.body)
		}))
		client, err := NewClient(WithAPIKey("secret-key"), WithBaseURL(srv.URL), noRetry())
		if err != nil {
			t.Fatal(err)
		}
		result, callErr := client.ListModels(context.Background())
		srv.Close()
		if result != nil && result.RequestID != "" {
			t.Fatalf("key echoed in successful metadata: %q", result.RequestID)
		}
		var apiErr *APIError
		if errors.As(callErr, &apiErr) && apiErr.RequestID != "" {
			t.Fatalf("key echoed in API metadata: %q", apiErr.RequestID)
		}
		var protocolErr *ProtocolError
		if errors.As(callErr, &protocolErr) && protocolErr.RequestID != "" {
			t.Fatalf("key echoed in protocol metadata: %q", protocolErr.RequestID)
		}
		if strings.Contains(fmt.Sprintf("%v %+v %#v", callErr, callErr, callErr), "secret-key") {
			t.Fatal("key echoed in error formatting")
		}
	}
}

func TestACAPI09Models(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "m1")
		io.WriteString(w, `{"models":[{"name":"jev-latest","description":"alias","release_date":"2026-09-21"}]}`)
	}))
	defer srv.Close()
	c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), noRetry())
	resp, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resp.RequestID != "m1" || len(resp.Models) != 1 {
		t.Fatalf("bad models: %#v", resp)
	}
	if _, err := decodeModels([]byte(`{"models":[]}`), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeModels([]byte(`{"models":[{"name":"x","description":"x","release_date":"2026-02-30"}]}`), ""); !errors.Is(err, ErrProtocol) {
		t.Fatal("invalid date accepted")
	}
}

func TestACSEC01And05SecretSafeFormatting(t *testing.T) {
	secret := "super-secret-token"
	c, _ := NewClient(WithAPIKey(secret))
	var log strings.Builder
	logger := slog.New(slog.NewTextHandler(&log, nil))
	logger.Info("client", "value", c)
	values := []string{fmt.Sprint(c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c), log.String()}
	encoded, _ := json.Marshal(c)
	values = append(values, string(encoded))
	e := &APIError{StatusCode: 422, RequestID: "attacker\nstate and msg"}
	values = append(values, e.Error(), fmt.Sprintf("%+v", e))
	for _, value := range values {
		if strings.Contains(value, secret) || strings.Contains(value, "state and msg") {
			t.Fatalf("sensitive value leaked: %q", value)
		}
	}
	var target *APIError
	if !errors.Is(e, ErrAPI) || !errors.As(e, &target) || target.RequestID == "" {
		t.Fatal("API error not inspectable")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "request-422")
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, `{"detail":[{"msg":"state and msg","input":{"private":"payload"}}]}`)
	}))
	defer srv.Close()
	client, _ := NewClient(WithAPIKey(secret), WithBaseURL(srv.URL), noRetry())
	_, err := client.SystemOne(context.Background(), SystemOneRequest{State: map[string]any{"private": "payload"}, Questions: map[string]Question{"q": Noul(nil, nil)}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 422 || apiErr.RequestID != "request-422" {
		t.Fatalf("API metadata missing: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", err), "payload") || strings.Contains(err.Error(), "state and msg") {
		t.Fatalf("422 response leaked: %+v", err)
	}
}

func TestACSEC03URLAndRedirectGuards(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://u:p@example.com", "https://example.com?q=x", "https://example.com/#x", "//example.com"} {
		if _, err := NewClient(WithAPIKey("x"), WithBaseURL(raw)); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Store(true) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	caller := &http.Client{}
	c, _ := NewClient(WithAPIKey("x"), WithBaseURL(redirect.URL), WithHTTPClient(caller), noRetry())
	_, err := c.ListModels(context.Background())
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != 307 || reached.Load() {
		t.Fatalf("redirect followed or wrong error: %v", err)
	}
	if caller.CheckRedirect != nil {
		t.Fatal("caller client mutated")
	}
}

func TestRedirectMatrixNeverFollowsOrRetries(t *testing.T) {
	for _, status := range []int{301, 302, 307, 308} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, custom := range []bool{false, true} {
				name := fmt.Sprintf("%d/%s/custom=%t", status, method, custom)
				t.Run(name, func(t *testing.T) {
					var originCalls, targetCalls atomic.Int32
					target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
					defer target.Close()
					origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						originCalls.Add(1)
						http.Redirect(w, r, target.URL, status)
					}))
					defer origin.Close()
					options := []Option{WithAPIKey("key"), WithBaseURL(origin.URL), fastRetry(2)}
					var supplied *http.Client
					if custom {
						supplied = &http.Client{}
						options = append(options, WithHTTPClient(supplied))
					}
					client, err := NewClient(options...)
					if err != nil {
						t.Fatal(err)
					}
					if method == http.MethodGet {
						_, err = client.ListModels(context.Background())
					} else {
						_, err = client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: map[string]Question{"q": Noul(nil, nil)}})
					}
					var apiErr *APIError
					if !errors.As(err, &apiErr) || apiErr.StatusCode != status || originCalls.Load() != 1 || targetCalls.Load() != 0 {
						t.Fatalf("err=%v origin=%d target=%d", err, originCalls.Load(), targetCalls.Load())
					}
					if supplied != nil && supplied.CheckRedirect != nil {
						t.Fatal("supplied client mutated")
					}
				})
			}
		}
	}
}

func TestResponseLimitRejectsOverflow(t *testing.T) {
	if _, err := NewClient(WithAPIKey("x"), WithResponseLimit(1<<63-1)); err == nil {
		t.Fatal("MaxInt64 response limit accepted")
	}
}

func TestACSEC04BoundedBodies(t *testing.T) {
	for _, status := range []int{200, 422} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			io.WriteString(w, strings.Repeat("x", 32))
		}))
		c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), WithResponseLimit(8), noRetry())
		_, err := c.ListModels(context.Background())
		srv.Close()
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("status %d: %v", status, err)
		}
	}

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(strings.Repeat("x", 32)))
	_ = writer.Close()
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": {"gzip"}}, Body: io.NopCloser(bytes.NewReader(compressed.Bytes()))}, nil
	})
	c, _ := NewClient(WithAPIKey("x"), WithHTTPClient(&http.Client{Transport: rt}), WithResponseLimit(8), noRetry())
	if _, err := c.ListModels(context.Background()); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("decompressed body was not bounded: %v", err)
	}
}

func TestDefaultTransportBoundsDecompressedGzipAtExactLimit(t *testing.T) {
	payload := []byte(`{"models":[]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		writer := gzip.NewWriter(w)
		if _, err := writer.Write(payload); err != nil {
			return
		}
		_ = writer.Close()
	}))
	defer server.Close()
	for _, test := range []struct {
		limit  int64
		tooBig bool
	}{{int64(len(payload)), false}, {int64(len(payload) - 1), true}} {
		client, err := NewClient(WithAPIKey("x"), WithBaseURL(server.URL), WithResponseLimit(test.limit), noRetry())
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.ListModels(context.Background())
		if errors.Is(err, ErrResponseTooLarge) != test.tooBig {
			t.Fatalf("limit %d: %v", test.limit, err)
		}
	}
}

func TestACSEC06ConcurrentClientAndOwnership(t *testing.T) {
	criteria := map[string]Content{"yes": "yes"}
	q := Choice(nil, criteria)
	criteria["evil"] = "changed"
	if _, ok := q.Criteria["evil"]; ok {
		t.Fatal("constructor did not copy map")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"models":[]}`) }))
	defer srv.Close()
	c, _ := NewClient(WithAPIKey("x"), WithBaseURL(srv.URL), noRetry())
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.ListModels(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
