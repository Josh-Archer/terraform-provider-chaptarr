# External-service integrations

The provider manages Chaptarr indexers, download clients, and notification
providers. Each resource supports create, read, update, delete, and import by
its numeric Chaptarr ID.

Use the corresponding schema data source before choosing an implementation,
config contract, and dynamic field names:

- `chaptarr_indexer_schema`
- `chaptarr_download_client_schema`
- `chaptarr_notification_schema`

Non-secret settings belong in `field_values_json`. Password, token, cookie, and
API-key fields belong only in the Sensitive and WriteOnly `secret_fields` map.
The provider validates that separation against the current server schema during
apply, removes protected values from schema data sources and refresh state, and
stores only canonical non-secret JSON plus its SHA-256 fingerprint.

## Safety and testing

Chaptarr automatically runs a provider connection/test call when an enabled
integration is created or updated. To prevent surprise external traffic,
`enable = true` is rejected unless `test_on_apply = true` is also configured.
Leave the integration disabled to apply settings without a connection test.
Plan and refresh never test providers. The provider never calls provider
`test`, `testall`, `action`, release-download, or release-push endpoints.

Treat `test_on_apply = true` as explicit authorization for Chaptarr to contact
the configured service during that apply. Alternatively, run tests separately
in the Chaptarr UI or access-controlled operational tooling. Release search remains unimplemented because
the GET endpoint can launch indexer searches; release download and release push
remain explicit action-only operations.

## Tags and routing

Indexer and notification `tags` are optional and computed. Omit them to
preserve associations managed outside Terraform, or configure them to replace
the set. Download clients expose separate optional/computed `audiobook_tags`
and `ebook_tags`; `tags` is their computed union.

`chaptarr_indexer_flags` reads the server-owned flag catalog and never mutates
it.

## Prowlarr compatibility

This provider configures Chaptarr's native indexer definitions. Chaptarr exposes
a Prowlarr-compatible definition name for compatible clients, but this provider
does not install, configure, or synchronize a separate Prowlarr instance. If a
separate Prowlarr deployment owns indexer synchronization, keep that integration
in its own access-controlled workflow and avoid managing the same Chaptarr
indexer with both systems.

## Imports

```shell
tofu import chaptarr_indexer.example 1
tofu import chaptarr_download_client.example 2
tofu import chaptarr_notification.example 3
```

Protected values cannot be recovered from state. Supply required values through
`secret_fields` before the next update of an imported provider.
