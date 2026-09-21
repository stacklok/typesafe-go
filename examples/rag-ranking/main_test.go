package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"

	typesafe "github.com/stacklok/typesafe-go"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestACDOC03RAGRankingIsOrderedAndConcurrencyBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		release := make(chan struct{})
		scores := map[string]float64{"alpha": 2, "beta": 0, "gamma": 1}
		rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
			var request struct {
				State string `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				return nil, err
			}
			calls.Add(1)
			<-release
			body := []byte(`{"model":"demo-1.0.0","answers":{"relevance":{"type":"score","score":` + strconv.FormatFloat(scores[request.State], 'f', -1, 64) + `,"probabilities":{"0":0.1,"1":0.2,"2":0.7},"legend":{"0":"low","1":"medium","2":"high"},"confidence":0.8}},"usage":{"input_tokens":3,"output_tokens":1}}`)
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		})
		policy := typesafe.DefaultRetryPolicy()
		policy.MaxRetries = 0
		client, err := typesafe.NewClient(typesafe.WithAPIKey("synthetic"), typesafe.WithHTTPClient(&http.Client{Transport: rt}), typesafe.WithRetryPolicy(policy))
		if err != nil {
			t.Fatal(err)
		}

		type result struct {
			values []ranking
			err    error
		}
		done := make(chan result, 1)
		go func() {
			values, err := rankWithClient(context.Background(), client, []string{"alpha", "beta", "gamma"})
			done <- result{values, err}
		}()

		synctest.Wait()
		if started := calls.Load(); started != 2 {
			close(release)
			<-done
			t.Fatalf("calls started while workers are blocked = %d, want 2", started)
		}
		close(release)
		got := <-done
		if got.err != nil {
			t.Fatal(got.err)
		}
		if calls.Load() != 3 {
			t.Fatalf("eventual calls = %d, want 3", calls.Load())
		}
		if len(got.values) != 3 || got.values[0].Document != "alpha" || got.values[1].Document != "gamma" || got.values[2].Document != "beta" {
			t.Fatalf("unexpected ranking: %#v", got.values)
		}
	})
}
