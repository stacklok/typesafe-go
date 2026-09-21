package typesafe

import (
	"context"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
)

// AC-API-01: the package compiles at the module path and has no module dependencies.
func TestAcceptanceModuleHasNoRuntimeDependencies(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Path != "github.com/stacklok/typesafe-go" {
		t.Fatalf("unexpected module: %#v", info)
	}
	if len(info.Deps) != 0 {
		t.Fatalf("unexpected module dependencies: %#v", info.Deps)
	}
}

// AC-SEC-02: construction causes no network call; production has no implicit logger, cache, telemetry, or environment path.
func TestAcceptanceConstructionHasNoSideEffects(t *testing.T) {
	var calls atomic.Int32
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, context.Canceled
	})
	client, err := NewClient("synthetic", WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil || client == nil || calls.Load() != 0 {
		t.Fatalf("construction side effect: client=%v calls=%d err=%v", client, calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = client.ListModels(ctx)
	if calls.Load() != 0 {
		t.Fatal("canceled call reached transport")
	}
}

// AC-DOC-01 and AC-DOC-02: contract and README contain discrepancy, privacy, model, and retry-risk guidance.
// AC-GOV-01: governance artifacts and repository-specific policy are present.
// AC-CI-01: CI contains the required test, race, vet, fuzz, and vulnerability checks.
// AC-LIVE-01: live conformance remains build-tagged, opt-in, and pinned-model-only.
func TestAcceptanceRepositoryArtifacts(t *testing.T) {
	checks := map[string][]string{
		"README.md":                {"duplicate", "Confidence", "Adversarial", "jev-1.13.0"},
		"docs/contract.md":         {"Schema and documentation discrepancies", "Zero Data Retention", "255", "2–10"},
		"LICENSE":                  {"Copyright 2026 Stacklok, Inc."},
		"SECURITY.md":              {"stacklok/typesafe-go/security/advisories/new", "security@stacklok.com", "within 24 hours", "1–7 days", "1–21 days"},
		"CONTRIBUTING.md":          {"Signed-off-by", "Chris Beams", "good first issue"},
		".github/workflows/ci.yml": {"permissions:\n  contents: read", "go test -race ./...", "go vet ./...", "FuzzDecodeSystemOneResponse", "govulncheck-action@b625fbe08f3bccbe446d94fbf87fcc875a4f50ee"},
		"integration/live_test.go": {"//go:build live", "TYPESAFE_LIVE_TEST", "TYPESAFE_PINNED_MODEL", "pinnedModel"},
	}
	for path, wants := range checks {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, want := range wants {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
	}
}

// Focused executable evidence (in addition to repository-artifact checks above):
//   - AC-API-02/03/06/07/08: TestPublicClientContractFixturesAndHeaders,
//     TestPublicSystemOneBaselineZerosAndAdditiveFields,
//     TestPublicSystemOneRequiredFieldsMissingAndNull,
//     TestPublicSystemOneAdversarialResponseMatrix, and
//     TestPublicSystemOneAcceptsNestedArbitraryJSONNumbers.
//   - AC-API-04/05/09: TestACAPI03And04SchemaLeniencyAndValidation,
//     TestACAPI05InputFailuresDoNotCallTransport, and TestACAPI09Models.
//   - AC-SEC-01/03/04/05/06: TestACSEC01And05SecretSafeFormatting,
//     TestRedirectMatrixNeverFollowsOrRetries,
//     TestPublicTransportClosesEveryResponseBodyExactlyOnce,
//     TestDefaultTransportBoundsDecompressedGzipAtExactLimit, and
//     TestACSEC06ConcurrentClientAndOwnership.
//   - AC-REL-01..06: TestPublicRetryConnectionAndAttemptTimeoutFlags,
//     TestPublicRetryCountersDefaultMaximum, TestACREL02BodyReplayAndMarshalOnce,
//     TestRetryAfterPrecedenceAndBudgetsScheduleActualRetry,
//     TestPublicRetryBackoffReturnsCallerCauseWithoutNextAttempt,
//     TestCancellationAndTimeoutCloseResponseBody, and
//     TestACREL06ProtocolAndOversizeNeverRetry.
//   - AC-DOC-03: the named TestACDOC03 tests under each examples directory;
//     RAG uses TestACDOC03RAGRankingIsOrderedAndConcurrencyBounded.
