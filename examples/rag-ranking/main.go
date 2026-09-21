package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"

	typesafe "github.com/stacklok/typesafe-go"
)

type ranking struct {
	Document string
	Score    float64
}

func rankAll(docs []string) ([]ranking, error) {
	scores := map[string]float64{"alpha": 2, "beta": 0, "gamma": 1}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		score := scores[request.State]
		if _, err := fmt.Fprintf(w, `{"model":"demo-1.0.0","answers":{"relevance":{"type":"score","score":%g,"probabilities":{"0":0.1,"1":0.2,"2":0.7},"legend":{"0":"low","1":"medium","2":"high"},"confidence":0.8}},"usage":{"input_tokens":3,"output_tokens":1}}`, score); err != nil {
			log.Printf("write synthetic response: %v", err)
		}
	}))
	defer server.Close()

	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 0
	client, err := typesafe.NewClient(typesafe.WithAPIKey("synthetic"), typesafe.WithBaseURL(server.URL), typesafe.WithRetryPolicy(policy))
	if err != nil {
		return nil, err
	}
	return rankWithClient(context.Background(), client, docs)
}

func rankWithClient(ctx context.Context, client *typesafe.Client, docs []string) ([]ranking, error) {
	out := make([]ranking, len(docs))
	sem := make(chan struct{}, 2) // Bound concurrent API calls.
	var wg sync.WaitGroup
	var first error
	var mu sync.Mutex
	for i, doc := range docs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{State: doc, Model: "demo-1.0.0", Questions: map[string]typesafe.Question{"relevance": typesafe.Score("Relevance", "low", "medium", "high")}})
			if err != nil {
				mu.Lock()
				if first == nil {
					first = err
				}
				mu.Unlock()
				return
			}
			answer, ok := response.Answers["relevance"].(typesafe.ScoreAnswer)
			if !ok {
				mu.Lock()
				if first == nil {
					first = fmt.Errorf("unexpected relevance answer")
				}
				mu.Unlock()
				return
			}
			out[i] = ranking{Document: doc, Score: answer.Score}
		}()
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

func main() {
	values, err := rankAll([]string{"alpha", "beta", "gamma"})
	if err != nil {
		log.Printf("rank documents: %v", err)
		return
	}
	log.Printf("rankings: %v", values)
}
