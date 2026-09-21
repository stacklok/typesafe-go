package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	typesafe "github.com/stacklok/typesafe-go"
)

func recommend(confidence float64) (string, error) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprintf(w, `{"model":"demo-1.0.0","answers":{"skill":{"type":"choice","choice":"go","probabilities":{"go":0.6,"rust":0.4},"confidence":%g}},"usage":{"input_tokens":4,"output_tokens":1}}`, confidence); err != nil {
			log.Printf("write synthetic response: %v", err)
		}
	}))
	defer s.Close()
	p := typesafe.DefaultRetryPolicy()
	p.MaxRetries = 0
	client, err := typesafe.NewClient(typesafe.WithAPIKey("synthetic"), typesafe.WithBaseURL(s.URL), typesafe.WithRetryPolicy(p))
	if err != nil {
		return "review", err
	}
	r, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "candidate profile", Model: "demo-1.0.0", Questions: map[string]typesafe.Question{"skill": typesafe.Choice("Best match", map[string]typesafe.Content{"go": "Go", "rust": "Rust"})}})
	if err != nil {
		return "review", err
	}
	a, ok := r.Answers["skill"].(typesafe.ChoiceAnswer)
	if !ok {
		return "review", fmt.Errorf("unexpected skill answer")
	}
	if a.Confidence < 0.75 { // Illustrative abstention threshold, not a universal probability.
		return "review", nil
	}
	// This recommends a skill for review; it does not execute or authorize work.
	return "recommend-" + a.Choice, nil
}
func main() {
	v, err := recommend(.8)
	if err != nil {
		log.Printf("recommend skill: %v", err)
		return
	}
	if _, err := fmt.Println(v); err != nil {
		log.Printf("write result: %v", err)
	}
}
