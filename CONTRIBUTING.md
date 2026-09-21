# Contributing to TypeSafe Go

TypeSafe Go is Apache-2.0 licensed. Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md); report conduct concerns to [code-of-conduct@stacklok.com](mailto:code-of-conduct@stacklok.com). Report vulnerabilities through the private process in [SECURITY.md](SECURITY.md), never a public issue.

## Issues and development

Use [GitHub Issues](https://github.com/stacklok/typesafe-go/issues) for reproducible bugs and discussed enhancements. Issues labeled `good first issue` are intended as self-contained introductions. Comment before claiming existing work so contributors can coordinate.

Use Go 1.26 or later:

```sh
go test ./...
go test -race ./...
go vet ./...
```

Add focused tests with changes. Keep the SDK's runtime dependency-free and do not make live or paid API calls in ordinary tests.

## Pull requests

1. Discuss substantial changes in an issue.
2. Keep changes focused and update tests and documentation.
3. Format with `gofmt` and run the development checks.
4. Use a clear title and explain what and why.
5. Ensure CI passes and address review.

Every commit must certify the [Developer Certificate of Origin](dco.md) with a `Signed-off-by:` trailer; `git commit -s` adds it.

## Commit messages

Follow [Chris Beams' guidance](https://chris.beams.io/posts/git-commit/):

1. Separate subject from body with a blank line.
2. Limit the subject line to 50 characters.
3. Capitalize the subject line.
4. Do not end the subject with a period.
5. Use imperative mood in the subject.
6. Explain what and why, rather than how, in the body.
