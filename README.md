# Terraform/OpenTofu Provider for Chaptarr

`terraform-provider-chaptarr` is an early-stage Terraform Plugin Framework
provider for declaratively managing [Chaptarr](https://github.com/Chaptarr/chaptarr).

The bootstrap release contains the provider configuration, a hardened API
client, and an operation-level OpenAPI coverage guardrail. Resources and data
sources are intentionally not registered until their roadmap issues are
implemented and acceptance-tested.

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

## Development

Prerequisites are Go 1.25.12 and OpenTofu 1.8.0 or newer. Run:

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
