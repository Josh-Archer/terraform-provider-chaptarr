# Operational API policy

Chaptarr exposes maintenance endpoints for queues, searches, imports, backup
restore, upgrades, shutdown, and other runtime actions. Terraform plans and
refreshes must never trigger those operations.

## Provider policy

- `GET` endpoints may be classified as future data sources only when a typed,
  bounded schema can omit credentials, logs, raw request data, download URLs,
  internal paths, and other unsafe operational detail.
- `POST`, `PUT`, `PATCH`, and `DELETE` maintenance endpoints are classified
  `action-only` and `excluded`. The provider registers no action resource.
- Raw application/update logs and matching-log previews are intentionally
  unsupported because they can contain sensitive values or bulk payloads.
- A planned row in the generated coverage matrix is a design decision, not a
  registered provider feature.

This means `plan`, `refresh`, and data-source reads cannot start searches,
imports, downloads, backup creation/restores, queue removals/grabs, command
execution, upgrades, process restart/shutdown/reset, or release pushes.

## Read-only observations

The following families remain candidates for future typed and redacted data
sources: command status, queue status, history, wanted/missing/cutoff lists,
blocklist and ignored entries, manual-import candidates, pending-author-import
status, and backup inventory. They remain `planned` until their state schemas
can prove that operational URLs, request bodies, paths, and other sensitive
details are excluded.

The typed, whitelisted health/system/update observations implemented under
issue #10 are separate: they use only `GET`, bound responses, and deliberately
limited state.

## Imperative maintenance

Use access-controlled CI jobs or operational tooling for maintenance. Those
workflows can require human approval, short-lived authorization, explicit
targets, idempotency keys where supported, audit logs, and retry controls that
Terraform resource lifecycle cannot safely provide for these APIs.

If a future issue proposes an action resource, it must be create-only,
disabled unless explicitly enabled, aware of upstream idempotency semantics,
and protected against execution during refresh or destroy. No current Chaptarr
maintenance endpoint meets that bar in this provider.

The remote-path mapping connection test is not a maintenance mutation. Issue
#3 permits that read-only network/filesystem probe only when
`test_before_apply` is explicitly enabled, and only during create/update apply.
It never runs during plan, refresh, or destroy.
