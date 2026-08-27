#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
validator="${repo_root}/scripts/validate-release-assets.sh"
version="1.2.3"
prefix="terraform-provider-chaptarr_${version}"

assets=(
  "${prefix}_SHA256SUMS"
  "${prefix}_SHA256SUMS.sig"
  "${prefix}_manifest.json"
)
for target in \
  darwin_amd64 darwin_arm64 \
  freebsd_386 freebsd_amd64 freebsd_arm freebsd_arm64 \
  linux_386 linux_amd64 linux_arm linux_arm64 \
  windows_386 windows_amd64 windows_arm64; do
  assets+=("${prefix}_${target}.zip")
done

printf '%s\n' "${assets[@]}" | bash "${validator}" "v${version}" >/dev/null

unsigned=()
for asset in "${assets[@]}"; do
  if [[ "${asset}" != "${prefix}_SHA256SUMS.sig" ]]; then
    unsigned+=("${asset}")
  fi
done
if printf '%s\n' "${unsigned[@]}" | bash "${validator}" "v${version}" >/dev/null 2>&1; then
  echo "release validator accepted an unsigned asset set without --allow-unsigned" >&2
  exit 1
fi
printf '%s\n' "${unsigned[@]}" | bash "${validator}" --allow-unsigned "v${version}" >/dev/null

for invalid_tag in 'not-a-tag' 'v1.2.3-01' 'v1.2.3-alpha.01'; do
  if printf '%s\n' "${assets[@]}" | bash "${validator}" "${invalid_tag}" >/dev/null 2>&1; then
    echo "release validator accepted invalid tag ${invalid_tag}" >&2
    exit 1
  fi
done

echo "Release asset validator accepts complete asset-name sets and rejects unsafe sets."
