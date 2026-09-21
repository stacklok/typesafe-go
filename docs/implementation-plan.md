# TypeSafe Go client implementation plan

Status: implemented and offline-verified on 2026-09-21. The repository includes code, tests, examples, documentation, governance, and CI. Full live endpoint and boundary conformance remains unverified.

## 1. Scope and architectural decision

Build `github.com/stacklok/typesafe-go`, package `typesafe`, as a small, synchronous, concurrency-safe Go library with no runtime dependencies outside the standard library. It supports the complete live API v0.2.0 surface as researched on 2026-09-21:

- `POST /v1/systemone`
- `GET /v1/models`
- bearer authentication against `https://api.typesafe.ai`
- Noul, Choice, and Score questions and answers
- request IDs and model-list metadata

Do not add service scaffolding, code generation, agent/tool frameworks, chat abstractions, model allowlists, pricing/rate-limit calculations, policy thresholds, authorization decisions, execution hooks, caching, telemetry, or implicit logging. The top-level package is the boundary: public request/response/question/error/configuration types; private wire decoding, validation, retries, and transport.

### Reuse decision: fresh implementation, informed by prior art

Use a fresh implementation. Do not copy code from the community clients. Their MIT licenses permit reuse, but none is a safe base without substantial subtraction and contract changes. Fresh code is the smaller and more auditable path for this two-endpoint library. If implementation later copies a non-trivial expression, test, comment, or fixture, add the source's MIT notice to `THIRD_PARTY_NOTICES.md` and retain the applicable copyright; otherwise do not add a misleading third-party notice.

Research was against these exact Git revisions:

| Repository | Revision | Useful prior art | Why it is not the base |
|---|---|---|---|
| [`unimtx/typesafe-sdk-go`](https://github.com/unimtx/typesafe-sdk-go/tree/c87b35da062b5fd0d7f7c72c8b2830b2750670ea) | `c87b35da062b5fd0d7f7c72c8b2830b2750670ea` | Typed question variants, response dispatch, body replay, retry-header parsing, explicit parity notes | Considerably broader public option/logging/raw-response machinery than needed; debug logging includes payload bodies; its design text locally enforces stale Score 2..10 and Choice 255 limits; transport reads unbounded bodies. |
| [`Stumble/jev-go`](https://github.com/Stumble/jev-go/tree/a475dc925ba68602be93f4478e1381cf5ec27ee4) | `a475dc925ba68602be93f4478e1381cf5ec27ee4` | Explicit config, loopback-only HTTP, copied client with redirects disabled, validation of requested answers, simple retry loop | Adds Vercel/provider/CLI/skill surfaces unrelated to the API; stale Score 2..10 and Choice 255 validation; validates and recomputes probability distributions and legends rather than preserving server values; API errors retain and print server strings that may echo input. |
| [`zhirschtritt/typesafe-go`](https://github.com/zhirschtritt/typesafe-go/tree/a4062e525d1ad23aa739d3454247bcec52486a57) | `a4062e525d1ad23aa739d3454247bcec52486a57` | Small flat package, sealed question types, bounded reads, typed protocol errors, separate model response | Implicit environment configuration; accepts unsafe non-HTTPS base URLs; supplied clients may follow redirects; stale Score 2..10 and Choice 255 limits; API and validation errors retain/format response bodies; no total retry budget. |

All three are MIT licensed (2026 copyrights respectively Unimatrix, Stumble, and Zach Hirschtritt). Concepts and Go idioms are not copied expression; implementation must be clean-room from this contract and the live schema. Official [Python `v0.7.0`](https://github.com/typesafe-ai/typesafe-sdk-python/tree/v0.7.0/src/typesafe_sdk) is the closest current wire reference (Score min 1, non-null levels); official [JavaScript `v0.6.0`](https://github.com/typesafe-ai/typesafe-sdk-js/tree/v0.6.0/src) is useful for retry defaults but has older Score/null-state types and unchecked response casting.

## 2. Source-of-truth and discrepancy policy

Wire types follow the live OpenAPI 3.1 schema at `https://api.typesafe.ai/openapi.json`, API version 0.2.0, as observed on 2026-09-21. Documentation gives model-use guidance, not stricter client-side wire validation. Record the following in `docs/contract.md` and do not silently combine them:

| Topic | Live schema / Python 0.7.0 | Other documentation/client | Client decision |
|---|---|---|---|
| Instructions | Optional and nullable | API docs say required | Encode omitted optional values canonically; accept explicit/nested JSON null where represented; do not require instructions. |
| Score count | Minimum 1, no maximum | Service/model docs limit 10; 2+ is recommended for meaningful scoring; JS 0.6.0 requires 2+ | Enforce only one or more criteria. Calls above the documented service/model limit may be rejected remotely; recommend 2..10 without treating 2 as the schema minimum. |
| Choice count | No schema maximum | Service/model docs limit 255 | Do not enforce 255 locally. Clearly warn that calls above the documented service/model limit may be rejected remotely. |
| Score level null | Excluded | Advanced docs and JS accept null | Reject null Score entries before I/O. |
| State null | Excluded | JS accepts null | Reject root null before I/O. |
| Score legend values | String, object, or array | API prose says strings | Decode and preserve structured values. |
| Probability bounds | Descriptions specify 0–1 but omit machine-readable bound keywords | Public API semantics define probabilities on 0–1 | Require finite values in the inclusive 0–1 range; retain tests rejecting negative values and values above 1. |
| Unknown answer variants | Wire currently has three | Python drops them; JS casts unchecked | A missing, malformed, unknown, or wrong-kind requested answer is a `ProtocolError`; never silently omit it. Ignore unknown object fields for additive compatibility. |

Do not validate a model against `GET /v1/models`: the endpoint lists available aliases but is not an exhaustive allowlist of pinned versions.

## 3. Public contract to implement

Final spelling can change only if the implementer records the reason in this document before coding. Keep all of these in package `typesafe`; do not create an `option`, `transport`, or generated-schema public subpackage.

### Client and calls

```go
const DefaultBaseURL = "https://api.typesafe.ai"

type Client struct { /* immutable private configuration */ }

func NewClient(opts ...Option) (*Client, error)
func (c *Client) SystemOne(ctx context.Context, req SystemOneRequest) (*SystemOneResponse, error)
func (c *Client) ListModels(ctx context.Context) (*ModelsResponse, error)
```

`NewClient` performs no network call or environment lookup and requires exactly one authentication strategy. `WithAPIKey` rejects blank/newline-containing keys and lets the SDK set bearer authentication. `WithAuthenticatedHTTPClient` copies a caller-owned authenticated client and leaves `Authorization` entirely to its transport. `WithHTTPClient` is only transport customization and cannot authenticate by itself. Duplicate selectors and API-key/authenticated-client or ordinary/authenticated-client conflicts fail regardless of option order; API key plus one ordinary `WithHTTPClient` is valid. Configuration errors name the conflicting option(s) and direct callers to `WithAPIKey` or `WithAuthenticatedHTTPClient` as appropriate. The client is safe for concurrent calls when an injected `http.RoundTripper` is itself safe. It must not mutate caller options, requests, maps/slices, or an injected `*http.Client`.

Use ordinary functional `Option` values for this small constructor only:

- `WithAPIKey(string)`: explicit SDK-managed bearer authentication; no implicit environment source.
- `WithAuthenticatedHTTPClient(*http.Client)`: copy a client whose caller-owned transport manages authentication; this is a declaration, not SDK verification of authentication. It is mutually exclusive with API-key authentication and `WithHTTPClient`.
- `WithBaseURL(string)`: default HTTPS endpoint; permit path prefixes and append `/v1/*` without discarding or re-escaping them. Permit plaintext HTTP only for an explicit `localhost` or loopback-IP URL for tests. Reject userinfo, query (including bare `?`), fragment (including bare `#`), dot segments including percent-encoded forms, and missing host/scheme. No environment override.
- `WithHTTPClient(*http.Client)`: copy the client value for API-key mode; retain its transport, jar, timeouts, and other fields while forcing `CheckRedirect` on the copy to return `http.ErrUseLastResponse`. Redirect responses are errors and are never followed, so neither token nor POST body can cross origins.
- `WithDefaultModel(string)`: default `jev-latest`; this is a convenience default, not an allowlist.
- `WithAttemptTimeout(time.Duration)`: default 10 seconds, applying through request context to send and body read.
- `WithRetryPolicy(RetryPolicy)`: immutable copy; `MaxRetries: 0` disables retries.
- `WithResponseLimit(int64)`: one bounded limit for both success and error bodies after Go transport decompression; default 4 MiB.

In API-key mode set `Authorization`, `Accept`, `Content-Type` for POST, a versioned `User-Agent`, and retry count internally. In authenticated-transport mode never read, set, delete, decode, or log `Authorization`; the wrapper runs on every retry and remains caller-owned. The SDK does not import OAuth code or invent a token-provider interface.

### Requests and questions

```go
type Content = any

type SystemOneRequest struct {
    State     Content
    Model     string // empty uses the client's default
    Questions map[string]Question
}

type Question interface { /* sealed marker */ }

type NoulCriteria struct { True, False Content }
type NoulQuestion struct { Instructions Content; Criteria *NoulCriteria }
type ChoiceQuestion struct { Instructions Content; Criteria map[string]Content }
type ScoreQuestion struct { Instructions Content; Criteria []Content }

func Noul(instructions Content, criteria *NoulCriteria) NoulQuestion
func Choice(instructions Content, criteria map[string]Content) ChoiceQuestion
func Score(instructions Content, criteria ...Content) ScoreQuestion
```

Concrete question marshalers add immutable discriminators. Helpers copy top-level maps/slices so later caller mutation does not change a constructed question; nested JSON remains caller-owned and must not be concurrently mutated. `SystemOne` resolves the model and marshals once into owned bytes, then validates the serialized JSON shape and reuses the exact bytes for retries. This supports ordinary structs, maps, slices/arrays, strings, `json.RawMessage`, nested numbers/booleans/null, and custom marshalers without reflection-based guesses.

Local wire checks before I/O:

- State root is string, object, or array, and not null.
- Questions is nonempty; IDs are nonempty (do not trim or rewrite valid IDs).
- Every question is non-nil and has the fixed recognized discriminator.
- Instructions, when non-null, are string/object/array.
- Noul criteria is omitted when nil; its `true`/`false` values may be null.
- Choice criteria is an object. Values may be string/object/array/null. Do not impose undocumented count, label, or sum rules.
- Score criteria has at least one entry; every entry is a non-null string/object/array. Order is preserved.
- Values must be valid JSON: reject NaN/infinity, unsupported map keys/channels/functions/cycles, invalid `RawMessage`, and multiple JSON values through normal marshaling/decoding errors.

There is no `RawQuestion` or generic top-level extension map initially. Add a typed field when the live API adds one. This keeps malformed discriminators and accidental field overrides out of the public surface.

### Responses

```go
type Answer interface { AnswerType() string /* sealed by private method too */ }
type NoulAnswer struct { Noul float64 }
type ChoiceAnswer struct {
    Choice string; Probabilities map[string]float64; Confidence float64
}
type ScoreAnswer struct {
    Score float64; Probabilities map[string]float64
    Legend map[string]Content; Confidence float64
}
type Usage struct { InputTokens, OutputTokens int }
type SystemOneResponse struct {
    Model string; Answers map[string]Answer; Usage Usage; RequestID string
}
type Model struct { Name, Description, ReleaseDate string }
type ModelsResponse struct { Models []Model; RequestID string }
```

Decode through private pointer-bearing wire structs so a required field missing/null is distinguishable from a legitimate zero or empty value. Require response `model`, `answers`, both usage counts, and every schema-required answer field. Validate finite probabilities/confidence/score and schema ranges (`noul` and confidence in [0,1]); do not normalize distributions, require exact sums, recompute confidence, round scores, compare legends to requests, or replace server values. Score is the zero-based fractional expected index. Legend keys and probability keys stay strings exactly as sent.

For every requested ID, require an answer of the matching discriminator. Missing, null, malformed, unknown, or mismatched requested answers produce `ProtocolError`. Extra JSON fields are ignored. Extra answer IDs may be decoded if they use a known discriminator; an unknown answer discriminator anywhere is a protocol error rather than silent data loss.

Validate model cards have nonempty name/description and a strict `YYYY-MM-DD` calendar date while retaining the original string. An empty `models` array is valid. Return request ID from `x-typesafe-request-id` on both endpoint responses and errors.

### Errors and secret-safe formatting

Implement these inspectable errors and sentinels:

- `APIError { StatusCode int; RequestID string; RetryAfter time.Duration }`, matching `ErrAPI` through `errors.Is` and discoverable with `errors.As`.
- `ProtocolError { RequestID, Field, Reason string }`, matching `ErrProtocol`; `Reason` is a fixed SDK classification and no decoder error or raw response body is retained.
- `ResponseTooLargeError { Limit int64 }`, matching `ErrResponseTooLarge`.
- connection/attempt-timeout wrappers that unwrap the transport cause and match stable sentinels; caller cancellation/deadline must continue to satisfy `errors.Is(err, context.Canceled/DeadlineExceeded)`.
- construction/input errors with stable, payload-free messages.

Never put API keys, request state/questions, response bodies, or server-provided 4xx/5xx strings into error text or exported error fields. In particular, a 422 body may echo submitted input or a validation `msg`; drain only up to the bound and discard it. `APIError.Error()` contains only status text/code and request ID. `Client` implements redacted `String`, `GoString`, and `slog.LogValuer` (or is shaped so default formatting cannot reveal the private key); test `%v`, `%+v`, `%#v`, JSON, and slog formatting. There is no implicit payload logging, telemetry, or cache.

### Retries, deadlines, and body ownership

```go
type RetryPolicy struct {
    MaxRetries int              // default 2
    BackoffInitial time.Duration // 500ms
    BackoffMax time.Duration     // 5s
    BackoffJitter float64        // 0.25
    TotalBudget time.Duration    // 30s, including attempts and waits
    RetryConnectionErrors bool   // true
    RetryTimeouts bool           // true
}
func DefaultRetryPolicy() RetryPolicy
```

Retry HTTP 408, 429, and all 5xx including 529, connection errors, interrupted body reads, and per-attempt timeouts according to policy. Never retry local validation/protocol errors, body-too-large errors, redirects, or other 4xx. Parse `retry-after-ms` first, then `Retry-After` decimal seconds or HTTP date. A valid server delay takes precedence over backoff. If it cannot complete inside the lesser of caller deadline and total budget, return the current error immediately; never substitute a shorter delay and retry early. Invalid/missing headers use capped exponential subtractive jitter. Inject private clock/random/wait seams only as needed for deterministic tests; do not expose them.

Create one total-budget child context per call and one per-attempt child context. Both govern request send, bounded body read, and waits. Serialize POST once and create a fresh `bytes.Reader` each attempt (`GetBody` may also be set). Always close response bodies. Document that a connection can fail after the server processed a POST; retries may duplicate usage/billing because the API documents no idempotency key. Users that cannot accept SDK-initiated retries must set `MaxRetries: 0`, while recognizing this does not guarantee at-most-once processing or constrain transport/network behavior.

## 4. Domain guidance to document, not encode

`README.md` and `docs/contract.md` must clearly state:

- Jev is a fast RLCD parallel decision model, not chat, content generation, tool execution, or a source of reasoning traces.
- Model docs currently identify `jev-1.13.0` and stable/preview aliases. Pin model versions when thresholds matter. Never hardcode current pricing ($0.042/M input, free output), throughput (250k tokens/s), RPM (1200), or token-window values (64k state+all questions, 32k state+longest question) as validation/config defaults; link to model docs because these can change.
- Confidence is a model statistic, not a probability that the answer is correct. Do not implement the approximate demo formula.
- Adversarial state can steer outputs; known weaknesses include numeric/date/counting, multi-hop, and context rot. Noul/Choice and separately phrased negations are not mathematically interchangeable. Local application policy owns thresholds, authorization, and actions.
- One request can batch independent questions over one shared state. Do not batch unrelated documents merely to save calls.
- TypeSafe states input is not used for training, is US-hosted, and retained as reasonably necessary; Zero Data Retention is enterprise-only. Applications must make their own data-classification decision and re-check current privacy terms.

## 5. File-level implementation sequence

Keep production code flat and cohesive. Tests may be larger than production because transport/security behavior is the product.

1. **Repository/governance**
   - `go.mod`: module path, Go 1.26 baseline; no runtime requirements.
   - `.gitignore`: Go test binaries, coverage/profiles, fuzz cache/corpus outputs, editor/OS outputs; do not ignore source fixtures.
   - `LICENSE`: full Apache-2.0 text, `Copyright 2026 Stacklok, Inc.`
   - `README.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `dco.md`, `renovate.json`; adapt the read-only ToolHive templates and repository-specific advisory URL. Include all requirements from the parent `STACKLOK_OSS_PROJECT_REQUIREMENTS.md` (Contributor Covenant 1.4, DCO and Chris Beams commit guidance, security timelines/contacts/GPG/Discord). Add `MAINTAINERS.md`/`.github/CODEOWNERS` only after maintainers/owners are confirmed; do not invent names.
2. **Public domain types**
   - `questions.go`: sealed variants, constructors, top-level ownership copies, custom marshaling.
   - `responses.go`: public responses/answers/models and private discriminated decoding.
   - `types_test.go`, `fuzz_test.go`: exact JSON fixtures, omitted/null/zero distinctions, structured values, invalid numeric/JSON shapes, malformed discriminators.
3. **Configuration and errors**
   - `client.go`: immutable client, explicit constructor, public methods.
   - `options.go`: small constructor options and URL/key validation.
   - `errors.go`: safe typed errors, sentinels, redacted formatting.
   - `client_test.go`, `errors_test.go`: copy isolation, concurrent use, secret/payload non-disclosure, `errors.Is/As`.
4. **Transport and reliability**
   - `transport.go`: request creation, protected headers, no redirects, bounded body reads, status handling, request ID.
   - `retry.go`: validated policy, classification, Retry-After parsing, budget-aware cancellable backoff.
   - `transport_test.go`, `retry_test.go`: offline `httptest`/custom `RoundTripper` tests for all failure paths and body replay.
5. **Contract and user documentation**
   - `docs/contract.md`: live request/response contract, discrepancy table, semantics, model guidance, source URLs and observed versions/date.
   - `docs/threat-model.md`: assets (token/payload/results), trust boundaries, redirects/plaintext/error/logging/decompression/retry duplication risks, mitigations, residual application risks.
   - `docs/releasing.md`: pre-v1 SemVer policy, tag/module checks, changelog and provenance steps; do not claim v1 stability.
   - Six compact, tested examples in separate `examples/<scenario>/` packages using local `httptest` fixtures and no network/secrets: `support-triage` (mixed primitives), `skill-selection` (abstain and return a recommendation only), `rag-ranking` (bounded goroutine concurrency), `composite-prioritization` (caller-owned weighting), `candidate-extraction` (local parsing then model selection), and `security-routing` (transport/protocol errors route to review, never pass). No agent framework or tool execution.
6. **Conformance and automation**
   - `testdata/`: hand-reviewed request/response fixtures derived from the live schema, containing synthetic data only.
   - `integration/live_test.go`: build tag `live` plus explicit `TYPESAFE_LIVE_TEST=1`, key and pinned model variables; one tiny synthetic conformance request and model list. Never run by default or now. Do not assert a particular model judgment.
   - `.github/workflows/ci.yml`: minimal `contents: read`; Go 1.26; test, race, vet, fuzz-smoke, and govulncheck jobs. Pin every action to a verified full commit SHA with a release-tag comment. No `pull_request_target`, write permissions, secrets, artifact execution, or live calls on PRs.
   - `renovate.json`: governance-required recommended config, action digest pinning, no semantic commits, Go mod tidy.

No generated client is warranted: two stable endpoints plus security-sensitive custom response validation are cheaper to maintain directly than a generator and its runtime/dependency surface.

## 6. Acceptance criteria and test map

Each acceptance ID must appear in a test name or a short adjacent test comment, so an independent reviewer can trace evidence.

### API and wire contract

- **AC-API-01:** `go list -deps .` shows no non-standard runtime dependency, and the public package is `github.com/stacklok/typesafe-go` / `typesafe`.
- **AC-API-02:** POST emits exactly resolved `state`, `model`, and `questions` with fixed question discriminators; GET uses `/v1/models`; both use bearer auth and surface `x-typesafe-request-id`.
- **AC-API-03:** Noul/Choice/Score accept all live-schema JSON shapes; nested numbers, booleans, and null survive; root null state and null Score levels fail before transport.
- **AC-API-04:** Score accepts one level and more than ten; Choice can exceed 255. Tests make clear these are wire-contract checks, not model-quality recommendations.
- **AC-API-05:** blank API key or explicit default model, empty questions/ID, typed-nil question, invalid JSON, NaN/infinity, and unsupported roots fail without an HTTP attempt.
- **AC-API-06:** response zero values (`noul:0`, `score:0`, `confidence:0`, token counts 0) decode successfully; omission/null of each required field fails distinctly as `ProtocolError`.
- **AC-API-07:** missing requested answer, wrong discriminator, unknown discriminator, malformed answer, out-of-range/non-finite schema values fail safely; additive object fields do not.
- **AC-API-08:** distributions, confidence, scores, score keys, and structured legends are returned exactly without normalization/recomputation/rounding; model names are never allowlisted.
- **AC-API-09:** models decode aliases/cards, validate real dates, permit an empty list, and expose response request ID.

### Security and transport

- **AC-SEC-01:** API key is absent from all supported client/error formatting, JSON, slog, test failures, and returned response metadata.
- **AC-SEC-02:** no logger, telemetry, cache, environment read, or payload persistence occurs implicitly.
- **AC-SEC-03:** HTTPS is required except explicit loopback test URLs; malformed/userinfo/query/fragment URLs fail. No redirect is followed with either default or supplied `http.Client`; redirected endpoint receives neither token nor body.
- **AC-SEC-04:** success and error bodies, including transparently decompressed gzip responses, are bounded. Oversize produces `ResponseTooLargeError`, closes the body, and never retries.
- **AC-SEC-05:** a synthetic 422 body containing state and server `msg` is absent from `Error()`, `%+v`, exported error fields, and logs; status and request ID remain inspectable with `errors.As`.
- **AC-SEC-06:** caller clients/options/maps/slices/requests are not mutated; constructor copies configuration; `go test -race ./...` covers shared-client calls.

### Retry and cancellation

- **AC-REL-01:** defaults perform at most three attempts and retry 408, 429, every 5xx including 529, configured connection failures, interrupted reads, and per-attempt timeouts; `MaxRetries:0` performs one attempt.
- **AC-REL-02:** POST body bytes are identical on every retry and caller marshalers run once.
- **AC-REL-03:** `retry-after-ms` wins over `Retry-After`; decimal seconds and HTTP dates work; invalid values use deterministic tested jitter/backoff.
- **AC-REL-04:** a server delay or next attempt that cannot fit in total/caller budget returns the current error without sleeping past budget or retrying early.
- **AC-REL-05:** cancellation/deadline during send, body read, and backoff returns promptly and preserves `errors.Is` for the context cause; bodies/timers are closed/stopped.
- **AC-REL-06:** redirects, protocol errors, local validation errors, non-retryable 4xx, and oversized bodies are never retried.

### Documentation, governance, and delivery

- **AC-DOC-01:** discrepancy table and all model/security/privacy guidance in sections 2 and 4 appear in user docs without presenting volatile pricing/limits as code constants.
- **AC-DOC-02:** retry duplicate-usage risk, no documented idempotency, confidence semantics, adversarial-state risk, local policy ownership, and pinning model versions for thresholds are prominent.
- **AC-DOC-03:** all six examples compile and their local fixture/policy tests run under `go test ./...`; no example performs tools/actions or treats an error as approval.
- **AC-GOV-01:** required governance files pass the parent checklist, Apache copyright is 2026 Stacklok, repo-specific links use `stacklok/typesafe-go`, and any copied MIT material has retained notice.
- **AC-CI-01:** clean CI runs tests, `-race`, vet, two named fuzz smoke targets, and govulncheck on Go 1.26 with SHA-pinned verified actions/minimal permissions and no secrets/live calls.
- **AC-LIVE-01:** live conformance is doubly gated, synthetic, tiny, pinned-model capable, and documented as optional/possibly paid. It is not run as part of implementation acceptance without explicit credentials and authorization.

Suggested commands after implementation (not in this approach stage):

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -fuzz '^FuzzDecodeSystemOneResponse$' -fuzztime=10s .
go test -run '^$' -fuzz '^FuzzValidateRequestJSON$' -fuzztime=10s .
govulncheck ./...
```

## 7. Independent review checklist

The panel should review against this plan rather than comparing line-for-line with community clients:

1. Contract reviewer traces AC-API IDs to live-schema fixtures and checks every required field/missing-vs-zero case.
2. Security reviewer focuses on token/payload exfiltration via formatting, errors, redirects, plaintext, gzip/oversize, injected clients, and retry duplication.
3. Reliability reviewer uses custom transports to force cancellation, partial bodies, lost responses, Retry-After edge cases, and races.
4. Go API reviewer checks package surface with `go doc`, constructor ergonomics, ownership, error inspection, and absence of speculative abstractions/dependencies.
5. Governance reviewer checks the parent requirements and pinned workflow provenance.

## 8. Transport-authentication implementation decision

Authentication is an explicit constructor option rather than a constructor argument. Exactly one of `WithAPIKey` and `WithAuthenticatedHTTPClient` is required. The latter declares that authentication belongs to the supplied transport; the SDK does not inspect tokens or verify that the transport authenticates. OAuth grant flow, acquisition, refresh, lifecycle context, and token-endpoint TLS/timeouts remain application concerns. An inference request context may not bound token acquisition performed with a context captured by an OAuth client.

Gateway base URLs preserve their decoded and escaped path prefix and append exactly `/v1/systemone` or `/v1/models`. Literal trailing slashes are normalized; queries, userinfo, fragments, malformed URLs, and decoded `.`/`..` segments are rejected. Redirects remain disabled on a copied HTTP client in both authentication modes.

Transport-owned credentials are unknown to the SDK and therefore cannot be redacted from caller transport logs or an unwrapped custom transport cause. SDK error text and client String/GoString/JSON/slog representations remain static and redacted.

AC-AUTH01 through AC-AUTH06 are evidenced by named `TestACAUTH*` tests and `Example_withAuthenticatedHTTPClient`: option selection and order-independent conflicts; per-attempt transport authentication and no hidden 401/403 retry; copied-client and redirect behavior; exact gateway RequestURI escaping and URL rejection; request-ID/redaction behavior; and compilation of all packages, examples, README snippets, and the live-tagged test without live calls.

## 9. Unresolved questions (non-blocking unless noted)

- **Maintainers/CODEOWNERS:** actual GitHub users/teams are not known. Confirm before adding `MAINTAINERS.md` or `.github/CODEOWNERS`; do not guess.
- **Release automation:** repository release/signing policy is not provided. Initial scope should document manual pre-v1 tagging; add publishing automation only after Stacklok chooses provenance/signing requirements.
- **Response-size default:** 4 MiB is deliberately conservative for tiny JSON responses and configurable. Security review may lower it; raising it needs evidence.
- **Model for optional live conformance:** require an explicitly supplied pinned model at run time rather than embedding today's version. No live request is authorized in this stage.
