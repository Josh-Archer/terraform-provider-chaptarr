# Contributing

Create a focused branch and pull request for each tracked issue. Pull request
descriptions must link the issue with `Closes #<number>` when the change fully
resolves it.

Before requesting review, run:

```shell
go mod tidy
bash ./scripts/test-all-locally.sh
go test -race ./...
```

Changes to resources, data sources, compatibility claims, or the acceptance
harness must also run `go run ./tools/compatibility check` and the relevant
immutable-image lane in `scripts/test-acceptance.sh`. The harness must use only
its generated API key and disposable empty volumes; never point it at a local
or production Chaptarr configuration or media path.
On Windows, run the shell gates from Git Bash so `bash`, Docker, Go, and
OpenTofu share the same Windows environment.

Do not commit credentials, Terraform state, plan files, raw Chaptarr logs, or
provider response bodies. Tests must use unmistakably synthetic values and
must assert that those values do not appear in diagnostics or plan output.

OpenAPI changes require an upstream version pin, provenance update, review of
every changed operation classification, regenerated inventory and docs, and a
passing `go run ./tools/openapi check`.

## Releases

0.x is the release line. The manifest in `.release-please-manifest.json` is
the version release-please will tag. `bump-minor-pre-major` and
`bump-patch-for-minor-pre-major` keep breaking changes and features on 0.x.

`v1.0.0` waits on the checklist in
[docs/consumer-readiness.md](docs/consumer-readiness.md#provider-1-0-checklist).
Complete that checklist in the pull request that proposes the tag. Link
evidence for the upstream API stable tag, the coverage threshold, and the
release gates. Dispatch `.github/workflows/release.yml` for that exact tag
only after the checklist is complete. Leave `bump-minor-pre-major` enabled
until that pull request updates the manifest.

`scripts/calculate-next-version.sh` can print a major tag when it sees a
breaking-change trailer. A printed tag leaves the checklist incomplete.

Release signing keys and passphrases are owner-managed secrets in the protected
GitHub `release` environment. Contributors and tests must never read or print
them. The `v1.0.0` publication run signs checksums. It uses the signed
GoReleaser step in the Release workflow.
