# Import lists, exclusions, and Hardcover

`chaptarr_import_list` manages one provider-backed import list with typed
monitoring, search, root-folder, profile, and media controls. Provider-specific
nested settings use canonical `field_values_json` and an apply-only
`secret_fields` map. Read the sanitized `chaptarr_import_list_schema` data
source to select an implementation, config contract, and field names.

Like other provider-backed integrations, Chaptarr automatically runs a
connection test when an enabled list is created or updated. `enable = true`
therefore requires explicit `test_on_apply = true`. Disabled lists can be
configured without that external action. Plan and refresh never test a list.

## Exclusions

`chaptarr_import_list_exclusion` owns exactly one exclusion by numeric ID and
supports `all`, `audiobook`, or `ebook` media scope. The resource never calls
bulk endpoints or enumerates and deletes unmatched exclusions. Exclusions and
imports created outside Terraform remain untouched unless their exact resource
ID is explicitly imported and managed.

## Hardcover singleton

`chaptarr_hardcover_config` owns Chaptarr's single global Hardcover token.
`token` is Sensitive and WriteOnly and should come from an ephemeral variable.
Chaptarr validates every submitted token against Hardcover before saving it,
so create or rotation requires `allow_external_validation = true`.

The upstream GET endpoint may also contact Hardcover to backfill username and
avatar data. Ordinary refresh therefore stays local-state-only. Set
`observe_server = true` only when authorizing that possible external request
and accepting the resulting profile metadata in state. The token is never
returned or stored; state contains only `has_token`, `enabled`, username, and
avatar observations. With observation disabled, Terraform deliberately cannot
detect out-of-band token removal.

Destroy only relinquishes Terraform ownership. Set `enabled = false` to clear
the singleton token and profile fields in Chaptarr. Import it with the literal
ID `hardcover`; import defaults both external-network controls to false.

## Multi-user routing

This provider does not fan out one Hardcover account into per-user imports or
silently create lists for household members. A later GitOps consumer may read
an explicitly approved user-to-list mapping and instantiate separate import
list resources. Keep that mapping in version control, give each user an
explicit target/root/profile, and avoid sharing or moving credentials between
users.

## Imports

```shell
tofu import chaptarr_import_list.example 4
tofu import chaptarr_import_list_exclusion.example 7
tofu import chaptarr_hardcover_config.current hardcover
```
