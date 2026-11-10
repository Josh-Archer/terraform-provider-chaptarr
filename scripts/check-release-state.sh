#!/usr/bin/env bash
# Decide whether a manually dispatched release still has work to do.
#
# Usage: check-release-state.sh <vSemVer> <signing-enabled:true|false> [release.json]
#
# release.json is a `gh api .../releases/tags/<tag>` payload. When it is
# omitted or missing, the tag is treated as having no published release yet.
#
# Output (stdout): exactly one of `published=true` or `published=false`.
# A stable release is complete only when its asset names validate *with* the
# detached GPG signature: registry.terraform.io never lists a version without
# one, so a 15-asset unsigned release must rebuild, not skip. A prerelease
# keeps the unsigned-tolerant check because no public registry consumes it.
#
# A stable tag with signing disabled and no complete release is refused
# (exit 1): publishing it unsigned would repeat the registry gap, and the
# release can be dispatched again once signing is available. An already
# complete release stays a quiet skip even without secrets.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
validator="${repo_root}/scripts/validate-release-assets.sh"

if [[ $# -lt 2 || $# -gt 3 ]]; then
  echo "usage: check-release-state.sh <vSemVer> <true|false> [release.json]" >&2
  exit 2
fi

release_tag="$1"
signing_enabled="$2"
release_json="${3:-}"

semver_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
if [[ ! "${release_tag}" =~ ${semver_pattern} ]]; then
  echo "release tag must be exact v-prefixed SemVer" >&2
  exit 2
fi

if [[ "${signing_enabled}" != "true" && "${signing_enabled}" != "false" ]]; then
  echo "signing flag must be 'true' or 'false'" >&2
  exit 2
fi

prerelease=false
if [[ "${release_tag}" == *-* ]]; then
  prerelease=true
fi

complete=false
if [[ -n "${release_json}" && -f "${release_json}" ]]; then
  if jq -e . >/dev/null 2>&1 <"${release_json}"; then
    draft="$(jq -r 'if .draft == false then "false" else "true" end' <"${release_json}")"
    if [[ "${draft}" == "false" ]]; then
      flags=()
      if [[ "${prerelease}" == "true" ]]; then
        flags+=("--allow-unsigned")
      fi
      if jq -r '.assets[].name' <"${release_json}" \
        | bash "${validator}" "${flags[@]}" "${release_tag}" >/dev/null 2>&1; then
        complete=true
      fi
    fi
  fi
fi

if [[ "${complete}" == "true" ]]; then
  echo "published=true"
  exit 0
fi

echo "published=false"
if [[ "${prerelease}" == "false" && "${signing_enabled}" != "true" ]]; then
  echo "Refusing to publish stable ${release_tag} without detached GPG signing: registry.terraform.io never lists an unsigned version." >&2
  echo "Configure the release signing secrets and dispatch again; nothing was published." >&2
  exit 1
fi
