# Implementation evidence

Status: implemented and offline-verified on 2026-09-21. The repository includes code, tests, examples, documentation, governance, and CI. Live service behavior remains unverified because no paid conformance run was authorized.

Validation completed locally after the review fixes:

- `GOTOOLCHAIN=go1.26.6 go test ./...`: passed.
- `GOTOOLCHAIN=go1.26.6 go test -race ./...`: passed.
- `GOTOOLCHAIN=go1.26.6 go vet ./...`: passed.
- `GOTOOLCHAIN=go1.26.6 go test -run '^$' -fuzz '^FuzzDecodeSystemOneResponse$' -fuzztime=10s .`: passed.
- `GOTOOLCHAIN=go1.26.6 go test -run '^$' -fuzz '^FuzzValidateRequestJSON$' -fuzztime=10s .`: passed.
- `GOTOOLCHAIN=go1.26.6 go test -run '^$' -tags live ./...`: passed compilation without a live call.
- `GOTOOLCHAIN=go1.26.6 go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...`: no vulnerabilities found.

Focused regression evidence is mapped to executable test names rather than file-wide claims:

- Public response requirements and adversarial protocol behavior: `TestPublicSystemOneBaselineZerosAndAdditiveFields`, `TestPublicSystemOneRequiredFieldsMissingAndNull`, and `TestPublicSystemOneAdversarialResponseMatrix`.
- Arbitrary nested JSON-number request shapes and exact structured-legend number preservation: `TestPublicSystemOneAcceptsNestedArbitraryJSONNumbers`.
- Public retry flags, counters, scheduling, caller cancellation/deadline causes, and policy validation: `TestPublicRetryConnectionAndAttemptTimeoutFlags`, `TestPublicRetryCountersDefaultMaximum`, `TestRetryAfterPrecedenceAndBudgetsScheduleActualRetry`, `TestPublicRetryBackoffReturnsCallerCauseWithoutNextAttempt`, `TestPublicRetryCallerDeadlineDuringBackoffReturnsCauseWithoutNextAttempt`, and `TestRetryPolicyValidationErrorsArePerField`.
- Exact-once response-body closure and typed error metadata: `TestPublicTransportClosesEveryResponseBodyExactlyOnce`, `TestPublicTransportCancellationClosesBodyExactlyOnceAndReturnsCause`, and `TestCancellationAndTimeoutCloseResponseBody`.
- Meaningful request/response fuzz invariants: `FuzzValidateRequestJSON` and `FuzzDecodeSystemOneResponse`.
- Number-safe canonical request fixture comparison and observable RAG concurrency bound: `TestPublicClientContractFixturesAndHeaders` and `TestACDOC03RAGRankingIsOrderedAndConcurrencyBounded`.
- Existing exact composite-weight and skill abstain/recommend behavior remains covered by `TestACDOC03CallerOwnedWeighting` and `TestACDOC03SkillSelectionAbstainsOrRecommendsOnly`.

Probability values intentionally remain finite and within 0–1. The public schema descriptions and probability semantics require that range even though machine-readable minimum/maximum keywords are absent; negative and above-one rejection cases remain in `TestPublicSystemOneAdversarialResponseMatrix`.

This records only assertions present in the offline tests; it does not claim live API conformance or release status.

The optional live test is build-tagged, environment-gated, pinned-model-only, context-bounded, and has SDK retries disabled.

No community SDK source was copied. The implementation is fresh, uses only the Go standard library at runtime, and therefore needs no third-party code notice.
