# Singleton configuration resources

Chaptarr exposes one instance of each application configuration object. The
provider models those objects as singleton resources:

| Resource | API family |
| --- | --- |
| `chaptarr_host_config` | Host, authentication, proxy, update, backup, and OIDC settings |
| `chaptarr_ui_config` | UI, date, theme, and default-media settings |
| `chaptarr_media_management_config` | Audiobook and ebook import/filesystem behavior |
| `chaptarr_naming_config` | Audiobook and ebook naming patterns |
| `chaptarr_conversion_config` | Audiobook and ebook conversion settings |
| `chaptarr_metadata_provider_config` | Tag, cover, and embedded-metadata behavior |
| `chaptarr_download_client_config` | Global completed-download handling |
| `chaptarr_indexer_config` | Global indexer limits and RSS synchronization |
| `chaptarr_development_config` | Advanced diagnostics and metadata-server settings |
| `chaptarr_hardcover_config` | Hardcover connection state |

All ordinary attributes are optional and computed. On create or update, the
provider first reads the current singleton, overlays only explicitly configured
values, and sends the complete result. This preserves server defaults and
settings managed by a newer Chaptarr version.

Destroy is intentionally state-only. It relinquishes Terraform ownership but
does not reset application settings, disable authentication, remove proxy
configuration, or disconnect Hardcover. Set `enabled = false` explicitly on
`chaptarr_hardcover_config` to disconnect Hardcover.

## Credentials

The following `chaptarr_host_config` arguments are sensitive and write-only:

- `password`
- `password_confirmation`
- `ssl_cert_password`
- `proxy_password`
- `oidc_client_secret`

`chaptarr_hardcover_config.token` is also sensitive and write-only. Chaptarr
responses are never copied back into these arguments, and server-returned
credential fields are removed before update payloads are created.

Write-only arguments require OpenTofu 1.11.2 or newer. Supply them through an
ephemeral sensitive variable so the value is absent from saved plans and state:

```hcl
variable "oidc_client_secret" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "chaptarr_host_config" "this" {
  authentication_method = "oidc"
  oidc_client_secret     = var.oidc_client_secret
}
```

The API returns its own authentication key in the host configuration document,
but Chaptarr v0.9.925 explicitly refuses to update that field. The provider
therefore neither exposes nor retransmits it. API-key rotation remains outside
this resource until Chaptarr provides a functional, safely reauthenticated
rotation endpoint.

## Import

Import accepts the singleton name or its numeric API identifier. The next read
normalizes state to Chaptarr's numeric identifier:

```shell
tofu import chaptarr_host_config.this host
tofu import chaptarr_conversion_config.this conversion
tofu import chaptarr_hardcover_config.this hardcover
```

## Naming helpers

Naming compilation, decompilation, validation, preview, and examples are
read-only data sources. They may use HTTP POST because the Chaptarr API models
evaluation that way, but they never update naming configuration.

```hcl
data "chaptarr_naming_pattern" "validate" {
  operation = "validate"
  pattern   = "{Author Name}/{Book Title}"
}

data "chaptarr_naming_examples" "current" {}
```

`result_json` contains Chaptarr's bounded response document. Complex AST and
sample inputs can be supplied with `ast_json` and `sample_json`.
