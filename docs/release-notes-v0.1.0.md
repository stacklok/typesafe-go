# v0.1.0 release notes (draft, unreleased)

> **Unreleased:** this document prepares a future v0.1.0 release. No tag, GitHub release, or module publication has occurred.

TypeSafe Go v0.1.0 is the initial pre-v1 release of the community Go SDK for TypeSafe's System One API.

## Highlights

- Typed Noul, Choice, and Score requests and validated responses for `POST /v1/systemone`, plus model discovery through `GET /v1/models`.
- Explicit SDK-managed API-key authentication or caller-owned authenticated transports, with redirect protection and no implicit environment lookup.
- Bounded response handling, safe typed errors, cancellation, configurable retry behavior, and no runtime dependencies outside the Go standard library.
- Choice responses enforce the documented argmax relationship: the selected key must be present and no returned probability may be strictly greater. Ties and empty labels remain valid; comparison uses exact `>` with no epsilon. This is a behavioral tightening: non-argmax Choice answers, including extra unrequested answers, now fail as `ProtocolError` rather than being returned.
- Previous PR additions validated `ProtocolError.Usage`, rejected negative token counts, preserved known HTTP status metadata through body-decoding failures, and documented the pre-v1 caveat that external unkeyed `ProtocolError` literals may need migration to keyed literals.
- Offline conformance fixtures trace to the checked-in public OpenAPI 0.2.0 snapshot.

## Compatibility and validation

This is a pre-v1 release. Minor releases may change the public API. The decoder retains finite/range, requested type/membership/key, Score bounds, contiguous Score-key, and probability/legend key-equality checks. It deliberately does not normalize or require probability sums, recompute or compare Score, compare legend content, or infer relationships across questions. Requested aliases and service-resolved model names are not required to match.

Token counts are required to be nonnegative as SDK semantic validation; the reviewed schema declares integers but does not specify a minimum.

## Operational and security notes

Retries can duplicate inference, usage, or charges because the API documents no idempotency key. Disable SDK retries when that risk is unacceptable, while recognizing that transport failures still cannot guarantee at-most-once processing. Request IDs are constrained but untrusted metadata. The SDK does not log payloads or credentials, but caller transports and surrounding infrastructure remain separate trust boundaries.

See [`docs/contract.md`](contract.md), [`docs/threat-model.md`](threat-model.md), and [`docs/releasing.md`](releasing.md) before release. Release checks must bind the tag to the exact reviewed merged commit and separately record Go module checksums; checksum transparency is not authorship or signed-build provenance.
