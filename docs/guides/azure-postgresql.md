# PostgreSQL composition boundary

`chaptarr_postgres_database` manages a PostgreSQL role and its application
databases through a configured PostgreSQL endpoint. It can be used with Azure
Database for PostgreSQL, but it does not provision Azure resources. This guide
states the boundary between that resource and the infrastructure, secret, and
workload systems around it.

## Provider-managed database lifecycle

On create and update, the resource connects with the configured PostgreSQL
administrator. It creates a missing application role or updates that role's
password, creates missing requested databases with that role as owner, and
grants the role access to each database's `public` schema. Because database and
bridge credentials are write-only, refresh does not reconnect; it preserves the
last successful mutation result and removes any legacy credential fields from
the current state.

Destroy deliberately relinquishes OpenTofu ownership; it does not drop the
role or databases. Database deletion, replacement, backups, restores, and
disaster recovery therefore remain separate, explicitly approved operations.

The resource accepts either an explicit role password or a password resolved
through the Vaultwarden bridge during create or update. When bridge
configuration is supplied, the provider makes an authenticated read request
for an existing item property and uses the returned value only for that
database mutation. Refresh and import never contact the bridge; neither the
bridge lookup nor this resource creates, updates, or rotates a Vaultwarden
item.

Database and bridge credentials are Sensitive and WriteOnly, so OpenTofu does
not retain them in plan or current state artifacts. Supply them from ephemeral
variables for each create or update. Upgrading or refreshing also removes
legacy credential values from the current state. Rotate any credentials that
were configured before this security fix and use the state backend's retention
and history-cleanup process: a provider cannot erase previously retained remote
state versions.

## External responsibilities

| Responsibility | Owner outside this provider |
| --- | --- |
| Azure PostgreSQL server, networking, private connectivity, identity, backups, and availability policy | Azure infrastructure composition |
| Vaultwarden item bootstrap, generation, storage, rotation, revocation, and recovery | Approved secret-store automation and operators |
| External Secrets Operator lookup, Kubernetes Secret projection, and workload environment or file delivery | Kubernetes and workload composition |
| Coordinating a role-password change with the secret store, projected workload secret, and workload restart or reload | Deployment operators and runbooks |
| Chaptarr process startup, application configuration, schema migrations, health checks, and rollback | Chaptarr workload composition and operators |
| Existing-data transfer, cutover, integrity checks, and rollback between deployments | A separately tested migration runbook |

The provider connects to an existing PostgreSQL service; it neither creates an
Azure server nor configures Azure networking or identity. It also does not
create a Vaultwarden item, deliver a secret through External Secrets Operator,
or configure a Kubernetes workload to consume one.

## Deployment and migration limits

Database and role creation do not prove a deployable Chaptarr installation.
The consuming deployment must supply its own connection settings, arrange
secret delivery, start the workload, and verify its health. Chaptarr itself is
responsible for any application schema migration after it starts; this resource
does not migrate application data, transfer an existing deployment, perform a
cutover, or verify a live Azure, database, secret-delivery, or workload
environment.

Before a production rollout, validate the complete path in the repository and
environment that own those systems: PostgreSQL connectivity and privileges,
secret bootstrap and rotation, ESO delivery, workload startup, application
migrations, backups, and rollback.
