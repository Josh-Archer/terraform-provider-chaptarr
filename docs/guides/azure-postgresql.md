# Azure PostgreSQL composition boundary

This provider manages supported settings through the Chaptarr API. It does not
provision PostgreSQL, bootstrap database roles, deliver workload secrets, or
migrate application data. Those responsibilities belong to the infrastructure
and workload repositories that compose a Chaptarr deployment.

Issue #13 is closed by documenting that the requested deployment integration
belongs in the owning infrastructure and workload repositories, not this
provider repository. It does not add an Azure deployment feature to
`terraform-provider-chaptarr`.

## Chaptarr startup contract

A PostgreSQL-backed Chaptarr process expects these startup settings:

| Setting | Meaning |
|---|---|
| `Host` | PostgreSQL network host |
| `Port` | PostgreSQL network port |
| `User` | Application database role |
| `Password` | Credential for that role |
| `MainDb` | Main application database name |
| `LogDb` | Log database name |
| `CacheDb` | Cache database name |

The three named databases must exist before Chaptarr starts. The application
role must be able to connect to them and perform the operations required by
Chaptarr. After connectivity is available, Chaptarr startup owns its table and
schema migrations inside those databases. Infrastructure automation must not
attempt to recreate or independently advance Chaptarr's internal tables.
This creates or upgrades Chaptarr's schema in precreated databases; it does not
copy data from SQLite or another existing deployment. Existing-data transfer is
unsupported and unvalidated here and requires an external migration runbook.

## Ownership boundary

| Responsibility | Owner |
|---|---|
| Supported settings exposed by the running Chaptarr API | This provider |
| Azure PostgreSQL server, networking, identity, and availability policy | Azure infrastructure composition |
| Three database creations, SQL role creation, and grants | Database bootstrap composition |
| Credential generation, rotation, and revocation | Secret and database operators |
| Vaultwarden writes or other secret-store updates | Secret-store automation |
| External Secrets Operator and Kubernetes Secret projection | Kubernetes platform composition |
| Chaptarr workload startup wiring | Workload or GitOps composition |
| Existing-data transfer, cutover, rollback, and integrity checks | Migration runbook and operators |
| Cloud, database, secret-delivery, and application validation | Owning deployment repositories |

AzureRM resources, SQL role and grant tooling, Vaultwarden integrations,
External Secrets Operator resources, Kubernetes workload definitions, and
data-migration procedures are therefore deliberately outside this repository.

The issue-linked
[vaultwarden-eso-bridge](https://github.com/Josh-Archer/vaultwarden-eso-bridge)
is a read path for External Secrets Operator lookups. It retrieves existing
Vaultwarden item properties; it does not generate credentials or create and
update Vaultwarden items. Item bootstrap and rotation therefore require
separate, approved secret-store automation.

## Symbolic composition order

The dependency order is:

1. Establish the Azure database service and private connectivity.
2. Precreate the main, log, and cache databases.
3. Create the least-privileged application role and grant only required access.
4. Place the credential under the chosen secret system's ownership.
5. Project the startup settings into the Chaptarr workload.
6. Start Chaptarr and allow its own migrations to finish.
7. Validate application health and only then manage supported API settings with
   this provider.

This ordering is descriptive, not executable infrastructure configuration.

## Lifecycle and security warnings

- Do not store database passwords, secret-store payloads, or rendered workload
  secrets in Terraform state, plans, logs, repository files, or CI artifacts.
- Keep database administration authority separate from the Chaptarr application
  role. Do not grant broad server or database-owner privileges merely to simplify
  bootstrap.
- Coordinate credential rotation across PostgreSQL, the secret system, secret
  projection, and workload restart. An uncoordinated rotation can make Chaptarr
  unavailable.
- Treat database deletion, replacement, restore, and migration as destructive
  operations with backups, explicit approval, rollback criteria, and integrity
  checks.
- Do not run competing schema migration tools against Chaptarr's internal
  tables. Version compatibility and rollback behavior remain Chaptarr concerns.

## Validation status

This guide records a composition contract only. It has not provisioned Azure
resources, created SQL roles or grants, written a secret, started a Kubernetes
workload, transferred existing data, or validated a live cloud deployment.
Those checks must pass in the repositories and environments that own them.
