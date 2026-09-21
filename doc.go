// Package typesafe provides a synchronous client for TypeSafe's System One API.
//
// Clients are safe for concurrent calls when a supplied HTTP transport is safe.
// Request values remain caller-owned and must not be mutated concurrently with a
// call. Construction performs no network access or implicit environment lookup.
package typesafe
