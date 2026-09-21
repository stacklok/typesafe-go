package main

import "testing"

func TestACDOC03SecurityRoutingIsAdvisoryAndFailsClosed(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"high", `{"model":"m","answers":{"safe":{"type":"noul","noul":0.95}},"usage":{"input_tokens":0,"output_tokens":0}}`, "eligible-for-policy-review"},
		{"low", `{"model":"m","answers":{"safe":{"type":"noul","noul":0.2}},"usage":{"input_tokens":0,"output_tokens":0}}`, "review"},
		{"protocol", "not-json", "review"},
		{"transport", "transport-error", "review"},
		{"api", "api-error", "review"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := route(test.body)
			if err != nil || got != test.want {
				t.Fatalf("got %q, %v; want %q", got, err, test.want)
			}
		})
	}
}
