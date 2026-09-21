package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"

	typesafe "github.com/stacklok/typesafe-go"
)

var candidatePattern = regexp.MustCompile(`[A-Z]{2}-[0-9]+`)

func selectCandidate(text string) (string, error) {
	candidates := candidatePattern.FindAllString(text, -1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, `{"model":"demo-1.0.0","answers":{"candidate":{"type":"choice","choice":"AB-12","probabilities":{"AB-12":0.9,"CD-34":0.1},"confidence":0.8}},"usage":{"input_tokens":3,"output_tokens":1}}`); err != nil {
			log.Printf("write synthetic response: %v", err)
		}
	}))
	defer s.Close()
	criteria := map[string]typesafe.Content{}
	for _, v := range candidates {
		criteria[v] = v
	}
	p := typesafe.DefaultRetryPolicy()
	p.MaxRetries = 0
	client, err := typesafe.NewClient("synthetic", typesafe.WithBaseURL(s.URL), typesafe.WithRetryPolicy(p))
	if err != nil {
		return "", err
	}
	r, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: text, Model: "demo-1.0.0", Questions: map[string]typesafe.Question{"candidate": typesafe.Choice("Select relevant candidate", criteria)}})
	if err != nil {
		return "", err
	}
	answer, ok := r.Answers["candidate"].(typesafe.ChoiceAnswer)
	if !ok {
		return "", fmt.Errorf("unexpected candidate answer")
	}
	return answer.Choice, nil
}
func main() {
	v, err := selectCandidate("compare AB-12 with CD-34")
	if err != nil {
		log.Printf("select candidate: %v", err)
		return
	}
	if _, err := fmt.Println(v); err != nil {
		log.Printf("write result: %v", err)
	}
}
