# chaptarr_indexer (Resource)

Manage a Chaptarr indexer registration (e.g. Torznab, Newznab, Prowlarr).

## Example Usage

```hcl
resource "chaptarr_indexer" "prowlarr" {
  name            = "Prowlarr Torznab"
  enable          = true
  protocol        = "torrent"
  priority        = 25
  implementation  = "Torznab"
  config_contract = "TorznabSettings"

  field {
    name  = "baseUrl"
    value = "http://prowlarr-service.media.svc.cluster.local:9696/1"
  }
  field {
    name            = "apiKey"
    sensitive_value = var.prowlarr_api_key
  }
}
```

## Argument Reference

- `name` - (Required, String) Indexer display name.
- `enable` - (Optional, Boolean) Whether active. Default `true`.
- `protocol` - (Required, String) Protocol (`"torrent"` or `"usenet"`).
- `priority` - (Optional, Integer) Indexer priority. Default `25`.
- `implementation` - (Required, String) Engine implementation (`"Torznab"`, `"Newznab"`).
- `config_contract` - (Required, String) Contract name (`"TorznabSettings"`, `"NewznabSettings"`).
- `app_profile_id` - (Optional, Integer) Application profile ID. Default `1`.
- `field` - (Optional, Block Set) Configuration fields:
  - `name` - (Required, String) Field name.
  - `value` - (Optional, String) Non-sensitive setting value.
  - `sensitive_value` - (Optional, String, Sensitive, WriteOnly) Sensitive setting value (apiKeys, passkeys).
