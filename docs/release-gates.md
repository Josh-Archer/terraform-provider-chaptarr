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
