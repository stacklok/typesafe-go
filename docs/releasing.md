# Releasing

TypeSafe Go is pre-v1. Minor releases may change the public API; patch releases should remain compatible within a minor line.

## First-release prerequisite: protect release tags

Before the first public release, configure a repository ruleset for `v*` tags that prevents updates and deletion and restricts creation to authorized releasers. **This repository currently has no such protection, and this pull request does not configure repository settings. Do not publish until an administrator has enabled and verified the ruleset.**

This protection is part of the release mechanism, not an optional hardening step. GitHub cannot atomically combine the workflow's final tag check with creation of the GitHub release. The publisher rechecks the fully qualified tag immediately before creation as defense in depth, but without protected tags there remains a race in which the tag can move between that check and release creation. The recheck is not a complete guarantee.

## Release procedure

1. Ensure the implementation plan, contract, and draft release notes describe the behavior being released. Draft notes are planning material: they remain unreleased and the automation does not upload them.
2. Run formatting, tests, race tests, vet, both fuzz-smoke targets, `go list -deps`, and govulncheck on Go 1.26.6. The tag workflow repeats these checks with live tests explicitly disabled.
3. Confirm `go mod tidy` leaves no unexpected dependencies and examples run without unpublished modules, secrets, or network access.
4. Review the public API (`go doc`) and security-sensitive changes. Record the exact reviewed, merged commit; do not release from an unreviewed working tree or merely from a branch name. All review and CI must finish before tagging because pushing a public tag can make a Go module version available independently of the GitHub release workflow.
5. After CI is green, create a strict stable `v0.x.y` tag at that exact commit and verify both directions before pushing it: `test "$(git rev-list -n 1 v0.x.y)" = "<reviewed-merged-commit>"` and `git tag --points-at <reviewed-merged-commit>`. Use v0 while the project remains pre-v1. The workflow syntactically supports v1, but create a v1 tag only after a deliberate stability and public-contract review; v1 is not selected automatically. The unversioned module path rejects v2 and later, which require a `/vN` module path. Push with a user or token whose tag push triggers Actions; tag pushes made with a workflow's default `GITHUB_TOKEN` do not start another workflow.
6. A newly created `v*` tag starts `.github/workflows/release.yml`; deleted or force-updated tags cannot pass its job guard. The read-only verifier checks the strict version, module major, exact event commit, and main-branch ancestry, then runs the release checks. Only after verification does an isolated write-permission job resolve the fully qualified tag again and create a GitHub release with generated notes. It does not check out or execute repository code. Do not create a GitHub release manually while this run is pending.
7. After publication, verify through a newly created clean consumer module (outside this repository and with normal public proxy/sumdb settings). Proxy indexing is eventually consistent, so this remains a manual post-publication check and may need to be retried:

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
8. Review the generated GitHub release notes for compatibility and security implications, then confirm repository and module consumers resolve the same exact tag before announcing completion. If hand-written notes are required, update the generated release deliberately after publication; draft note files are not uploaded automatically.

## Failure handling

A transient workflow failure may be rerun for the same unchanged tag. For a validation failure or source defect, fix the source in a new reviewed commit and choose a new version. Never move, reuse, or delete a public tag. If a GitHub release already exists, inspect it and the tag rather than trying to overwrite it; the workflow deliberately fails instead of replacing an existing release.

The workflow creates GitHub releases with generated notes for already pushed source tags; it never creates tags. It creates no assets or provenance attestations; do not imply signed build provenance.
