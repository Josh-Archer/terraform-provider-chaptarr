# Import an existing Chaptarr instance

`chaptarr-import` reads one live Chaptarr server and writes OpenTofu
configuration, import blocks, and sensitive variable declarations. It does not
apply a plan, and it does not claim that every Chaptarr setting is safe to
manage from Terraform.

Run it from a checkout of this provider:

```shell
export CHAPTARR_URL="https://chaptarr.example.test"
export CHAPTARR_API_KEY="the-api-key"
go run ./tools/importer -out-dir ./chaptarr-import
```

There is no default URL or port. Set `-url` or `CHAPTARR_URL`. The API key is
sent only as the `X-Api-Key` header. Prefer `CHAPTARR_API_KEY` over `-api-key`
so the key is not stored in shell history. The tool warns and continues when
the key is empty.

`-format` is `all` (the default), `hcl`, `imports`, or `script`. An unknown
format exits before any HTTP request. `-timeout` is the per-request timeout in
seconds (default 30). `-provider-header` includes the `terraform` and
`provider` blocks (default true). The generated provider constraint is
`version = "~> 0.11"` and `required_version` is `>= 1.11.2`.

The output directory is created mode `0700`. `main.tf`, `variables.tf`, and
`imports.tf` are mode `0600`. `import.sh` is mode `0700`. A second run refuses
to replace an existing file unless you pass `-overwrite` or `-force`.

When discovery finds nothing to emit, the command exits 0, prints the manual
notes, and does not write empty files. HTTP 404 is treated as an unsupported
endpoint and skipped. Authentication, transport, and server errors fail the
run before any file is written. `-allow-partial` writes the resources
discovered so far and lists those errors. Diagnostics do not include response
bodies.

## Review, import, and confirm a clean plan

1. Read `main.tf`, `imports.tf`, and `variables.tf`.
2. Supply every sensitive variable with `TF_VAR_*` or an uncommitted tfvars
   file. Generated files contain variable references, not credential values.
3. Export `CHAPTARR_URL` and `CHAPTARR_API_KEY` for the provider.
4. Run `tofu init` or `terraform init`.
5. Run `tofu apply` or `terraform apply`. Import blocks adopt the existing
   objects.
6. Run `tofu plan` or `terraform plan` and confirm there are no changes.
7. Delete `imports.tf` after the import succeeds so a later apply does not
   import those addresses again.

`import.sh` is a legacy `terraform import` script. Prefer the import blocks.

## Sensitive values and state

Write-only attributes (passwords, tokens, and API-key fields) are declared as
sensitive variables with no default. Import does not store those values, so
the import apply sends the variable once. A later plan is empty while the
variable is unchanged. After apply, the write-only attribute is null in state,
but a configured value is still sent to Chaptarr on the next update. Change
the variable and apply to rotate it.

Every other attribute the importer emits is stored in state in plaintext,
including host names, paths, profile identifiers, and non-secret provider
fields. Use an encrypted state backend or OpenTofu state encryption. Do not
commit state, plans, or tfvars files.

## What stays manual

The importer always leaves `chaptarr_postgres_database` unmanaged. It is not a
Chaptarr API object. Adopt it by hand with the server host, port, role, and
write-only passwords. See the
[PostgreSQL composition boundary](azure-postgresql.md). This tool does not
recover that resource or its credentials.

When `GET /api/v1/config/hardcover` returns a populated object,
`chaptarr_hardcover_config` is left out of the generated files so import does
not contact Hardcover or rotate its token. Adopt it by hand only when you
intend to manage that token. Its import id is the literal `hardcover`. A 404
means the endpoint is absent; the tool records that and does not add a manual
note.

Other objects are named in the manual-adoption notes when a required value
cannot be reconstructed. A series with no media type is one example. The note
includes the resource type and the live id. The live object is not modified.

## Import identifiers

| Resource | Import id |
| --- | --- |
| Singleton configuration (`chaptarr_host_config`, `chaptarr_ui_config`, and the other `*_config` singletons) | Type name, such as `host_config` |
| `chaptarr_edition` | `<book_id>/<edition_id>` |
| `chaptarr_hardcover_config`, if you add it by hand | `hardcover` |
| `chaptarr_quality_definition` | Quality-definition id |
| Every other emitted resource | Numeric Chaptarr id |

## Fields the importer leaves unset

Search, file-move, and delete flags on authors and books stay unset so import
does not search, move, or delete. Book `lookup_json` is not recoverable from
the API. Series `selected_books` stays empty, and `preferred_narrator` is
observed rather than managed, so a later update does not call the
preferred-narrator endpoint. An imported edition keeps `monitored = true`;
destroy forgets Terraform ownership and does not change the edition.

`chaptarr_root_folder.allow_destroy` is `false`, so Terraform will not remove
the root-folder registration. `chaptarr_remote_path_mapping.test_before_apply`
is `false`, so import does not call the connection test. Integration
`test_on_apply` is emitted only when that integration is already enabled.
Download-client `tags` are computed by Chaptarr and are not emitted;
`audiobook_tags` and `ebook_tags` are emitted when they are set.

Direct quality leaves omit server-owned ids and names. Quality groups keep
the id and name Chaptarr requires.
