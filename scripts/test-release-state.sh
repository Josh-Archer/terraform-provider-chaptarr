#!/usr/bin/env bash
# Focused test for scripts/check-release-state.sh. No network, no secrets:
# release payloads are canned fixtures exercising the registry-driven
# skip/rebuild/refuse decisions (see docs/release-gates.md).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="${repo_root}/scripts/check-release-state.sh"
version="1.2.3"
prefix="terraform-provider-chaptarr_${version}"

test_root="$(mktemp -d)"
trap 'rm -rf "${test_root}"' EXIT

pass() {
  echo "PASS $1"
}

fail() {
  echo "FAIL $1" >&2
  exit 1
}

write_assets() {
  local file="$1"
  shift
  printf '%s\n' "$@" >"${file}"
}

write_release() {
  # write_release <file> <draft> <asset-list-file>
  local file="$1" draft="$2" assets="$3"
  jq -n --argjson draft "${draft}" --rawfile names "${assets}" \
    '{draft: $draft, assets: ($names | split("\n") | map(select(length > 0)) | map({name: .}))}' \
    >"${file}"
}

signed_assets="${test_root}/signed.txt"
{
  echo "${prefix}_SHA256SUMS"
  echo "${prefix}_SHA256SUMS.sig"
  echo "${prefix}_manifest.json"
  for target in \
    darwin_amd64 darwin_arm64 \
    freebsd_386 freebsd_amd64 freebsd_arm freebsd_arm64 \
    linux_386 linux_amd64 linux_arm linux_arm64 \
    windows_386 windows_amd64 windows_arm64; do
    echo "${prefix}_${target}.zip"
  done
} >"${signed_assets}"

unsigned_assets="${test_root}/unsigned.txt"
grep -v '\.sig$' "${signed_assets}" >"${unsigned_assets}"

signed_release="${test_root}/signed.json"
write_release "${signed_release}" false "${signed_assets}"
unsigned_release="${test_root}/unsigned.json"
write_release "${unsigned_release}" false "${unsigned_assets}"
empty_release="${test_root}/empty.json"
: >"${test_root}/empty-assets.txt"
write_release "${empty_release}" false "${test_root}/empty-assets.txt"

# Signed stable release: already complete, skips with or without secrets.
[[ "$(bash "${checker}" "v${version}" true "${signed_release}")" == "published=true" ]] \
  || fail "signed stable release must skip"
pass "signed stable release skips"
[[ "$(bash "${checker}" "v${version}" false "${signed_release}")" == "published=true" ]] \
  || fail "complete release must stay a quiet skip without secrets"
pass "complete release skips without secrets"

# Unsigned stable release (the v0.11.10-v0.11.17 gap): must rebuild, never skip.
[[ "$(bash "${checker}" "v${version}" true "${unsigned_release}")" == "published=false" ]] \
  || fail "unsigned stable release must rebuild when signing is available"
pass "unsigned stable release rebuilds instead of skipping"
if bash "${checker}" "v${version}" false "${unsigned_release}" \
  >"${test_root}/refused.out" 2>"${test_root}/refused.err"; then
  fail "unsigned stable release without signing must be refused"
fi
grep -q "without detached GPG signing" "${test_root}/refused.err" \
  || fail "refusal must name the signing requirement"
pass "unsigned stable release without signing is refused"

# Empty stable releases (v0.11.8/v0.11.9): build when signing exists, refuse otherwise.
[[ "$(bash "${checker}" "v${version}" true "${empty_release}")" == "published=false" ]] \
  || fail "empty stable release must build when signing is available"
pass "empty stable release builds when signing is available"
if bash "${checker}" "v${version}" false "${empty_release}" >/dev/null 2>&1; then
  fail "empty stable release without signing must be refused"
fi
pass "empty stable release without signing is refused"

# Missing payload (no GitHub release yet): same rule as empty.
[[ "$(bash "${checker}" "v${version}" true)" == "published=false" ]] \
  || fail "missing release must build when signing is available"
pass "missing release builds when signing is available"
if bash "${checker}" "v${version}" false >/dev/null 2>&1; then
  fail "missing release without signing must be refused"
fi
pass "missing release without signing is refused"

# Prereleases keep the unsigned-tolerant path: no registry consumes them.
[[ "$(bash "${checker}" "v${version}-rc.1" false "${unsigned_release}")" == "published=false" ]] \
  || fail "unsigned prerelease without signing must still build"
pass "unsigned prerelease still builds without signing"

# Drafts are never complete, whatever their assets look like.
draft_release="${test_root}/draft.json"
write_release "${draft_release}" true "${signed_assets}"
[[ "$(bash "${checker}" "v${version}" true "${draft_release}")" == "published=false" ]] \
  || fail "draft release must not count as published"
pass "draft release is not complete"

for bad in 'not-a-tag' 'v1.2' './signed.json' ; do
  if bash "${checker}" "${bad}" true "${signed_release}" >/dev/null 2>&1; then
    fail "invalid tag '${bad}' was accepted"
  fi
done
pass "invalid tags are rejected"

echo "Release state gate skips only complete signed stable releases and refuses unsigned stable publishes."
