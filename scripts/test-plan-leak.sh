#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.12+auto}"

if ! command -v tofu >/dev/null 2>&1; then
  echo "OpenTofu is required for the provider plan leak smoke test." >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf -- "${test_root}"' EXIT

mkdir -p "${test_root}/bin" "${test_root}/configuration"
go build -trimpath -o "${test_root}/bin/terraform-provider-chaptarr" "${repo_root}"

cat >"${test_root}/tofurc" <<EOF
provider_installation {
  dev_overrides {
    "josh-archer/chaptarr" = "${test_root}/bin"
  }
  direct {}
}
EOF

cat >"${test_root}/configuration/main.tf" <<'EOF'
terraform {
  required_providers {
    chaptarr = {
      source = "josh-archer/chaptarr"
    }
  }
}

provider "chaptarr" {
  url = "https://chaptarr.example.test/reverse-proxy"
}
EOF

sentinel='CHAPTARR_TEST_API_KEY_SENTINEL_DO_NOT_USE_79f6f1d2'
export CHAPTARR_API_KEY="${sentinel}"
export TF_CLI_CONFIG_FILE="${test_root}/tofurc"
unset TF_LOG TF_LOG_PATH

set +e
tofu -chdir="${test_root}/configuration" plan \
  -input=false -no-color -out="${test_root}/plan.tfplan" \
  >"${test_root}/stdout.txt" 2>"${test_root}/stderr.txt"
plan_status=$?
set -e
if [[ ${plan_status} -ne 0 ]]; then
  echo "OpenTofu plan smoke test failed." >&2
  sed -n '1,160p' "${test_root}/stderr.txt" >&2
  exit "${plan_status}"
fi

tofu -chdir="${test_root}/configuration" show -json "${test_root}/plan.tfplan" \
  >"${test_root}/plan.json" 2>"${test_root}/show-stderr.txt"

if grep -F -R -- "${sentinel}" \
  "${test_root}/stdout.txt" "${test_root}/stderr.txt" \
  "${test_root}/plan.json" "${test_root}/show-stderr.txt" >/dev/null; then
  echo "Synthetic API key leaked into OpenTofu output." >&2
  exit 1
fi

echo "OpenTofu plan output contains no synthetic API key."
