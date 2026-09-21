package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	typesafe "github.com/stacklok/typesafe-go"
)

func priority(impactWeight float64) (float64, error) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, `{"model":"demo-1.0.0","answers":{"impact":{"type":"score","score":1.5,"probabilities":{"0":0.1,"1":0.3,"2":0.4,"3":0.2},"legend":{"0":"low","1":"medium","2":"high","3":"critical"},"confidence":0.7},"urgent":{"type":"noul","noul":0.8}},"usage":{"input_tokens":5,"output_tokens":2}}`); err != nil {
			log.Printf("write synthetic response: %v", err)
		}
	}))
	defer s.Close()
	p := typesafe.DefaultRetryPolicy()
	p.MaxRetries = 0
	client, err := typesafe.NewClient(typesafe.WithAPIKey("synthetic"), typesafe.WithBaseURL(s.URL), typesafe.WithRetryPolicy(p))
	if err != nil {
		return 0, err
	}
	r, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "work item", Model: "demo-1.0.0", Questions: map[string]typesafe.Question{"impact": typesafe.Score("Impact", "low", "medium", "high", "critical"), "urgent": typesafe.Noul("Urgent?", nil)}})
	if err != nil {
		return 0, err
	}
	impact, ok := r.Answers["impact"].(typesafe.ScoreAnswer)
	if !ok {
		return 0, fmt.Errorf("unexpected impact answer")
	}
	urgent, ok := r.Answers["urgent"].(typesafe.NoulAnswer)
	if !ok {
		return 0, fmt.Errorf("unexpected urgent answer")
	}
	return impactWeight*impact.Score + (1-impactWeight)*urgent.Noul, nil
}
func main() {
	v, err := priority(.7)
	if err != nil {
		log.Printf("compute priority: %v", err)
		return
	}
	if _, err := fmt.Println(v); err != nil {
		log.Printf("write result: %v", err)
	}
}
