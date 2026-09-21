package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	typesafe "github.com/stacklok/typesafe-go"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(t *testing.T, transport http.RoundTripper) *typesafe.Client {
	t.Helper()
	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 0
	client, err := typesafe.NewClient(
		typesafe.WithAPIKey("synthetic-not-a-secret"),
		typesafe.WithHTTPClient(&http.Client{Transport: transport}),
		typesafe.WithRetryPolicy(policy),
	)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestEvaluate(t *testing.T) {
	calls := 0
	client := testClient(t, roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			t.Fatal("expected one System One POST")
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-not-a-secret" {
			t.Fatal("missing expected authorization")
		}
		var request struct {
			State     map[string]string `json:"state"`
			Model     string            `json:"model"`
			Questions map[string]struct {
				Type     string `json:"type"`
				Criteria any    `json:"criteria"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != modelName || request.State["request"] != "I was charged twice for order A-104. Please help me get the duplicate charge refunded." || len(request.Questions) != 3 {
			t.Fatal("unexpected request")
		}
		if request.Questions[duplicateLabel].Type != "noul" || request.Questions[teamLabel].Type != "choice" || request.Questions[urgencyLabel].Type != "score" {
			t.Fatal("unexpected questions")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"model":"jev-1.13.0","answers":{"duplicate_charge":{"type":"noul","noul":0.9},"team":{"type":"choice","choice":"billing","probabilities":{"billing":0.8,"technical":0.1,"other":0.1},"confidence":0.7},"urgency":{"type":"score","score":1,"probabilities":{"0":0.2,"1":0.6,"2":0.2},"legend":{"0":"Routine: the customer can wait.","1":"Needs prompt attention: respond soon.","2":"Time-sensitive blocking issue: immediate attention is needed."},"confidence":0.8}},"usage":{"input_tokens":10,"output_tokens":2}}`))}, nil
	}))

	out, err := evaluate(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || out.Model != modelName || out.Usage.InputTokens != 10 || out.Usage.OutputTokens != 2 || out.DuplicateCharge != 0.9 || out.Team.Choice != "billing" || out.Team.Confidence != 0.7 || len(out.Team.Probabilities) != 3 || out.Urgency.Score != 1 || out.Urgency.Confidence != 0.8 || len(out.Urgency.Probabilities) != 3 {
		t.Fatal("unexpected result")
	}
}

func TestEvaluateMalformedServiceResponseReturnsNoResult(t *testing.T) {
	client := testClient(t, roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{`))}, nil
	}))

	out, err := evaluate(context.Background(), client)
	if err == nil {
		t.Fatal("expected error")
	}
	if out.Model != "" || out.Usage.InputTokens != 0 || out.Usage.OutputTokens != 0 || out.DuplicateCharge != 0 || out.Team.Choice != "" || len(out.Team.Probabilities) != 0 || out.Urgency.Score != 0 || len(out.Urgency.Probabilities) != 0 {
		t.Fatal("unexpected result data")
	}
}

func TestRunRejectsMissingUnreadableAndBlankKeyFile(t *testing.T) {
	if _, err := run(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected unreadable key file error")
	}
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(keyFile); err == nil {
		t.Fatal("expected blank key file error")
	}
}
