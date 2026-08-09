# Library customization resources

The provider manages tags, UI filters, custom formats, library metadata
providers, and outbound proxies. Each resource supports import with its numeric
Chaptarr ID.

## Tags and associations

`chaptarr_tag` owns only the tag label. It never changes associations to
authors, download clients, indexers, import lists, notifications,
restrictions, or delay profiles. Use `chaptarr_tag_details` to observe those
associations without mutating them.

The `tags` argument on `chaptarr_metadata` is optional and computed. Omit it to
preserve associations managed outside Terraform; configure it explicitly to
replace the association set.

## Dynamic contracts

Chaptarr extensions have open-ended fields that vary by implementation.
`chaptarr_custom_format_schema` and `chaptarr_metadata_schema` expose the
server's current contracts as canonical JSON and a SHA-256 fingerprint.

Custom-filter `filters_json`, custom-format `specifications_json`, and metadata
`field_values_json` are canonicalized during refresh. Their computed SHA-256
attributes provide deterministic drift detection despite JSON formatting or
object-key order. Custom-format specifications must be a non-empty array and
each entry must name its implementation.

## Credentials and apply behavior

Proxy `password` and metadata `secret_fields` are Sensitive and WriteOnly.
Use ephemeral sensitive variables, as in the example, so values reach only an
apply request and are never retained in plan or state. Metadata password and
API-key fields are accepted only in `secret_fields`; non-secret fields belong
in `field_values_json`. The provider checks the current metadata schema before
apply and rejects fields placed in the wrong channel without echoing values in
diagnostics. The metadata-schema data source removes any protected values,
including values nested in presets.

Enabling a metadata provider can make Chaptarr validate it during create or
update. The provider never calls proxy or metadata test actions during plan or
refresh. Omitting a proxy password on update preserves the existing server-side
password.

## Imports

```shell
tofu import chaptarr_tag.example 3
tofu import chaptarr_proxy.example 4
tofu import chaptarr_custom_filter.example 5
tofu import chaptarr_custom_format.example 6
tofu import chaptarr_metadata.example 7
```

After importing metadata, configure any required protected fields through
`secret_fields` before an update. Their values cannot be recovered from state.
