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

## `terraform/arr-config` replay gate

`test/consumer/arr-config` is a sanitized copy of the Chaptarr resources in
the consumer `terraform/arr-config` module. The CI job `Consumer Replay
(arr-config)` applies it to a disposable loopback Chaptarr, requires a clean
re-plan, then repeats that from the previous published provider. The candidate
build is installed only from a filesystem mirror. Ambient `CHAPTARR_URL` and
`CHAPTARR_API_KEY` values are discarded.

Release publication waits on that gate. A stable tag is promoted when the
tagged commit's Consumer Replay check succeeded, or when the release workflow
replays the tagged source and that replay succeeds. See
[release gates](release-gates.md). This is disposable-instance evidence, not a
production apply.

## Registry and release boundary

CI can validate unit, contract, lint, acceptance (OpenTofu and Terraform),
consumer replay, vulnerability, secret, and unsigned release-snapshot tiers.
Registry install checks for both public registries run on a schedule and after
publication. Registry namespace authorization, public
signing-key registration, protected-environment private keys/passphrases,
signing, and any publisher terms or attestations remain owner actions. Tests
and contributors must never read or print those secrets.
