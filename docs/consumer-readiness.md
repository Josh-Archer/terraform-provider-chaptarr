# Consumer and release readiness

## Provider configuration

Configure `url` directly or through `CHAPTARR_URL`. Supply the API credential
through `CHAPTARR_API_KEY`; the optional `api_key` provider argument is
sensitive, but environment-only configuration avoids placing it in source.
Keep TLS verification enabled outside a deliberately trusted private sandbox.
Provider configuration is offline; network access begins with a resource or
data-source operation.

## Imports

Import resources with the local numeric Chaptarr identifier documented by the
resource guide, for example:

```shell
tofu import chaptarr_tag.example 42
```

Imported objects cannot recover apply-only credentials. Configure required
write-only values before the next update. Series imports also cannot recover
the original selected-book intent and must be completed before update.

The [migration guide](guides/migration.md) can generate configuration and
import blocks from a live instance. It does not manage every resource.
PostgreSQL composition and a populated Hardcover token stay manual, and
settings the importer cannot reconstruct are named instead of emitted.

## Sensitive settings

Provider API keys, OIDC secrets, Calibre passwords, proxy passwords, metadata
API keys, integration tokens, and Hardcover tokens must come from sensitive,
ephemeral variables or process environment variables. Write-only resource
attributes are deliberately null in state after apply. Do not commit state,
plans, Chaptarr configuration, raw logs, API response bodies, or credentials.

## Destructive and imperative controls

Searches, file moves, media deletion, connection tests, external validation,
and import-list exclusions default to disabled or require explicit opt-in.
Queue, backup/restore, upgrade, process, log, and release-push operations are
not Terraform resources. Review each resource guide before enabling an
apply-only or destructive control, and use approval-controlled operational
tooling for imperative maintenance.

## Readarr migration example

Treat migration as configuration translation, not state-file conversion:

1. Inventory Readarr root folders, profiles, tags, indexers, download clients,
   notifications, import lists, and monitored authors without exporting
   credentials or production media.
2. Use Chaptarr schema and lookup data sources to select current identifiers.
3. Write Chaptarr resources with searches, moves, deletes, tests, and external
   validation disabled.
4. Import existing Chaptarr objects by local numeric ID where supported.
5. Review a saved plan in a disposable or dedicated non-production Chaptarr
   instance before any production apply.

Readarr metadata identities and Chaptarr identities are not interchangeable.
This workflow is an example only and has not been live-validated as a complete
Readarr migration.

## `terraform/arr-config` integration gate

An example integration is intentionally deferred. Propose it only after the
immutable-image acceptance matrix passes on the exact provider head and a
dedicated sandbox validates the required resources without production media or
credentials. The later proposal must pin a provider release, start with
destructive controls disabled, document rollback, and carry its own plan and
apply evidence.

## Registry and release boundary

CI can validate unit, contract, lint, acceptance, vulnerability, secret, and
unsigned release-snapshot tiers. Registry namespace authorization, public
signing-key registration, protected-environment private keys/passphrases,
signing, and any publisher terms or attestations remain owner actions. Tests
and contributors must never read or print those secrets.

Those CI tiers are what a signed 0.x release uses. Provider 1.0 adds the
upstream tag, coverage threshold, and release gates in the following sections.

<a id="provider-1-0-stability-criteria"></a>

## Provider 1.0 stability criteria

The release manifest is below 1.0.0. Releases stay on 0.x until the
[provider 1.0 checklist](#provider-1-0-checklist) is complete. This guide
records that bar. It does not tag or publish `v1.0.0`.

On 0.x, release-please maps a breaking change to a minor bump and a feature
to a patch bump (`bump-minor-pre-major` and `bump-patch-for-minor-pre-major`
in `.github/release-please-config.json`). Compatibility evidence for a 0.x
release is the generated [compatibility matrix](compatibility.md). The pinned
contract is Chaptarr `v0.9.965`. Acceptance images are `0.9.929`, `0.9.925`,
and `0.9.914`, each recorded as a `compatibility-candidate`.

### Upstream API stable tag

An upstream API stable tag is a Chaptarr GitHub Release that meets every rule
below.

1. The tag matches `v<major>.<minor>.<patch>` with a major of at least 1 and
   with no prerelease or build suffix.
2. The GitHub Release is published. Drafts and prereleases are outside this
   definition.
3. The tag is an immutable upstream release. Floating Docker or Git tags,
   including `latest`, `develop`, and `nightly`, are outside this definition.
   Upstream may promote a 0.x build to the Docker `latest` channel after it
   has run on `develop`. That channel is still a 0.x build.
4. `compatibility/chaptarr.json` sets `contract.tag` to that tag, or to a
   later tag with the same major version that also meets rules 1–3.
   `contract.commit` and `contract.openapi_sha256` match
   [`third_party/chaptarr/NOTICE.md`](../third_party/chaptarr/NOTICE.md).
5. `go run ./tools/openapi drift <tag>` reports no drift, and no open issue
   carrying the `<!-- openapi-drift -->` marker remains for that tag.
6. The compatibility matrix has a row for that Chaptarr version with
   `contract_status` `pinned-contract`, `acceptance_status` `live-verified`,
   and an image of the form
   `chaptarr/chaptarr:<version>@sha256:<64 hex digits>`. CI acceptance runs
   that image.

A published 0.x GitHub Release stays outside this definition, including when
the release omits the prerelease flag. The current pin, `v0.9.965`, is a
pre-1.0 upstream tag. No compatibility-matrix row uses `pinned-contract`.

### Coverage threshold

Measure coverage from the summary table in the generated
[OpenAPI coverage matrix](openapi-coverage.md) after
`go run ./tools/openapi check` passes. If the snapshot below and that table
disagree, the generated table wins. Refresh the snapshot in the same change
that updates the pin or the classifications.

- Data sources: every operation classified `data-source` has status
  `implemented`. The planned count for that classification is zero, and the
  implemented count is greater than zero.
- Resources: let `R` be the number of operations classified `resource` with
  status `implemented` or `planned`, and let `Ri` be the implemented count.
  The threshold holds when `Ri * 10 >= R * 9` (at least 90 percent).
- Operations classified `action-only` or `out-of-scope` stay outside both
  ratios. They follow the [operational API policy](operational-api-policy.md).

The 90 percent resource bar leaves room for a reviewed minority of collection
list, bulk edit, and editor operations to stay `planned` after per-id
resources exist. Today's ratio is below that bar.

Moving a `planned` operation to `action-only` or `out-of-scope` is an OpenAPI
classification change. [CONTRIBUTING.md](../CONTRIBUTING.md) requires an
upstream version pin, provenance update, review of every changed
classification, regenerated inventory and docs, and a passing
`go run ./tools/openapi check` before that change can move the threshold.
The proposing pull request lists every resource operation that remains
`planned`, with a one-line reason for each.

On the `v0.9.965` matrix this section was written against, the generated
summary is 80 implemented data-source operations, 0 planned data-source
operations, 117 implemented resource operations, and 42 planned resource
operations (316 operations in total). `117 * 10 >= 159 * 9` is false
(`1170 < 1431`). The coverage gate is open. The manifest version at that
commit is `0.11.21`.

### Release gates

`v1.0.0` is built from a commit on which every gate below is green.

1. Unit: `go test -race ./...` with the Go toolchain pinned in CI.
2. Contract: `go mod verify`, `go run ./tools/openapi check`,
   `go run ./tools/compatibility check`, the acceptance compose config,
   `scripts/test-plan-leak.sh`, `scripts/test-release-assets.sh`, and
   `scripts/check-doc-links.sh`.
3. Lint: `gofmt`, `go vet`, `tofu fmt -check`, golangci-lint, and
   `git diff --check`.
4. Acceptance: CI is green for every image in `compatibility/chaptarr.json`,
   including the pinned stable tag from the upstream rule above.
5. Secret scan and `govulncheck` are green.
6. The unsigned release-snapshot job is green. That job checks that release
   archives build. Publication of `v1.0.0` is a separate signed run.
7. Publication uses `.github/workflows/release.yml` from the default branch
   for the exact tag `v1.0.0`, with the protected `release` environment, GPG
   signing enabled, and `scripts/validate-release-assets.sh` invoked without
   `--allow-unsigned`. An existing published release stays as it is.
8. The OpenTofu registry indexes `josh-archer/chaptarr` at `1.0.0`. A
   Terraform Registry claim needs the same clean install evidence before the
   release notes say that registry serves the version.
9. Release-confidence workflows block promotion until a consumer
   configuration replay applies, the following plan is empty, and an upgrade
   from the previous provider release succeeds; the lifecycle suite passes
   on Terraform and on OpenTofu; a clean registry install resolves the
   release. Those workflows are tracked by
   [#104](https://github.com/Josh-Archer/terraform-provider-chaptarr/issues/104)
   and are absent from this repository, so this gate is open.

<a id="provider-1-0-checklist"></a>

## Provider 1.0 checklist

The maintainer who proposes `v1.0.0` completes this list in that pull request
and links evidence for each item. Release Please, and
`scripts/calculate-next-version.sh`, can print a version. Printing a version
leaves this list incomplete. `.release-please-manifest.json` stays on 0.x
until that pull request updates it. `bump-minor-pre-major` stays enabled until
then.

- [ ] The upstream API stable tag rules above hold, including a
  `pinned-contract` and `live-verified` matrix row for that tag.
- [ ] The coverage threshold holds on the generated OpenAPI summary for that
  pin, and every remaining `planned` resource operation is listed with a
  reason.
- [ ] Release gates 1–9 are green on the commit that will be tagged
  `v1.0.0`, including the release-confidence workflows from gate 9.
- [ ] The proposing pull request quotes this checklist and points at the
  workflow runs, drift result, and registry index entry.

Dispatching `.github/workflows/release.yml` for `v1.0.0` waits on this
checklist. The release steps are in [CONTRIBUTING.md](../CONTRIBUTING.md#releases).
