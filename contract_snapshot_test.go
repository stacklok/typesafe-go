package typesafe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestOpenAPISnapshotProvenance(t *testing.T) {
	data, err := os.ReadFile("testdata/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got, want := hex.EncodeToString(sum[:]), "a191f8a7df6bd6fedced8120dd0fd106f88575d1d1c8360d08900a6c7c0360d5"; got != want {
		t.Fatalf("OpenAPI snapshot SHA-256 = %s, want %s", got, want)
	}
	var document struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Version string `json:"version"`
		} `json:"info"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" || document.Info.Version != "0.2.0" {
		t.Fatalf("unexpected snapshot versions: OpenAPI %q, API %q", document.OpenAPI, document.Info.Version)
	}
	choice := document.Components.Schemas["ChoiceAnswer"].Properties["choice"]
	var description string
	if err := json.Unmarshal(choice["description"], &description); err != nil || description != "The name of the choice with the highest probability among the question's criteria." {
		t.Fatalf("Choice argmax provenance changed: %q (%v)", description, err)
	}
	for _, field := range []string{"input_tokens", "output_tokens"} {
		if _, hasMinimum := document.Components.Schemas["Usage"].Properties[field]["minimum"]; hasMinimum {
			t.Fatalf("Usage.%s unexpectedly gained a schema minimum; review semantic validation documentation", field)
		}
	}
}
