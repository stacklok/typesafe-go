# Contract test data

`openapi.json` is the exact public document returned by [`GET https://api.typesafe.ai/openapi.json`](https://api.typesafe.ai/openapi.json) on 2026-09-21. It declares OpenAPI 3.1.0 and API version 0.2.0. SHA-256 of the exact stored 14,158 bytes:

```text
a191f8a7df6bd6fedced8120dd0fd106f88575d1d1c8360d08900a6c7c0360d5
```

Relevant source pointers:

- [`ChoiceAnswer`](https://api.typesafe.ai/openapi.json#/components/schemas/ChoiceAnswer): required selected choice, confidence, and probabilities; the selected choice is described as the highest-probability criterion.
- [`ChoiceQuestion`](https://api.typesafe.ai/openapi.json#/components/schemas/ChoiceQuestion): choice labels are unconstrained object keys; empty strings are not excluded.
- [`ScoreAnswer`](https://api.typesafe.ai/openapi.json#/components/schemas/ScoreAnswer): fractional expected score, legend, confidence, and probabilities.
- [`Usage`](https://api.typesafe.ai/openapi.json#/components/schemas/Usage): integer token counts, with no schema `minimum`; the SDK's nonnegative requirement is semantic validation.
- [`SystemOneRequest`](https://api.typesafe.ai/openapi.json#/components/schemas/SystemOneRequest) and [`SystemOneResponse`](https://api.typesafe.ai/openapi.json#/components/schemas/SystemOneResponse): endpoint envelopes.

Fixture mapping:

- `systemone-request.json` exercises all three question schemas and structured/null content.
- `systemone-response.json` exercises all three answer schemas. Its Choice selection is an argmax, and its Score equals the documented probability-weighted level value.
- `models-response.json` exercises `ModelMetadataList` with synthetic metadata.

These are synthetic, hand-reviewed conformance fixtures; they are not captured service responses. Deliberately permissive decoder values used to prove that the SDK does not normalize sums, recompute Score, or compare legend content appear in tests such as `client_test.go` and are explicitly not represented as live-conformant examples.

## Updating the snapshot

When the public API changes, re-fetch only the public OpenAPI document and compare required fields, types, descriptions, and limits with this snapshot. Deliberately update the snapshot hash, then update the affected fixtures and tests together. Offline CI must validate only the checked-in snapshot and fixtures; it must not infer undocumented behavior or fetch live API documentation.
