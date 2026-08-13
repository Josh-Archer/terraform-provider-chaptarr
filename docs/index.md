# Chaptarr provider

The provider configures access to Chaptarr and manages supported declarative
application settings while offering read-only discovery and observability data
sources.

## Example

```hcl
provider "chaptarr" {
  url                     = "https://chaptarr.example.test/base-path"
  request_timeout_seconds = 30
}
```

Provide the credential through `CHAPTARR_API_KEY`. `CHAPTARR_URL` may supply
the URL when `url` is omitted. Explicit configuration takes precedence over
environment variables.

OpenTofu 1.11.2 or newer is required because root-folder Calibre passwords use
write-only provider schema and are never stored in Terraform state.

## Arguments

- `url` (optional): HTTP or HTTPS Chaptarr base URL. User information, query
  strings, fragments, and dot-segment paths are rejected. Reverse-proxy
  subpaths are preserved.
- `api_key` (optional, sensitive): Chaptarr API key. Prefer
  `CHAPTARR_API_KEY`; the value is sent only as `X-Api-Key`.
- `insecure_skip_verify` (optional): disables TLS certificate validation when
  true. Defaults to false and should be used only for a deliberately trusted
  private endpoint.
- `request_timeout_seconds` (optional): bounds the entire API operation,
  including safe-read retries. The schema documents and validates its range.

Provider configuration is offline. A network request occurs only when a
resource or data source performs an operation.

## Configuration resources

See [singleton configuration resources](resources/singleton-configuration.md)
for supported settings, write-only credential handling, import, conservative
destroy behavior, and naming-pattern data sources.

## Data sources

The [read-only data source guide](data-sources/read-only.md) documents API
capabilities, catalog searches, calendar observations, health/system checks,
filesystem lookups, and content fingerprints. Every data source refreshes when
Terraform reads it and none sends a mutating HTTP method.

The [storage resource guide](resources/storage.md) documents root folders,
remote-path mappings, imports, opt-in connection probes, and conservative
delete semantics.

The [profile resource guide](resources/profiles.md) documents typed quality,
metadata, release, and delay profiles; server schema data sources; ordered-list
normalization; and built-in quality-definition adoption.

The [library customization guide](resources/customization.md) documents tags,
custom filters and formats, metadata providers, outbound proxies, stable JSON
fingerprints, association preservation, and apply-only credentials.

The [external integrations guide](resources/integrations.md) documents
indexers, download clients, notifications, schema-driven settings, Prowlarr
compatibility, suppressed connection tests, and release-action boundaries.

The [import and Hardcover guide](resources/imports.md) documents import lists,
per-ID exclusions, explicit external validation, safe singleton observation,
and future multi-user GitOps routing.

The [library ownership guide](resources/library.md) documents authors, series
monitoring intent, provider-prefixed lookup identities, imports, and the
explicit controls that keep searches, file moves, and file deletion disabled.

The [book and edition guide](resources/books.md) documents apply-only lookup
candidates, monitored-edition selection, narrator state, conservative destroy
controls, and GET-only book-file/rename/retag inspection.

## Coverage status

The [generated coverage matrix](openapi-coverage.md) classifies every pinned
API operation. A `planned` row is not implemented functionality. Imperative or
destructive APIs remain action-only or out of scope unless a later issue adds
explicit safeguards.

The generated [compatibility matrix](compatibility.md) separates pinned
contract coverage, compatibility candidates, and live disposable acceptance
evidence. The [consumer readiness guide](consumer-readiness.md) covers provider
configuration, imports, sensitive settings, destructive controls, Readarr
migration boundaries, Registry ownership, and the deferred `terraform/arr-config`
integration gate.

The [PostgreSQL composition boundary guide](guides/azure-postgresql.md)
explains the scope and limits of `chaptarr_postgres_database`, including its
read-only Vaultwarden bridge lookup.

The [operational API policy](operational-api-policy.md) records why queue,
import, backup/restore, upgrade, process, log, and release-push operations are
not Terraform resources and recommends access-controlled operational tooling.
