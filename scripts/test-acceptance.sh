#!/usr/bin/env bash
set -euo pipefail

# Purpose: exercise a representative provider lifecycle against disposable Chaptarr.
# Usage: CHAPTARR_IMAGE=<tag@digest> CHAPTARR_VERSION=<version> [CHAPTARR_ENGINE=tofu|terraform] ./scripts/test-acceptance.sh
# Prerequisites: Go 1.25.13, OpenTofu 1.11.2 or Terraform 1.11.2 (write-only
# attributes), curl, and either Docker Compose (CHAPTARR_RUNTIME=docker) or
# curl+jq+tar+sha256sum (CHAPTARR_RUNTIME=native, which unpacks the same
# digest-pinned image and runs it as a loopback-only process for runners
# without Docker). Default engine is OpenTofu. Default runtime is docker if
# available, else native.
# Only a disposable 127.0.0.1 instance started here is ever targeted; any
# pre-existing CHAPTARR_URL/CHAPTARR_API_KEY in the environment is discarded.
# Each run uses its own working directory, lock file, and registry source
# address (registry.opentofu.org for OpenTofu, registry.terraform.io for Terraform).

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.13+auto}"

unset CHAPTARR_URL CHAPTARR_API_KEY
engine="${CHAPTARR_ENGINE:-tofu}"
case "${engine}" in
  tofu) registry_source="registry.opentofu.org/josh-archer/chaptarr" ;;
  terraform) registry_source="registry.terraform.io/josh-archer/chaptarr" ;;
  *) echo "CHAPTARR_ENGINE must be tofu or terraform." >&2; exit 1 ;;
esac
runtime="${CHAPTARR_RUNTIME:-}"
if [[ -z "${runtime}" ]]; then
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    runtime=docker
  else
    runtime=native
  fi
fi
case "${runtime}" in
  docker) required=(docker go "${engine}" curl) ;;
  native) required=(go "${engine}" curl jq tar sha256sum) ;;
  *) echo "CHAPTARR_RUNTIME must be docker or native." >&2; exit 1 ;;
esac
for command in "${required[@]}"; do
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

  if [[ "${runtime}" == native ]]; then
    if [[ -n "${native_pid:-}" ]] && kill -0 "${native_pid}" 2>/dev/null; then
      kill "${native_pid}" 2>/dev/null || true
      for _ in $(seq 1 20); do kill -0 "${native_pid}" 2>/dev/null || break; sleep 1; done
      kill -9 "${native_pid}" 2>/dev/null || true
    fi
  elif ! docker compose -f "${compose_file}" down --volumes --remove-orphans >/dev/null 2>&1; then
    echo "Disposable Chaptarr teardown failed." >&2
    cleanup_status=1
  fi
  if [[ "${runtime}" == docker ]] && { grep -q . <<<"$(docker ps -a --filter "label=com.docker.compose.project=${project_name}" --format '{{.ID}}')" ||
    grep -q . <<<"$(docker volume ls --filter "label=com.docker.compose.project=${project_name}" --quiet)" ||
    grep -q . <<<"$(docker network ls --filter "label=com.docker.compose.project=${project_name}" --quiet)"; }; then
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

run_tf() {
  local stage="$1"
  shift
  if ! "${engine}" -chdir="${tofu_test_root}/configuration" "$@" >"${test_root}/${stage}.out" 2>&1; then
    local classification="unclassified ${engine} failure"
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

echo "Starting disposable loopback-only Chaptarr ${CHAPTARR_VERSION} (${runtime})."
if [[ "${runtime}" == docker ]]; then
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
else
  # shellcheck source=scripts/lib/native-chaptarr.sh
  source "${repo_root}/scripts/lib/native-chaptarr.sh"
  native_start "${CHAPTARR_IMAGE}" "${test_root}/native"
fi
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
    echo "cygpath is required to normalize disposable paths for Windows ${engine}." >&2
    exit 1
  fi
  tofu_test_root="$(cygpath -m -- "${test_root}")"
  provider_binary="${provider_binary}.exe"
fi
go build -trimpath -o "${provider_binary}" "${provider_source}"
sed -i "s#source = \"josh-archer/chaptarr\"#source = \"${registry_source}\"#" "${test_root}/configuration/main.tf"
cat >"${test_root}/tofurc" <<EOF
provider_installation {
  dev_overrides {
    "${registry_source}" = "${tofu_test_root}/bin"
  }
  direct {}
}
EOF

export CHAPTARR_URL="${base_url}"
export CHAPTARR_API_KEY="${api_key}"
export TF_CLI_CONFIG_FILE="${tofu_test_root}/tofurc"
export TF_VAR_tag_label="tf-acceptance-${CHAPTARR_VERSION//./-}-initial"
echo "Lifecycle engine ${engine}: $("${engine}" version | head -n 1)"
echo "Lifecycle registry source: ${registry_source}"

run_tf init init -input=false -no-color
run_tf create apply -auto-approve -input=false -no-color
run_tf refresh refresh -input=false -no-color
tag_id="$("${engine}" -chdir="${tofu_test_root}/configuration" output -raw tag_id 2>/dev/null)"
actual_version="$("${engine}" -chdir="${tofu_test_root}/configuration" output -raw chaptarr_version 2>/dev/null)"
if [[ ! "${tag_id}" =~ ^[1-9][0-9]*$ ]]; then
  echo "Disposable Chaptarr tag identity verification failed." >&2
  exit 1
fi
if [[ "${actual_version}" != "${CHAPTARR_VERSION}" && "${actual_version}" != "${CHAPTARR_VERSION}.0" ]]; then
  echo "Disposable Chaptarr version verification failed: expected ${CHAPTARR_VERSION}, received ${actual_version}." >&2
  exit 1
fi

export TF_VAR_tag_label="tf-acceptance-${CHAPTARR_VERSION//./-}-updated"
run_tf update apply -auto-approve -input=false -no-color
run_tf refresh-updated refresh -input=false -no-color
run_tf forget state rm chaptarr_tag.acceptance
run_tf import import -input=false -no-color chaptarr_tag.acceptance "${tag_id}"
run_tf refresh-imported refresh -input=false -no-color

set +e
"${engine}" -chdir="${tofu_test_root}/configuration" plan -detailed-exitcode -input=false -no-color >"${test_root}/no-drift.out" 2>&1
plan_status=$?
set -e
if [[ ${plan_status} -ne 0 ]]; then
  echo "Imported tag did not produce a no-drift plan; disposable output was withheld." >&2
  exit 1
fi
echo "Acceptance stage no-drift passed."

run_tf destroy destroy -auto-approve -input=false -no-color
status="$(curl --silent --output /dev/null --write-out '%{http_code}' -H "X-Api-Key: ${api_key}" "${base_url}/api/v1/tag/${tag_id}" || true)"
if [[ "${status}" != "404" ]]; then
  echo "Disposable tag was not removed cleanly." >&2
  exit 1
fi

echo "Disposable Chaptarr ${CHAPTARR_VERSION} lifecycle passed with ${engine} $("${engine}" version | head -n 1)."
