# chaptarr_download_client (Resource)

Manage a Chaptarr download client registration (e.g. Transmission, SABnzbd, qBittorrent).

## Example Usage

```hcl
resource "chaptarr_download_client" "transmission" {
  name           = "Transmission"
  enable         = true
  protocol       = "torrent"
  priority       = 1
  implementation = "Transmission"
  config_contract = "TransmissionSettings"
  
  field {
    name  = "host"
    value = "transmission.media.svc.cluster.local"
  }
  field {
    name  = "port"
    value = "9091"
  }
  field {
    name  = "urlBase"
    value = "/transmission/"
  }
  field {
    name  = "audiobookCategory"
    value = "audiobooks"
  }
  field {
    name            = "password"
    sensitive_value = var.transmission_password
  }
}
```

## Argument Reference

- `name` - (Required, String) Download client display name.
- `enable` - (Optional, Boolean) Whether active. Default `true`.
- `protocol` - (Required, String) Protocol (`"torrent"` or `"usenet"`).
- `priority` - (Optional, Integer) Download priority. Default `1`.
- `implementation` - (Required, String) Engine implementation (`"Transmission"`, `"Sabnzbd"`, `"QBittorrent"`).
- `config_contract` - (Required, String) Contract name (`"TransmissionSettings"`, `"SabnzbdSettings"`, `"QBittorrentSettings"`).
- `remove_completed_downloads` - (Optional, Boolean) Default `true`.
- `remove_failed_downloads` - (Optional, Boolean) Default `true`.
- `field` - (Optional, Block Set) Configuration fields:
  - `name` - (Required, String) Field name.
  - `value` - (Optional, String) Non-sensitive setting value.
  - `sensitive_value` - (Optional, String, Sensitive, WriteOnly) Sensitive setting value (passwords, tokens).
