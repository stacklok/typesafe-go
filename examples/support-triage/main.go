package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	typesafe "github.com/stacklok/typesafe-go"
)

func run() (string, error) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, `{"model":"demo-1.0.0","answers":{"urgent":{"type":"noul","noul":0.9},"team":{"type":"choice","choice":"billing","probabilities":{"billing":0.8,"general":0.2},"confidence":0.7}},"usage":{"input_tokens":10,"output_tokens":2}}`); err != nil {
			log.Printf("write synthetic response: %v", err)
		}
	}))
	defer s.Close()
	p := typesafe.DefaultRetryPolicy()
	p.MaxRetries = 0
	c, err := typesafe.NewClient(typesafe.WithAPIKey("synthetic"), typesafe.WithBaseURL(s.URL), typesafe.WithRetryPolicy(p))
	if err != nil {
		return "", err
	}
	r, err := c.SystemOne(context.Background(), typesafe.SystemOneRequest{State: map[string]any{"subject": "refund", "age_days": 3, "vip": true}, Model: "demo-1.0.0", Questions: map[string]typesafe.Question{"urgent": typesafe.Noul("Is urgent?", nil), "team": typesafe.Choice("Route", map[string]typesafe.Content{"billing": "billing", "general": "general"})}})
	if err != nil {
		return "", err
	}
	answer, ok := r.Answers["team"].(typesafe.ChoiceAnswer)
	if !ok {
		return "", fmt.Errorf("unexpected team answer")
	}
	return answer.Choice, nil
}
func main() {
	value, err := run()
	if err != nil {
		log.Printf("triage support request: %v", err)
		return
	}
	if _, err := fmt.Println(value); err != nil {
		log.Printf("write result: %v", err)
	}
}
