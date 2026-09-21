# Releasing

TypeSafe Go is pre-v1. Minor releases may change the public API; patch releases should remain compatible within a minor line. Do not claim v1 stability until the contract is explicitly reviewed.

1. Ensure the implementation plan, contract, and draft release notes describe the behavior being released.
2. Run formatting, tests, race tests, vet, both fuzz-smoke targets, `go list -deps`, and govulncheck on the supported Go version.
3. Confirm `go mod tidy` leaves no unexpected dependencies and examples run without unpublished modules, secrets, or network access.
4. Review the public API (`go doc`) and security-sensitive changes. Record the exact reviewed, merged commit; do not release from an unreviewed working tree or merely from a branch name.
5. After CI is green, create the `v0.x.y` tag at that exact commit and verify both directions before pushing it: `test "$(git rev-list -n 1 v0.x.y)" = "<reviewed-merged-commit>"` and `git tag --points-at <reviewed-merged-commit>`. Tagging and publication are deliberate release actions, not part of release preparation.
6. After publication, verify through a newly created clean consumer module (outside this repository and with normal public proxy/sumdb settings):

   ```sh
   mkdir typesafe-go-consumer
   cd typesafe-go-consumer
   go mod init example.com/typesafe-go-consumer
   go get github.com/stacklok/typesafe-go@v0.x.y
   go list -m -json github.com/stacklok/typesafe-go
   go mod download -json github.com/stacklok/typesafe-go@v0.x.y
   go mod verify
   go test github.com/stacklok/typesafe-go/...
   ```

   Review `Version` and `Time` from `go list`, and `Sum` and `GoModSum` from `go mod download`; retain that JSON with the release record. Inspect `Dir` only after the module has been downloaded. If `Origin.Hash` is provided, record it as module-origin metadata, but do not treat it as the explicit tag-to-commit verification: that remains the repository check in step 5 against the reviewed merged commit. `go mod verify` checks the clean consumer's module-cache contents against recorded checksums. Proxy/checksum-database checksums authenticate module content within Go's transparency system; they do not prove who authored the source, who controlled the tag, or that Stacklok produced signed build provenance.
7. Publish the reviewed release notes describing compatibility and security implications. Confirm repository and module consumers resolve the same exact tag before announcing completion.

Repository signing/provenance automation has not yet been selected. Do not imply signed provenance until Stacklok adopts and verifies a release workflow.
