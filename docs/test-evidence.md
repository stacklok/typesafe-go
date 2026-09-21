# Implementation evidence

Status: implemented and offline-verified on 2026-09-21. The repository includes code, tests, examples, documentation, governance, and CI. Live service behavior remains unverified because no paid conformance run was authorized.

Validation completed locally after the focused authentication follow-up:

- `GOTOOLCHAIN=go1.26.6 go test ./...`: passed, including all six examples and the authenticated-transport executable example.
- `GOTOOLCHAIN=go1.26.6 go test -race ./...`: passed.
- `GOTOOLCHAIN=go1.26.6 go vet ./...`: passed.
- `GOTOOLCHAIN=go1.26.6 go test -run '^$' -tags live ./...`: passed compilation without a live call.
- `git -C typesafe-go diff --check`: passed.

The focused follow-up did not repeat the unchanged JSON-parser fuzz runs; their prior evidence remains recorded below.
Transport-authentication evidence maps to executable tests:

- AC-AUTH01 constructor selection, invalid values, nil clients, duplicate selectors, conflicts in both orders, missing authentication (including ordinary-client-only), and API-key plus ordinary-client success: `TestACAUTH01AuthenticationSelection`.
- AC-AUTH02 public GET/POST authentication, API-key plus ordinary transport composition in both option orders, no SDK Authorization in transport mode, independently retried GET and POST requests with rotating per-attempt credentials and replayed POST bodies, and one-attempt GET/POST 401/403 behavior: `TestACAUTH02AuthenticatedTransportRunsForEveryAttemptAndRoute`, `TestACAUTH02APIKeyWithHTTPClientAddsAuthorizationForGETAndPOST`, and `TestACAUTH02APIKeyModesAndAuthenticationFailuresDoNotRetry`.
- AC-AUTH03 copied client fields and redirect isolation for 301/302/307/308, GET/POST, and both authentication modes: `TestACAUTH03HTTPClientCopiedAndRedirectDisabled` and `TestACAUTH03RedirectMatrixNeverForwardsCredentialsOrBodies`.
- AC-AUTH04 root/prefixed/trailing/escaped gateway RequestURI preservation and malformed/query/fragment/userinfo/dot-path rejection: `TestACAUTH04GatewayPrefixesPreserveEscaping` and `TestACAUTH04InvalidBaseURLs`.
- AC-AUTH05 transport-mode request IDs, hostile/control/oversize filtering, no placeholder secret, static error formatting, explicit custom-cause residual risk, and redacted client String/GoString/JSON/slog output: `TestACAUTH05TransportModeRequestIDsAndRedaction`, `TestACAUTH05TransportClientAndErrorsRemainRedacted`, and the retained API-key echo tests.
- AC-AUTH06 source/tests/examples/live-tagged compilation and a standard-library authenticated transport: `Example_withAuthenticatedHTTPClient`; the README separately shows application-owned `oauth2.NewClient` without adding an SDK dependency.

Earlier focused regression evidence remains mapped to executable test names rather than file-wide claims:

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
