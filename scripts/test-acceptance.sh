#!/usr/bin/env bash
set -euo pipefail

# Purpose: exercise a representative provider lifecycle against disposable Chaptarr.
# Usage: CHAPTARR_IMAGE=<tag@digest> CHAPTARR_VERSION=<version> ./scripts/test-acceptance.sh
# Prerequisites: Docker Compose, Go 1.25.12, OpenTofu 1.11.2, and curl.

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.12+auto}"

for command in docker go tofu curl; do
  if ! command -v "${command}" >/dev/null 2>&1; then
    echo "${command} is required for Chaptarr acceptance testing." >&2
    exit 1
  fi
done
if [[ "${CHAPTARR_IMAGE:-}" != *@sha256:* || -z "${CHAPTARR_VERSION:-}" ]]; then
  echo "Set CHAPTARR_IMAGE to an immutable tag-and-digest image and CHAPTARR_VERSION to its expected version." >&2
  exit 1
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="${repo_root}/acceptance/compose.yaml"
test_root="$(mktemp -d)"
tofu_test_root="${test_root}"
project_name="chaptarr-acceptance-${RANDOM}-$$"
export COMPOSE_PROJECT_NAME="${project_name}"

cleanup() {
  local prior_status=$?
  local cleanup_status=0
  trap - EXIT INT TERM

  if ! docker compose -f "${compose_file}" down --volumes --remove-orphans >/dev/null 2>&1; then
    echo "Disposable Chaptarr teardown failed." >&2
    cleanup_status=1
  fi
  if docker ps -a --filter "label=com.docker.compose.project=${project_name}" --format '{{.ID}}' | grep -q . ||
    docker volume ls --filter "label=com.docker.compose.project=${project_name}" --quiet | grep -q . ||
    docker network ls --filter "label=com.docker.compose.project=${project_name}" --quiet | grep -q .; then
    echo "Disposable Chaptarr project resources remain after teardown." >&2
    cleanup_status=1
  fi
  if ! rm -rf -- "${test_root}" || [[ -e "${test_root}" ]]; then
    echo "Disposable acceptance output could not be removed." >&2
    cleanup_status=1
  fi

  if [[ ${prior_status} -ne 0 ]]; then
    exit "${prior_status}"
  fi
  exit "${cleanup_status}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

run_tofu() {
  local stage="$1"
  shift
  if ! tofu -chdir="${tofu_test_root}/configuration" "$@" >"${test_root}/${stage}.out" 2>&1; then
    local classification="unclassified OpenTofu failure"
    if grep -q "Inconsistent dependency lock file" "${test_root}/${stage}.out"; then
      classification="dependency lock initialization"
    elif grep -qE "Failed to (load plugin schemas|instantiate provider)|could not load the schema" "${test_root}/${stage}.out"; then
      classification="provider discovery"
    elif grep -qE "Failed to query available provider packages|provider registry" "${test_root}/${stage}.out"; then
      classification="provider registry lookup"
    elif grep -qE "connection refused|Failed to connect|Client.Timeout" "${test_root}/${stage}.out"; then
      classification="provider API connection"
    fi
    local error_heading
    error_heading="$(sed -n 's/^Error: \([^[:cntrl:]]*\)$/\1/p' "${test_root}/${stage}.out" | head -n 1)"
    if [[ -n "${error_heading}" ]]; then
      classification="${classification}: ${error_heading}"
    fi
    echo "Acceptance stage ${stage} failed (${classification}); disposable output was withheld and will be removed." >&2
    return 1
  fi
  echo "Acceptance stage ${stage} passed."
}

mkdir -p "${test_root}/bin" "${test_root}/configuration"
cp "${repo_root}/acceptance/main.tf" "${test_root}/configuration/main.tf"

echo "Starting disposable loopback-only Chaptarr ${CHAPTARR_VERSION}."
docker pull "${CHAPTARR_IMAGE}" >/dev/null
docker compose -f "${compose_file}" config --quiet
docker compose -f "${compose_file}" up -d >/dev/null

api_key=""
for _ in $(seq 1 90); do
  api_key="$(docker compose -f "${compose_file}" exec -T chaptarr sh -c "sed -n 's:.*<ApiKey>\([^<]*\)</ApiKey>.*:\1:p' /config/config.xml 2>/dev/null" 2>/dev/null | tr -d '\r\n' || true)"
  if [[ -n "${api_key}" ]]; then
    break
  fi
  sleep 2
done
if [[ -z "${api_key}" ]]; then
  echo "Chaptarr did not create its disposable API credential in time." >&2
  exit 1
fi

published="$(docker compose -f "${compose_file}" port chaptarr 8789)"
host_port="${published##*:}"
base_url="http://127.0.0.1:${host_port}"
for _ in $(seq 1 90); do
  if curl --silent --show-error --fail --output /dev/null -H "X-Api-Key: ${api_key}" "${base_url}/api/v1/system/status" 2>/dev/null; then
    break
  fi
  sleep 2
done
if ! curl --silent --show-error --fail --output /dev/null -H "X-Api-Key: ${api_key}" "${base_url}/api/v1/system/status" 2>/dev/null; then
  echo "Chaptarr did not become ready in time." >&2
  exit 1
fi

provider_source="${repo_root}"
provider_binary="${test_root}/bin/terraform-provider-chaptarr"
if [[ "$(go env GOOS)" == "windows" ]]; then
  if ! command -v cygpath >/dev/null 2>&1; then
    echo "cygpath is required to normalize disposable paths for Windows OpenTofu." >&2
    exit 1
  fi
  tofu_test_root="$(cygpath -m -- "${test_root}")"
  provider_binary="${provider_binary}.exe"
fi
go build -trimpath -o "${provider_binary}" "${provider_source}"
cat >"${test_root}/tofurc" <<EOF
provider_installation {
  dev_overrides {
    "josh-archer/chaptarr" = "${tofu_test_root}/bin"
  }
  direct {}
}
EOF

export CHAPTARR_URL="${base_url}"
export CHAPTARR_API_KEY="${api_key}"
export TF_CLI_CONFIG_FILE="${tofu_test_root}/tofurc"
export TF_VAR_tag_label="tf-acceptance-${CHAPTARR_VERSION//./-}-initial"

run_tofu create apply -auto-approve -input=false -no-color
tag_id="$(tofu -chdir="${tofu_test_root}/configuration" output -raw tag_id 2>/dev/null)"
actual_version="$(tofu -chdir="${tofu_test_root}/configuration" output -raw chaptarr_version 2>/dev/null)"
if [[ ! "${tag_id}" =~ ^[1-9][0-9]*$ ]]; then
  echo "Disposable Chaptarr tag identity verification failed." >&2
  exit 1
fi
if [[ "${actual_version}" != "${CHAPTARR_VERSION}" && "${actual_version}" != "${CHAPTARR_VERSION}.0" ]]; then
  echo "Disposable Chaptarr version verification failed: expected ${CHAPTARR_VERSION}, received ${actual_version}." >&2
  exit 1
fi

export TF_VAR_tag_label="tf-acceptance-${CHAPTARR_VERSION//./-}-updated"
run_tofu update apply -auto-approve -input=false -no-color
run_tofu forget state rm chaptarr_tag.acceptance
run_tofu import import -input=false -no-color chaptarr_tag.acceptance "${tag_id}"

set +e
tofu -chdir="${tofu_test_root}/configuration" plan -detailed-exitcode -input=false -no-color >"${test_root}/no-drift.out" 2>&1
plan_status=$?
set -e
if [[ ${plan_status} -ne 0 ]]; then
  echo "Imported tag did not produce a no-drift plan; disposable output was withheld." >&2
  exit 1
fi
echo "Acceptance stage no-drift passed."

run_tofu destroy destroy -auto-approve -input=false -no-color
status="$(curl --silent --output /dev/null --write-out '%{http_code}' -H "X-Api-Key: ${api_key}" "${base_url}/api/v1/tag/${tag_id}" || true)"
if [[ "${status}" != "404" ]]; then
  echo "Disposable tag was not removed cleanly." >&2
  exit 1
fi

echo "Disposable Chaptarr ${CHAPTARR_VERSION} tag CRUD/import and read-only smoke passed."
