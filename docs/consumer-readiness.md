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
