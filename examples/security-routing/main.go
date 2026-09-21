package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"

	typesafe "github.com/stacklok/typesafe-go"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("synthetic transport failure")
}

func route(response string) (string, error) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if response == "api-error" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		if _, err := io.WriteString(w, response); err != nil {
			log.Printf("write synthetic response: %v", err)
		}
	}))
	defer server.Close()

	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 0
	options := []typesafe.Option{typesafe.WithAPIKey("synthetic"), typesafe.WithBaseURL(server.URL), typesafe.WithRetryPolicy(policy)}
	if response == "transport-error" {
		options = append(options, typesafe.WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	}
	client, err := typesafe.NewClient(options...)
	if err != nil {
		return "review", err
	}
	result, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "untrusted event", Model: "demo-1.0.0", Questions: map[string]typesafe.Question{"safe": typesafe.Noul("Suitable for policy review?", nil)}})
	if err != nil {
		// API, protocol, and transport failures all fail closed to human review.
		return "review", nil
	}
	answer, ok := result.Answers["safe"].(typesafe.NoulAnswer)
	if !ok || answer.Noul < .9 { // Illustrative threshold, not a universal probability.
		return "review", nil
	}
	// This is advisory only. Independent authentication and authorization are
	// required before any security-sensitive action.
	return "eligible-for-policy-review", nil
}

func main() {
	value, err := route(`{"model":"demo-1.0.0","answers":{"safe":{"type":"noul","noul":0.5}},"usage":{"input_tokens":2,"output_tokens":1}}`)
	if err != nil {
		log.Printf("route event: %v", err)
		return
	}
	if _, err := fmt.Println(value); err != nil {
		log.Printf("write result: %v", err)
	}
}
