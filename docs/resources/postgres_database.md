# chaptarr_postgres_database (Resource)

Manage Azure PostgreSQL roles, databases, and Vaultwarden secret resolution directly within the Chaptarr OpenTofu provider.

## Example Usage

```hcl
resource "chaptarr_postgres_database" "main" {
  server_host        = "homelabdb.postgres.database.azure.com"
  server_port        = 5432
  admin_username     = "homelabdbadmin"
  admin_password     = var.postgres_admin_password
  
  vaultwarden_bridge_url   = "http://vaultwarden-eso-bridge.external-secrets.svc.cluster.local:8080"
  vaultwarden_bridge_token = var.vaultwarden_bridge_token
  vaultwarden_secret_key   = "media/chaptarr-postgres-credentials"
  
  ssl_mode           = "require"
}
```

## Argument Reference

- `server_host` - (Required, String) PostgreSQL server hostname.
- `server_port` - (Optional, Integer) PostgreSQL server port. Default `5432`.
- `admin_username` - (Required, String) PostgreSQL administrator username.
- `admin_password` - (Required, String, Sensitive, Write-only) PostgreSQL administrator password. It is supplied only for create or update and is never stored in plan or state artifacts.
- `vaultwarden_bridge_url` - (Optional, String) Vaultwarden ESO bridge REST API endpoint URL.
- `vaultwarden_bridge_token` - (Optional, String, Sensitive, Write-only) Bearer token for authenticating with the Vaultwarden bridge. It is used only during create or update.
- `vaultwarden_secret_key` - (Optional, String, Sensitive, Write-only) Secret item path key in Vaultwarden. Default `"media/chaptarr-postgres-credentials"`; it is used only during create or update.
- `role_name` - (Optional, String) Role name to create in PostgreSQL. Default `"chaptarr"`.
- `role_password` - (Optional, String, Sensitive, Write-only) Explicit role password. Automatically resolved from Vaultwarden during create or update if bridge parameters are set. The resolved value is never retained.
- `databases` - (Optional, List of String) List of databases owned by `role_name`. Default `["chaptarr-main", "chaptarr-log", "chaptarr-cache"]`.
- `ssl_mode` - (Optional, String) PostgreSQL SSL mode (`"require"`, `"prefer"`, `"disable"`). Default `"require"`.

## Attribute Reference

- `id` - Stable resource identifier (`server_host:server_port:role_name`).
- `is_healthy` - (Boolean) Last successful create or update result.

## Credential and state handling

All credential inputs require OpenTofu 1.11.2 or later and should be supplied
from ephemeral variables. They are write-only: provide them for every create or
update that needs database authentication, but they are unavailable during a
refresh. Refresh therefore only removes any legacy credential fields from the
current state and preserves `is_healthy` as the last successful mutation
result; it does not contact PostgreSQL or the Vaultwarden bridge.

This release cleans credentials from the current state on upgrade or refresh.
It cannot erase historical versions retained by a remote-state backend. Rotate
any credentials previously configured with this resource, then follow the
backend's retention and state-history cleanup process.
