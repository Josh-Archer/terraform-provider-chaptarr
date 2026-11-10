# Release gates

Release publication is a manually dispatched workflow for an existing tag. It
does not run from a pull request. Two checks sit in front of promotion, and a
third runs after publication.

## Consumer replay

`Consumer Replay (arr-config)` in CI applies
[`test/consumer/arr-config`](../test/consumer/arr-config) with the local
provider build, requires `tofu plan -detailed-exitcode` to exit 0, then
installs the previous published release from the OpenTofu registry and upgrades
back to the local build. The candidate version is the filesystem-mirror
sentinel `99.99.99`. The script refuses to install that sentinel, or the
release under test, from the registry.

The harness discards any ambient `CHAPTARR_URL` or `CHAPTARR_API_KEY` and
talks only to a loopback Chaptarr it starts. Plans and logs are scrubbed before
they are printed. Terraform state for the run is removed with the disposable
instance.

The release workflow's `Release candidate replay` job blocks the signed release
until that check succeeded on the tagged commit, or until this workflow replays
the tagged source successfully. An already published release with checksum
assets skips the replay. Prerelease tags skip it. A missing or failed Consumer
Replay check replays; it does not promote on its own.

## Lifecycle engines

Write-only attributes need Terraform 1.11.2 or OpenTofu 1.11.2 or newer.

| When | Engine | Version | Role | Chaptarr images |
| --- | --- | --- | --- | --- |
| Every pull request and main push | OpenTofu | 1.11.2 | Tested minimum | 0.9.929, 0.9.925, 0.9.914 |
| Every pull request and main push | Terraform | 1.17.1 | Tested current | 0.9.929 |
| Weekly `Lifecycle Matrix` | Terraform | 1.11.2 | Tested minimum | 0.9.929 |
| Weekly `Lifecycle Matrix` | Terraform | 1.17.1 | Tested current | 0.9.925, 0.9.914 |
| Weekly `Lifecycle Matrix` | OpenTofu | 1.13.9 | Tested current | 0.9.929 |

Each lane uses its own working directory, lock file, and registry source
(`registry.opentofu.org/josh-archer/chaptarr` or
`registry.terraform.io/josh-archer/chaptarr`). The lane runs init, apply,
refresh, update, import, a clean re-plan, and destroy against disposable
Chaptarr. The provider upgrade case is the consumer replay gate.

Expected compatibility is both engines at `>= 1.11.2`. The table lists
versions CI has been told to run. It is not a claim that every later patch
release was executed.

## Registry install

`Registry Install Check` runs on a schedule, on manual dispatch, and when a
release is published. It runs `terraform init` and `tofu init` for the exact
published version from each public registry, with signature verification left
on, and retries index lag. The lock file must record that version and a `zh:`
hash.

If the job fails, treat it as a failed verification. Do not delete or re-sign
the release from this workflow. A signature or checksum error needs the owner
signing key and the registry namespace, which stay outside CI. An index-lag
error after the retries means the registry has not listed the version yet;
rerun the workflow once the registry shows it. Snapshot artifact checks in CI
and the consumer replay gate remain the pre-publication checks.

## HashiCorp registry sync

`registry.terraform.io` syncs each provider version from the GitHub release
event and only lists a version whose release carries complete signed assets
(`*_SHA256SUMS` plus the detached `*_SHA256SUMS.sig` from the registered key).
It does not poll GitHub and does not self-heal a missed sync, unlike
`registry.opentofu.org`, which indexes releases on its own. Three distinct
miss shapes have occurred; only the last one needs no further repo change:

- Empty releases (`v0.11.8`, `v0.11.9`: release-please tags with zero assets)
  can never be listed anywhere. Leave them as history.
- Unsigned releases (`v0.11.10` through `v0.11.17`: no `.sig`) can never be
  listed by the HashiCorp registry. The release workflow now refuses to
  publish a stable tag without signing, and the release-state gate treats an
  unsigned stable release as unfinished, so re-dispatching one of these tags
  with signing available appends the missing signature instead of skipping.
- Signed releases whose sync was missed (`v0.11.18` through `v0.11.20`, and
  `v0.11.22`, all signed with the registered key and carrying complete
  assets) stay missing until an owner resyncs. `v0.11.21` shows the pipeline
  works when the signed release is complete at publish time.

A missed sync is an owner action outside CI and outside any workflow in this
repo: re-running the release workflow will not fix it (the gate correctly
reports the release as already published, and no new release event fires),
and the release must not be deleted or re-signed. Resync from the registry
provider settings for `josh-archer/chaptarr` (or redeliver the `release:
published` webhook event for the tag), then rerun Registry Install Check.
The release workflow now verifies HashiCorp indexing for ~20 minutes after
each publish and fails loudly with this pointer instead of going silently
stale.
