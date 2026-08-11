# Terraform/OpenTofu Provider for Chaptarr

`terraform-provider-chaptarr` is an early-stage Terraform Plugin Framework
provider for declaratively managing [Chaptarr](https://github.com/Chaptarr/chaptarr).

The provider includes hardened client behavior, operation-level OpenAPI
coverage, singleton application configuration resources, read-only naming
helpers, and read-only discovery and observability data sources. Resource
credentials use OpenTofu write-only attributes so they are not retained in
state, and changing runtime observations are data sources rather than resources.

## Provider configuration

```hcl
terraform {
  required_providers {
    chaptarr = {
      source = "josh-archer/chaptarr"
    }
  }
}

provider "chaptarr" {
  url = "https://chaptarr.example.test"
}
```

Set the API key through the `CHAPTARR_API_KEY` environment variable. The
provider also accepts a sensitive `api_key` attribute, but environment-only
configuration avoids writing credentials into configuration files. The base
URL can be provided through `CHAPTARR_URL` instead of `url`.

The API key is sent only in the `X-Api-Key` request header. Provider
configuration performs no network request.

See [provider documentation](docs/index.md) and the generated
[OpenAPI coverage matrix](docs/openapi-coverage.md).
Imperative queue, import, backup, upgrade, and process operations follow the
[operational API policy](docs/operational-api-policy.md).

Singleton resource behavior, imports, credential handling, and naming helpers
are documented in
[configuration resources](docs/resources/singleton-configuration.md). A
representative audiobook/ebook configuration is available under
[`examples/configuration`](examples/configuration).

## Read-only discovery

The provider exposes API/version capability, language, search, calendar,
health, system, task, update, disk, filesystem, and media-cover observations.
Health and system summaries deliberately omit raw messages, logs, installation
identifiers, and internal application paths. Binary cover and calendar-feed
responses are represented by their content type, byte length, and SHA-256
fingerprint instead of being stored in Terraform state.

See the [read-only data source guide](docs/data-sources/read-only.md).
Storage lifecycle and conservative delete behavior are documented in the
[storage resource guide](docs/resources/storage.md).
Typed quality, metadata, release, delay, and quality-definition management is
documented in the [profile resource guide](docs/resources/profiles.md).
Tags, custom filters and formats, metadata providers, and outbound proxies are
documented in the [library customization guide](docs/resources/customization.md).
Indexer, download-client, and notification management is documented in the
[external integrations guide](docs/resources/integrations.md).
Import lists, exclusions, and the guarded Hardcover singleton are documented in
the [import and Hardcover guide](docs/resources/imports.md).
Author and series collection intent, lookup-assisted identity selection, and
safe media-operation controls are documented in the
[library ownership guide](docs/resources/library.md).
Book and monitored-edition lifecycle plus GET-only book-file, rename, and retag
inspection are documented in the [book and edition guide](docs/resources/books.md).

## Development

Prerequisites are Go 1.25.12 and OpenTofu 1.11.2 or newer. OpenTofu 1.11 is
required for write-only configuration and Calibre credential attributes. Run:

```shell
go mod download
bash ./scripts/test-all-locally.sh
```

Regenerate or verify the pinned API inventory with:

```shell
go run ./tools/openapi generate
go run ./tools/openapi check
```

No live Chaptarr acceptance environment is used in the bootstrap. That work is
tracked in issue #12.

## Security and releases

Pull requests run only on GitHub-hosted runners with read-only permissions.
Actions are pinned to full commit SHAs. Releases are manually dispatched for an
existing SemVer tag, require the protected `release` environment, and refuse to
publish without detached GPG signing.

An existing published release is never overwritten. The workflow requires the
checksum signature asset to exist, but cryptographic re-verification of an
already-published signature remains an owner/registry check until the public
signing key and fingerprint are registered and safely available to automation.

See [SECURITY.md](SECURITY.md) for vulnerability reporting and
[CONTRIBUTING.md](CONTRIBUTING.md) for validation requirements.

Original provider code is licensed under MPL-2.0. The pinned upstream OpenAPI
artifact retains Chaptarr's GPL-3.0 license; see
[`third_party/chaptarr/NOTICE.md`](third_party/chaptarr/NOTICE.md).
