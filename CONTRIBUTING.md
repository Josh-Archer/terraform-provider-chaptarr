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

Release signing keys and passphrases are owner-managed secrets in the protected
GitHub `release` environment. Contributors and tests must never read or print
them.
