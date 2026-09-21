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
	client, err := NewClient(WithAPIKey("synthetic"), WithHTTPClient(&http.Client{Transport: transport}))
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

// AC-CI-02: lightweight artifact checks complement the executable release-script tests.
// These checks intentionally do not claim to provide complete YAML security validation.
func TestAcceptanceReleaseWorkflowArtifact(t *testing.T) {
	data, err := os.ReadFile(".github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	verify := releaseWorkflowJob(t, workflow, "verify")
	publish := releaseWorkflowJob(t, workflow, "publish")

	for _, want := range []string{
		"name: Release\n\non:\n  push:\n    tags:\n      - 'v*'\n\npermissions:\n  contents: read",
		"if: github.event.created == true && github.event.deleted == false",
		"TYPESAFE_LIVE_TEST: '0'",
		"actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955",
		"actions/setup-go@44694675825211faa026b3c33043df3e48a5fa00",
		"ref: ${{ github.sha }}", "fetch-depth: 0",
		"persist-credentials: false", "repo-checkout: false", "cache: false",
		"gofmt -l .", "go mod tidy", "git diff --exit-code", "git ls-files --others --exclude-standard",
		"go test ./...", "go test -race ./...", "go vet ./...", "go list -deps ./...",
		"FuzzDecodeSystemOneResponse", "FuzzValidateRequestJSON",
		"govulncheck-action@b625fbe08f3bccbe446d94fbf87fcc875a4f50ee",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
	if strings.Contains(workflow, "workflow_dispatch:") || strings.Contains(workflow, "pull_request:") || strings.Contains(workflow, "pull_request_target:") || strings.Contains(workflow, "    branches:") {
		t.Error("release workflow has a non-tag trigger")
	}
	for _, line := range strings.Split(workflow, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- uses: ") {
			continue
		}
		ref, ok := strings.CutPrefix(line, "- uses: ")
		if comment := strings.IndexByte(ref, ' '); comment >= 0 {
			ref = ref[:comment]
		}
		_, pin, ok := strings.Cut(ref, "@")
		if !ok || len(pin) != 40 || strings.Trim(pin, "0123456789abcdef") != "" {
			t.Errorf("release action is not pinned to a full commit: %q", line)
		}
	}
	if strings.Contains(verify, "permissions:") {
		t.Error("verify must inherit the workflow's default read-only permissions")
	}
	if strings.Count(workflow, "cache: false") != 2 {
		t.Error("every release Go tool setup must disable caching")
	}
	if !strings.Contains(publish, "needs: verify") {
		t.Error("publish must depend only on verify")
	}
	if strings.Count(workflow, "contents: write") != 1 || !strings.Contains(publish, "contents: write") {
		t.Error("release workflow must have exactly one job-scoped write permission")
	}
	for _, forbidden := range []string{"actions/checkout", "actions/setup-go", "govulncheck", "go test", "go run"} {
		if strings.Contains(publish, forbidden) {
			t.Errorf("publish job executes or checks out repository code: %q", forbidden)
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
