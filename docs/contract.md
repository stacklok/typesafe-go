# API and model contract

Observed 2026-09-21 from the public [OpenAPI document](https://api.typesafe.ai/openapi.json), API v0.2.0; the exact reviewed bytes are checked in at [`testdata/openapi.json`](../testdata/openapi.json) with provenance and SHA-256 in [`testdata/README.md`](../testdata/README.md). Behavior was not verified with a paid live call during implementation. The official [API reference](https://docs.typesafe.ai/api), [Choice documentation](https://docs.typesafe.ai/primitives/choice), and [model documentation](https://docs.typesafe.ai/models) provide prose semantics and volatile service guidance.

## Endpoints and data

`Client.SystemOne` posts `state`, a resolved `model`, and typed `questions` to the configured base-path prefix plus `/v1/systemone`. The requested model may be an alias; the response model is the service-resolved name and is preserved unchanged, so equality is not required. `Client.ListModels` gets the same prefix plus `/v1/models`. Gateway prefixes retain their escaped path representation. Both return `x-typesafe-request-id` as constrained, untrusted response/error metadata. Request IDs and all service data are untrusted; default error text omits request IDs and arbitrary service strings.

## Authentication and gateway transports

Construction requires exactly one explicit strategy:

- `WithAPIKey(key)`, optionally with one `WithHTTPClient(client)`, makes the SDK set bearer authentication. `WithHTTPClient` only customizes that API-key transport and does not authenticate by itself. The client value is copied, but its transport remains shared; the caller owns idle-connection cleanup and the SDK adds no `Close` method.
- `WithAuthenticatedHTTPClient(client)` alone declares that the supplied, shared transport owns authentication. The SDK leaves `Authorization` untouched and invokes that transport on every attempt; the caller owns its lifecycle and idle-connection cleanup.

Duplicate selectors, and combinations of `WithAuthenticatedHTTPClient` with `WithAPIKey` or `WithHTTPClient`, fail regardless of option order. Errors name the relevant options and direct callers to a valid selection. The SDK does not verify authenticated transports, decode tokens, or depend on an OAuth package.

An authenticated transport is commonly used with an explicit trusted gateway URL, for example `WithBaseURL("https://gateway.example.com/typesafe")`, where the gateway owns OAuth. Without that base URL, the default direct TypeSafe endpoint is intended for SDK-managed API-key authentication. Applications own OAuth grant flow, token acquisition/refresh, lifecycle context, and token-endpoint TLS/timeouts. An inference request context may not bound token acquisition by a client or source that captured another context; bound that work separately with application-owned lifecycle context and HTTP client. See the concrete [README gateway example](../README.md#use).

State must be a non-null JSON string, object, or array. Instructions may be omitted/null or a string, object, or array. Noul criteria are optional and may contain null values. Choice criteria are an object whose values may also be null. Score criteria are ordered, non-null strings, objects, or arrays.

Responses preserve finite service probabilities, confidence, fractional Score values, string keys, structured legends, and nonnegative `int` token counts (including zero, with no invented local maximum). Nonnegative token counts are an SDK semantic validation at the trust boundary; the current `Usage` schema says integer but has no `minimum`. For Choice, the selected key must occur in the returned probability map and no returned probability may be strictly greater. Ties are valid, comparison is exact `>`, and the SDK applies no epsilon or rewriting; this intrinsic rule also applies to accepted extra Choice answers. The client does not normalize or require probability sums, recompute or compare Score, recompute confidence, round scores, compare legend content to requests, or add cross-question invariants. It does require the complete requested probability-key set, and rejects negative usage, unknown/mismatched requested answer variants, a Choice outside the requested options, out-of-set probability keys, and Score values outside the requested zero-based bounds. Score probability keys must be contiguous canonical decimal indices and exactly equal the legend keys; all probability/confidence values retain finite inclusive `[0,1]` checks. Additive object fields remain compatible. Duplicate JSON object keys are rejected as ambiguous protocol data.

## Schema and documentation discrepancies

| Topic | Live schema / Python 0.7.0 | Other documentation/client | SDK behavior |
|---|---|---|---|
| Instructions | Optional and nullable | API docs say required | Omit optional nil values; accept schema-supported null where represented; do not require instructions. |
| Score count | Minimum 1, no schema maximum | Service/model docs limit 10; 2+ is recommended for meaningful scoring | Enforce the schema minimum only. Above 10 may be rejected remotely; use 2–10 for model quality. |
| Choice count | No schema maximum | Service/model docs limit 255 | No local maximum. Above 255 may be rejected remotely. |
| Score level null | Excluded | Older advanced/JS docs accepted null | Reject before I/O. |
| State null | Excluded | Older JS accepted null | Reject before I/O. |
| Score legend values | String, object, or array | API prose emphasizes strings | Decode and preserve structured values. |
| Choice selection | `choice` is the highest-probability option; schema does not exclude an empty option key | API prose also calls it the highest-probability option | Require membership in returned probabilities and reject only when another value is strictly greater; ties and empty labels are valid, with no numeric tolerance. |
| Usage counts | Integer, with no machine-readable `minimum` | Token counts are semantically nonnegative | Require nonnegative values as explicit semantic validation, not as a claimed schema bound. |
| Probability bounds | Schema descriptions specify 0–1, but omit machine-readable minimum/maximum keywords | Public API semantics define probabilities on 0–1 | Enforce finite values in the inclusive 0–1 range; this semantic validation is intentional. |
| Unknown answer variants | Three wire variants | Some clients drop/cast them | Return `ProtocolError`; never silently omit requested answers. |

Schema leniency is not a claim that the service/model supports over-limit calls. It avoids hard-coding a changing remote limit while allowing wire-contract conformance tests.

## Models and decision semantics

Jev is a fast RLCD parallel decision model, not chat, content generation, tool execution, or a source of reasoning traces. Current model documentation identifies `jev-1.13.0` plus stable/preview aliases. Pin a version when policy thresholds matter; aliases can move. The models endpoint is not an exhaustive allowlist of pinned versions.

Confidence is a model statistic, not a probability that an answer is correct. Do not use the approximate demo confidence formula as an authorization control. Adversarial state can steer answers. Known weaknesses include numeric/date/counting tasks, multi-hop reasoning, and context rot. Noul, Choice, and separately worded negations are not mathematically interchangeable. Applications own thresholds, authorization, review, and actions.

One request can batch independent questions over one shared state. Do not batch unrelated documents merely to reduce calls. Current pricing, throughput, RPM, and token-window values are deliberately not SDK constants because they can change; consult the current model documentation.

## Retries, errors, and privacy

Defaults are two retries (three total attempts), 500 ms initial/5 s maximum subtractive-jitter backoff, 10 s per-attempt timeout, and a 30 s total budget. HTTP 408, 429, and 5xx statuses are retryable even when their error body is malformed or unreadable; configured connection errors, interrupted 2xx reads, and per-attempt timeouts are also eligible. Other 4xx statuses are not retried. A known non-2xx status returns safe `APIError` metadata despite an ordinary gzip/read failure, while an oversized body remains non-retryable and takes precedence. Server retry delays take precedence and are never shortened to fit a budget.

Attempt and total timeouts cover HTTP attempts, response-body reads, and retry backoff. They do not bound arbitrary caller `MarshalJSON` work or other request preparation before HTTP starts, or synchronous response decoding after the HTTP call completes.

A failed connection can occur after a POST was processed. The service documents no idempotency key, so retries can duplicate usage or billing. `MaxRetries: 0` disables SDK retries only; it cannot guarantee at-most-once processing or constrain a caller's custom transport.

TypeSafe states that input is not used for training, is US-hosted, and is retained as reasonably necessary; Zero Data Retention is enterprise-only. Applications must re-check current terms and classify data themselves. The SDK does not log, persist, cache, or emit telemetry, but the Go runtime, operating system, proxies, and a caller-supplied transport can observe or log destinations, timing, headers, and payloads. The SDK cannot prevent a custom transport from doing so.

A 2xx System One protocol failure always returns a nil response and no partial answers. Its `ProtocolError.Usage` is populated only after the entire body passes JSON syntax and global duplicate-key validation and `usage` independently supplies both nonnegative integer counts representable by `int`. This permits accounting when model or answer decoding fails without treating malformed, null, partial, negative, fractional, or overflowing usage as trusted metadata. Error formatting still omits response payloads. The additive field can break external unkeyed `ProtocolError` literals; use keyed literals during the pre-v1 API period.
