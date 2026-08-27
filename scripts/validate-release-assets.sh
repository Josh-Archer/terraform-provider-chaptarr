#!/usr/bin/env bash
set -euo pipefail

allow_unsigned=false
if [[ "${1:-}" == "--allow-unsigned" ]]; then
  allow_unsigned=true
  shift
fi

if [[ $# -ne 1 ]]; then
  echo "usage: validate-release-assets.sh [--allow-unsigned] <vSemVer>" >&2
  exit 2
fi

release_tag="$1"
prerelease_identifier='(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)'
semver_pattern="^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-${prerelease_identifier}(\.${prerelease_identifier})*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$"
if [[ ! "${release_tag}" =~ ${semver_pattern} ]]; then
  echo "release tag must be exact v-prefixed SemVer" >&2
  exit 2
fi

version="${release_tag#v}"
prefix="terraform-provider-chaptarr_${version}"
mapfile -t assets

required=(
  "${prefix}_SHA256SUMS"
  "${prefix}_manifest.json"
)
if [[ "${allow_unsigned}" != "true" ]]; then
  required+=("${prefix}_SHA256SUMS.sig")
fi
for target in \
  darwin_amd64 darwin_arm64 \
  freebsd_386 freebsd_amd64 freebsd_arm freebsd_arm64 \
  linux_386 linux_amd64 linux_arm linux_arm64 \
  windows_386 windows_amd64 windows_arm64; do
  required+=("${prefix}_${target}.zip")
done

for expected in "${required[@]}"; do
  if ! printf '%s\n' "${assets[@]}" | grep -F -x -q -- "${expected}"; then
    echo "published release is missing required asset: ${expected}" >&2
    exit 1
  fi
done

echo "Published release contains every required provider asset name."
