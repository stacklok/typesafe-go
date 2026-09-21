package typesafe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestACAUTH04GatewayPrefixesPreserveEscaping(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		want   string
	}{
		{"root", "", ""},
		{"root slash", "/", ""},
		{"prefix", "/typesafe", "/typesafe"},
		{"trailing slash", "/typesafe/", "/typesafe"},
		{"encoded space", "/team%20one", "/team%20one"},
		{"escaped slash segment", "/team%2Fone", "/team%2Fone"},
		{"escaped trailing slash", "/team%2F", "/team%2F"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			requests := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests <- req.RequestURI
				switch req.Method {
				case http.MethodGet:
					_, _ = io.WriteString(w, `{"models":[]}`)
				case http.MethodPost:
					_, _ = io.WriteString(w, `{"model":"m","answers":{"q":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}}`)
				}
			}))
			defer server.Close()
			client, err := NewClient(WithAPIKey("key"), WithBaseURL(server.URL+test.prefix), noRetry())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ListModels(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := client.SystemOne(context.Background(), SystemOneRequest{State: "state", Model: "m", Questions: map[string]Question{"q": Noul(nil, nil)}}); err != nil {
				t.Fatal(err)
			}
			for _, endpoint := range []string{"/v1/models", "/v1/systemone"} {
				if got := <-requests; got != test.want+endpoint {
					t.Fatalf("RequestURI=%q, want %q", got, test.want+endpoint)
				}
			}
		})
	}
}

func TestACAUTH04InvalidBaseURLs(t *testing.T) {
	invalid := []string{
		"https://example.com?", "https://example.com#", "https://user@example.com/prefix",
		"https://example.com/a/./b", "https://example.com/a/../b",
		"https://example.com/a/%2e/b", "https://example.com/a/%2E%2E/b",
		"https://example.com/%zz", "://missing", "//example.com/prefix", "http://example.com/prefix",
	}
	for _, raw := range invalid {
		t.Run(strings.ReplaceAll(raw, "/", "_"), func(t *testing.T) {
			if _, err := NewClient(WithAPIKey("key"), WithBaseURL(raw)); err == nil {
				t.Fatalf("accepted invalid base URL %q", raw)
			}
		})
	}
}

func TestACAUTH05TransportModeRequestIDsAndRedaction(t *testing.T) {
	ids := []string{"request-transport", "bad\nvalue", strings.Repeat("x", 129)}
	for index, id := range ids {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-typesafe-request-id", id)
				_, _ = io.WriteString(w, `{"models":[]}`)
			}))
			defer server.Close()
			client, err := NewClient(WithAuthenticatedHTTPClient(server.Client()), WithBaseURL(server.URL), noRetry())
			if err != nil {
				t.Fatal(err)
			}
			if client.apiKey != "" {
				t.Fatal("transport authentication stored an API-key placeholder")
			}
			response, err := client.ListModels(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			want := id
			if index != 0 {
				want = ""
			}
			if response.RequestID != want {
				t.Fatalf("request ID=%q, want %q", response.RequestID, want)
			}
		})
	}
}
