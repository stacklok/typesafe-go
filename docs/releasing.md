# Releasing

TypeSafe Go is pre-v1. Minor releases may change the public API; patch releases should remain compatible within a minor line. Do not claim v1 stability until the contract is explicitly reviewed.

1. Ensure the implementation plan and contract describe the behavior being released and update release notes/changelog.
2. Run formatting, tests, race tests, vet, both fuzz-smoke targets, `go list -deps`, and govulncheck on the supported Go version.
3. Confirm `go mod tidy` leaves no unexpected dependencies and examples run without unpublished modules, secrets, or network access.
4. Review the public API (`go doc`) and security-sensitive changes.
5. Tag a SemVer release (`v0.x.y`) only after CI is green. Verify the tag points to the reviewed commit and that `go list -m github.com/stacklok/typesafe-go@v0.x.y` resolves after publication.
6. Publish release notes describing compatibility and security implications.

Repository signing/provenance automation has not yet been selected. Do not imply signed provenance until Stacklok adopts and verifies a release workflow.
