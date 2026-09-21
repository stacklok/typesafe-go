// Package typesafe provides a synchronous client for TypeSafe's System One API.
//
// NewClient requires exactly one authentication strategy: WithAPIKey, optionally
// combined with WithHTTPClient for transport customization, or
// WithAuthenticatedHTTPClient alone. WithHTTPClient alone does not authenticate.
// WithAPIKey enables SDK-managed bearer authentication for the direct TypeSafe
// API. WithAuthenticatedHTTPClient is for a caller-owned transport, such as one
// targeting a trusted gateway that owns OAuth; in that mode the SDK leaves
// Authorization untouched. Token acquisition and refresh must be bounded by the
// application's lifecycle context and HTTP client, because an inference context
// does not necessarily bound a transport's TokenSource.
//
// Clients are safe for concurrent calls when a supplied HTTP transport is safe.
// Request values remain caller-owned and must not be mutated concurrently with a
// call. Construction performs no network access or implicit environment lookup.
package typesafe
