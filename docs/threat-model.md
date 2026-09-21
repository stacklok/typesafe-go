# Threat model

## Assets and boundaries

Assets are the bearer token, request state/questions, model answers, usage metadata, and policy decisions made by applications. Trust boundaries exist between the application and SDK, SDK and `net/http` transport, host/proxy/network and TypeSafe service, and untrusted service/model output and application policy.

## Mitigations

- Credentials are explicit. API keys are rejected when blank/newline-containing, placed only in protected Authorization headers, and redacted from client formatting, JSON, and slog. API-key mode permits an ordinary transport client only as customization; it does not authenticate without `WithAPIKey`. Alternatively, an authenticated caller transport owns the header and credential lifecycle; the SDK never inspects or modifies it. An authenticated transport should use an explicit trusted gateway base URL when OAuth is gateway-owned, rather than the direct API-key endpoint.
- HTTPS is mandatory except explicit loopback HTTP. Gateway path prefixes and their escaping are preserved while dot segments, queries, fragments, and userinfo are rejected. Redirects are not followed in either authentication mode, including with a supplied client, preventing token/body forwarding. Caller clients are copied rather than mutated.
- Requests are marshaled once into SDK-owned bytes. Responses are bounded after transport decompression; bodies are always closed. Ambiguous duplicate keys and malformed/mismatched/out-of-set answers are protocol errors.
- Default errors omit payloads, arbitrary service strings, selected question IDs, and attacker-controlled request IDs. Request IDs remain explicitly inspectable metadata for callers that choose to log them safely.
- There is no implicit logger, telemetry, cache, payload persistence, environment lookup, or model allowlist.
- Per-attempt and total contexts cover sends, reads, and waits. Caller cancellation is not retried.

## Residual risks

A custom `RoundTripper`, proxy, runtime, OS, or application can log or exfiltrate credentials and payloads; the SDK cannot police caller-owned transport code. In authenticated-transport mode the SDK does not know the token and cannot redact it from transport logs or from a raw custom cause exposed through `errors.Unwrap`. The static SDK wrapper text remains payload-free. TLS endpoint compromise and service compromise remain external risks. Decompression work occurs in the HTTP stack before the SDK's post-decompression byte bound, so CPU/compression-ratio exposure is constrained by transport and context rather than byte limit alone.

Application-owned OAuth machinery can perform token acquisition or refresh using a lifecycle context captured when its client or source was created. The inference request context is not guaranteed to bound that work. Applications must configure token-endpoint TLS, HTTP timeouts, grant flow, refresh, and shutdown themselves.

A connection may fail after a POST was processed. Retries can duplicate inference, billing, or usage because no idempotency key is documented. Disabling SDK retries does not guarantee at-most-once network/server processing.

Model outputs are untrusted and adversarial state can steer them. Confidence is not correctness probability. Applications must apply review, thresholds, authorization, and data-handling policy; protocol, transport, or unknown-answer failures must fail closed or route to review, never silently allow an action.
