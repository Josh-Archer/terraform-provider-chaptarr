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
- `admin_password` - (Required, String, Sensitive) PostgreSQL administrator password.
- `vaultwarden_bridge_url` - (Optional, String) Vaultwarden ESO bridge REST API endpoint URL.
- `vaultwarden_bridge_token` - (Optional, String, Sensitive) Bearer token for authenticating with the Vaultwarden bridge.
- `vaultwarden_secret_key` - (Optional, String, Sensitive) Secret item path key in Vaultwarden. Default `"media/chaptarr-postgres-credentials"`.
- `role_name` - (Optional, String) Role name to create in PostgreSQL. Default `"chaptarr"`.
- `role_password` - (Optional, String, Sensitive) Explicit role password. Automatically resolved from Vaultwarden if bridge parameters are set.
- `databases` - (Optional, List of String) List of databases owned by `role_name`. Default `["chaptarr-main", "chaptarr-log", "chaptarr-cache"]`.
- `ssl_mode` - (Optional, String) PostgreSQL SSL mode (`"require"`, `"prefer"`, `"disable"`). Default `"require"`.

## Attribute Reference

- `id` - Stable resource identifier (`server_host:server_port:role_name`).
- `is_healthy` - (Boolean) True if all databases exist and accept connections.
