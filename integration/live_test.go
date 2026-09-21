//go:build live

package integration

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	typesafe "github.com/stacklok/typesafe-go"
)

var pinnedModel = regexp.MustCompile(`^jev-[0-9]+\.[0-9]+\.[0-9]+$`)

// TestACLIVE01Conformance is optional, may incur charges, and is never run by CI.
func TestACLIVE01Conformance(t *testing.T) {
	if os.Getenv("TYPESAFE_LIVE_TEST") != "1" {
		t.Skip("set TYPESAFE_LIVE_TEST=1 to authorize a live, possibly paid request")
	}
	key := os.Getenv("TYPESAFE_API_KEY")
	model := os.Getenv("TYPESAFE_PINNED_MODEL")
	if key == "" {
		t.Fatal("TYPESAFE_API_KEY is required")
	}
	if !pinnedModel.MatchString(model) {
		t.Fatal("TYPESAFE_PINNED_MODEL must be a pinned jev-X.Y.Z version, not an alias")
	}
	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 0
	client, err := typesafe.NewClient(typesafe.WithAPIKey(key), typesafe.WithDefaultModel(model), typesafe.WithRetryPolicy(policy))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := client.ListModels(ctx); err != nil {
		t.Fatal(err)
	}
	response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{State: "Synthetic: the light is on.", Questions: map[string]typesafe.Question{"observation": typesafe.Noul("Is the light on?", nil)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := response.Answers["observation"]; !ok {
		t.Fatal("requested answer missing")
	}
}
