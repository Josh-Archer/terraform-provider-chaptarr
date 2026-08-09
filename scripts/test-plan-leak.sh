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

  required_version = ">= 1.11.2"

  required_providers {
    chaptarr = {
      source = "josh-archer/chaptarr"
    }
  }
}

provider "chaptarr" {
  url = "https://chaptarr.example.test/reverse-proxy"
}

variable "oidc_client_secret" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "calibre_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "proxy_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

variable "metadata_api_key" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "chaptarr_host_config" "leak_test" {
  instance_name      = "plan-leak-test"
  oidc_client_secret = var.oidc_client_secret
}

resource "chaptarr_root_folder" "calibre" {
  name               = "Plan leak fixture"
  path               = "/library/plan-leak-fixture"
  folder_type        = "ebook"
  is_calibre_library = true
  host               = "calibre.example.test"
  port               = 8080
  library            = "fixture"
  output_profile     = "default"
  username           = "fixture"
  password           = var.calibre_password
}

resource "chaptarr_proxy" "fixture" {
  name     = "Plan leak proxy"
  type     = "http"
  hostname = "proxy.example.test"
  port     = 8080
  username = "fixture"
  password = var.proxy_password
}

resource "chaptarr_metadata" "fixture" {
  name              = "Plan leak metadata"
  implementation    = "FixtureMetadataProvider"
  config_contract   = "FixtureMetadataSettings"
  enable            = false
  field_values_json = jsonencode({ baseUrl = "https://metadata.example.test" })
  secret_fields     = { apiKey = var.metadata_api_key }
}
EOF

sentinel='CHAPTARR_TEST_API_KEY_SENTINEL_DO_NOT_USE_79f6f1d2'
host_sentinel='CHAPTARR_HOST_WRITE_ONLY_SENTINEL_DO_NOT_USE_913ad7c4'
calibre_sentinel='CHAPTARR_TEST_CALIBRE_PASSWORD_SENTINEL_DO_NOT_USE_3b4e911c'
proxy_sentinel='CHAPTARR_TEST_PROXY_PASSWORD_SENTINEL_DO_NOT_USE_46b2c884'
metadata_sentinel='CHAPTARR_TEST_METADATA_API_KEY_SENTINEL_DO_NOT_USE_8d187ab2'
export CHAPTARR_API_KEY="${sentinel}"
export TF_VAR_oidc_client_secret="${host_sentinel}"
export TF_VAR_calibre_password="${calibre_sentinel}"
export TF_VAR_proxy_password="${proxy_sentinel}"
export TF_VAR_metadata_api_key="${metadata_sentinel}"
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

if grep -F -R -e "${sentinel}" -e "${host_sentinel}" -e "${calibre_sentinel}" \
  -e "${proxy_sentinel}" -e "${metadata_sentinel}" -- \
  "${test_root}/stdout.txt" "${test_root}/stderr.txt" \
  "${test_root}/plan.json" "${test_root}/show-stderr.txt" >/dev/null; then
  echo "Synthetic API key leaked into OpenTofu output." >&2
  exit 1
fi

echo "OpenTofu plan output contains no synthetic credentials."
