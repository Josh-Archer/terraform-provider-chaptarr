# Chaptarr provider

The provider configures access to a Chaptarr API. This bootstrap does not yet
register resources or data sources.

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

Provider configuration is offline. A network request occurs only when a future
resource or data source performs an operation.

## Coverage status

The [generated coverage matrix](openapi-coverage.md) classifies every pinned
API operation. A `planned` row is not implemented functionality. Imperative or
destructive APIs remain action-only or out of scope unless a later issue adds
explicit safeguards.
