# TypeSafe Go

A small, community SDK for TypeSafe's System One decision API, maintained by Stacklok. It is not an official TypeSafe SDK. It supports `POST /v1/systemone` and `GET /v1/models`, uses only the Go standard library at runtime, and performs no implicit logging, telemetry, caching, environment reads, or network calls during construction.

> This is a pre-v1 SDK. Its public API may change before v1.

## Install

```sh
go get github.com/stacklok/typesafe-go
```

Go 1.26 or newer is required. Create an API key in the [TypeSafe console](https://console.typesafe.ai/). Keep it out of source code, command lines, logs, and error messages.

## Use

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    typesafe "github.com/stacklok/typesafe-go"
)

func main() {
    client, err := typesafe.NewClient(
        typesafe.WithAPIKey(os.Getenv("TYPESAFE_API_KEY")),
        typesafe.WithDefaultModel("jev-1.13.0"), // pin when thresholds matter
    )
    if err != nil {
        log.Printf("configure TypeSafe client: %v", err)
        return
    }

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
        State: map[string]string{"ticket": "Refund requested after duplicate charge"},
        Questions: map[string]typesafe.Question{
            "team": typesafe.Choice("Which team should review this ticket?", map[string]typesafe.Content{
                "billing": "Billing", "support": "General support",
            }),
        },
    })
    if err != nil {
        log.Printf("classify ticket: %v", err)
        return
    }
    answer, ok := response.Answers["team"].(typesafe.ChoiceAnswer)
    if !ok {
        log.Printf("team answer has unexpected type")
        return
    }
    if _, err := fmt.Printf("recommended review team: %s (confidence statistic %.2f)\n", answer.Choice, answer.Confidence); err != nil {
        log.Printf("write result: %v", err)
    }
}
```

Applications explicitly obtain and supply credentials; the SDK does not read environment variables. The only valid constructor combinations are:

- `WithAPIKey(key)`, optionally with one `WithHTTPClient(client)`. The SDK adds `Authorization: Bearer <key>` on every attempt; `WithHTTPClient` only customizes that transport and does not authenticate on its own.
- `WithAuthenticatedHTTPClient(client)` alone. Its caller-owned transport handles authentication; the SDK neither verifies authentication nor reads, sets, deletes, decodes, or logs the `Authorization` header. The transport is invoked for every retry and owns credential refresh and lifecycle.

`WithAPIKey`, `WithHTTPClient`, and `WithAuthenticatedHTTPClient` reject duplicate selections. An authenticated client cannot be combined with either API-key authentication or an ordinary client, in either option order. Constructor errors name the conflicting option and how to choose a valid combination.

```go
// API key plus ordinary transport customization.
client, err := typesafe.NewClient(
    typesafe.WithAPIKey(apiKey),
    typesafe.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
)
```

Pre-v1 migration: replace `typesafe.NewClient(key)` with `typesafe.NewClient(typesafe.WithAPIKey(key))`.

For OAuth, token acquisition and grant selection belong to the application. For example, an application that already depends on `golang.org/x/oauth2` can use a trusted gateway that owns OAuth:

```go
// golang.org/x/oauth2 is an application dependency, not an SDK dependency.
oauthClient := oauth2.NewClient(authLifecycleCtx, tokenSource)
client, err := typesafe.NewClient(
    typesafe.WithBaseURL("https://gateway.example.com/typesafe"),
    typesafe.WithAuthenticatedHTTPClient(oauthClient),
)
```

The explicit gateway URL is required: without `WithBaseURL`, the SDK targets the direct TypeSafe endpoint, which is for API-key authentication. The application owns grant flow, initial token acquisition, refresh behavior, lifecycle context, and token-endpoint TLS and HTTP timeouts. In particular, the inference request context passed to `SystemOne` or `ListModels` may not bound `oauth2.TokenSource.Token`: `oauth2.NewClient` can use the context captured when that client or token source was created. Bound token acquisition and refresh separately with the application-owned lifecycle context and HTTP client. The SDK cannot redact unknown transport-owned tokens from custom transport logs or errors. See the [gateway transport contract](docs/contract.md#authentication-and-gateway-transports).

See six small, synthetic, offline [`examples`](examples) for support triage, skill recommendation, RAG ranking, composite scoring, candidate extraction, and advisory security routing.

## Defaults and failure behavior

- Endpoint: `https://api.typesafe.ai`
- Model: `jev-latest`
- Per-attempt timeout: 10 seconds
- Total retry budget: 30 seconds
- Decompressed success/error response limit: 4 MiB
- Retries: two, for HTTP 408, 429, every 5xx status, configured connection failures, interrupted body reads, and configured attempt timeouts

The default policy makes up to three attempts. A POST may have been processed even when its response is lost, and the API documents no idempotency key: retries can duplicate inference, usage, or charges. Disable SDK retries while retaining all other defaults with:

```go
policy := typesafe.DefaultRetryPolicy()
policy.MaxRetries = 0
client, err := typesafe.NewClient(typesafe.WithAPIKey(apiKey), typesafe.WithRetryPolicy(policy))
```

A zero `RetryPolicy` is invalid; `WithRetryPolicy` uses the complete supplied value rather than merging zero fields with defaults. Disabling SDK retries does not guarantee at-most-once server processing or constrain custom transport behavior.

Caller cancellation and deadlines remain detectable with `errors.Is`. API errors expose safe status, retry delay, and sanitized request-ID metadata through `errors.As`; malformed successful responses return `ProtocolError`. Response bodies and API keys are never included in SDK error text. A custom `http.RoundTripper` remains responsible for honoring request contexts; the SDK closes a returned response body when the context ends so ordinary close-aware bodies unblock.

## API and model semantics

Choice's documented service/model limit is 255 options and Score's documented service/model limit is 10 criteria; larger calls may be rejected remotely. The wire schema currently has no Choice maximum and permits one Score criterion, so the client does not impose those service guidance limits. Use at least two Score criteria for meaningful scoring.

Jev is a fast RLCD parallel decision model—not chat, generation, tool execution, or a source of reasoning traces. Confidence is a model statistic, not the probability that an answer is correct. Adversarial state can steer output. Applications own thresholds, independent authorization, data classification, and every resulting action; SDK results must never be the sole authorization for a sensitive action.

See [the complete API contract](docs/contract.md), [threat model](docs/threat-model.md), and [release process](docs/releasing.md).

## Development and governance

Run `go test ./...`, `go test -race ./...`, and `go vet ./...`. See [CONTRIBUTING.md](CONTRIBUTING.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), and [SECURITY.md](SECURITY.md).

Licensed under [Apache 2.0](LICENSE).
