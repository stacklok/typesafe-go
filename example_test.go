package typesafe_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	typesafe "github.com/stacklok/typesafe-go"
)

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func Example_withAuthenticatedHTTPClient() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/typesafe/v1/models" {
			http.Error(w, "wrong endpoint", http.StatusNotFound)
			return
		}
		if req.Header.Get("Authorization") != "Bearer application-token" {
			http.Error(w, "missing application authentication", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"models":[]}`)
	}))
	defer server.Close()

	// Synthetic transport for this offline example; applications acquire tokens themselves.
	httpClient := &http.Client{Transport: bearerTransport{
		token: "application-token",
		base:  http.DefaultTransport,
	}}
	client, err := typesafe.NewClient(
		typesafe.WithBaseURL(server.URL+"/typesafe"),
		typesafe.WithAuthenticatedHTTPClient(httpClient),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	models, err := client.ListModels(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(len(models.Models))
	// Output: 0
}
