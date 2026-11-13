#!/usr/bin/env bash
# Purpose: replay a sanitized consumer configuration (test/consumer/arr-config)
# against a disposable Chaptarr and require a clean re-plan, including after a
# provider upgrade.
#   1. fresh apply with the locally built provider (filesystem mirror), then
#      `tofu plan -detailed-exitcode` == 0
#   2. destroy, apply with the previously published provider from the registry,
#      switch back to the local build, `tofu plan -detailed-exitcode` == 0, apply, plan == 0
# The version under test is never installed from the registry. It is published
# only after this replay has passed and the Release workflow promotes the tag.
# Ambient CHAPTARR_URL / CHAPTARR_API_KEY values are discarded. The only target
# is a loopback instance this script starts.
# Usage:
#   CHAPTARR_IMAGE=<tag@digest> CHAPTARR_VERSION=<version> bash ./scripts/test-consumer-replay.sh
# Environment:
#   CHAPTARR_REPLAY_PREVIOUS_VERSION   previous released version (X.Y.Z); default: newest published stable tag older than the candidate
#   CHAPTARR_REPLAY_RELEASE_VERSION    candidate release (X.Y.Z). Versions greater than or equal to it are not a valid upgrade baseline.
#   CHAPTARR_REPLAY_SKIP_UPGRADE=true  only run phase 1
#   CHAPTARR_RUNTIME=docker|native     same disposable runtimes as scripts/test-acceptance.sh
set -euo pipefail

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.13+auto}"

# Sentinel version for the filesystem mirror. It must not exist in the public registry.
current_version="99.99.99"

version_lt() {
  local left right sorted oldest
  left="${1#v}"
  right="${2#v}"
  # Avoid `sort | head`: under pipefail, SIGPIPE makes that pipeline flaky.
  sorted="$(printf '%s\n%s\n' "${left}" "${right}" | LC_ALL=C sort -V)"
  oldest="${sorted%%$'\n'*}"
  [[ "${left}" != "${right}" && "${oldest}" == "${left}" ]]
}

# stdin: `git ls-remote --tags` lines. Stable X.Y.Z only.
# Prefer the peeled commit SHA (^{}) over an annotated tag object SHA so HEAD
# comparison still works when the tag object line is listed last.
parse_stable_tag_shas() {
  awk '
    {
      peeled = ($2 ~ /\^\{\}$/)
      name = $2
      sub("^refs/tags/v", "", name)
      sub("\\^\\{\\}$", "", name)
      if (name !~ /^[0-9]+\.[0-9]+\.[0-9]+$/) next
      if (peeled || !(name in sha)) sha[name] = $1
    }
    END {
      for (v in sha) print sha[v], v
    }
  '
}

# Newest published stable version strictly older than the release under test.
# Callers install that baseline from the registry. The candidate build is separate.
previous_version() {
  local explicit
  explicit="${CHAPTARR_REPLAY_PREVIOUS_VERSION:-}"
  if [[ -n "${explicit}" ]]; then
    printf '%s\n' "${explicit#v}"
    return 0
  fi

  local head_sha published upper tags sha version best
  head_sha="$(git rev-parse HEAD)"
  if ! published="$(curl -fsSL --retry 3 "${CHAPTARR_REPLAY_REGISTRY_VERSIONS_URL:-https://registry.opentofu.org/v1/providers/josh-archer/chaptarr/versions}" \
    | grep -oE '"version" *: *"[^"]+"' \
    | sed -E 's/.*"([^"]+)"$/\1/')"; then
    echo "consumer replay FAILED: could not list published provider versions from the registry" >&2
    return 1
  fi
  if [[ -z "${published}" ]]; then
    echo "consumer replay FAILED: registry returned no provider versions" >&2
    return 1
  fi

  if ! tags="$(git ls-remote --tags "${CHAPTARR_REPLAY_REPO_URL:-https://github.com/Josh-Archer/terraform-provider-chaptarr.git}" 'v*' \
    | parse_stable_tag_shas)"; then
    echo "consumer replay FAILED: could not list release tags" >&2
    return 1
  fi

  upper="${CHAPTARR_REPLAY_RELEASE_VERSION:-}"
  upper="${upper#v}"
  if [[ -z "${upper}" ]]; then
    while read -r sha version; do
      [[ -n "${version}" ]] || continue
      if [[ "${sha}" == "${head_sha}" ]] && { [[ -z "${upper}" ]] || version_lt "${upper}" "${version}"; }; then
        upper="${version}"
      fi
    done <<< "${tags}"
  fi

  best=""
  while read -r sha version; do
    [[ -n "${version}" ]] || continue
    if [[ -n "${upper}" ]] && ! version_lt "${version}" "${upper}"; then
      continue
    fi
    if [[ "${sha}" == "${head_sha}" ]]; then
      continue
    fi
    if ! grep -qxF "${version}" <<< "${published}"; then
      continue
    fi
    if [[ -z "${best}" ]] || version_lt "${best}" "${version}"; then
      best="${version}"
    fi
  done <<< "${tags}"

  if [[ -z "${best}" ]]; then
    echo "consumer replay FAILED: no published stable provider version is older than the candidate" >&2
    return 1
  fi
  printf '%s\n' "${best}"
}

# The registry copy of the release under test does not exist until promotion.
reject_registry_version() {
  local version release
  version="$1"
  if [[ "${version}" == "${current_version}" ]]; then
    echo "consumer replay FAILED: refusing to install candidate sentinel ${version} from the registry" >&2
    return 1
  fi
  release="${CHAPTARR_REPLAY_RELEASE_VERSION:-}"
  release="${release#v}"
  if [[ -n "${release}" && "${version}" == "${release}" ]]; then
    echo "consumer replay FAILED: refusing to install release ${version} from the registry; it is published only after promotion" >&2
    return 1
  fi
}

use_cli_config() {
  local mode="$1" file="${artifact_dir}/tofurc-${1}"
  if [[ "${mode}" == "local" ]]; then
    # filesystem_mirror replaces the implicit direct method. The candidate
    # cannot fall through to the public registry.
    cat >"${file}" <<CFG
provider_installation {
  filesystem_mirror {
    path    = "${mirror_root}"
    include = ["registry.opentofu.org/josh-archer/chaptarr"]
  }
}
CFG
  else
    cat >"${file}" <<CFG
provider_installation {
  direct {}
}
CFG
  fi
  export TF_CLI_CONFIG_FILE="${file}"
}

installed_provider_bins() {
  local providers="${work_dir}/.terraform/providers"
  if [[ ! -d "${providers}" ]]; then
    return 0
  fi
  # An unpacked filesystem mirror is installed by symlinking the platform
  # directory (…/99.99.99/linux_amd64 -> the mirror). find without -L does not
  # descend into that link, so the built binary looks missing even after
  # tofu init records this platform's checksum in the lock file.
  find -L "${providers}" -type f -name 'terraform-provider-chaptarr*' ! -name '*.json' -print || true
}

assert_local_candidate() {
  local built installed matched
  built="${provider_target_dir}/terraform-provider-chaptarr"
  matched=false
  while IFS= read -r installed; do
    [[ -n "${installed}" ]] || continue
    if cmp -s "${built}" "${installed}"; then
      matched=true
    fi
  done < <(installed_provider_bins)
  if [[ "${matched}" != true ]]; then
    echo "consumer replay FAILED: candidate provider was not installed from the local build (${built})" >&2
    return 1
  fi
  echo "consumer replay OK: candidate provider is the local build" >&2
}

assert_registry_baseline() {
  local built installed found
  built="${provider_target_dir}/terraform-provider-chaptarr"
  found=false
  while IFS= read -r installed; do
    [[ -n "${installed}" ]] || continue
    found=true
    if cmp -s "${built}" "${installed}"; then
      echo "consumer replay FAILED: previous provider matches the candidate build; it was not installed from the registry" >&2
      return 1
    fi
  done < <(installed_provider_bins)
  if [[ "${found}" != true ]]; then
    echo "consumer replay FAILED: previous provider binary was not installed" >&2
    return 1
  fi
  echo "consumer replay OK: previous provider is the published registry build" >&2
}

scrub_log() {
  local file="$1"
  [[ -f "${file}" ]] || return 0
  if [[ -n "${api_key:-}" ]]; then
    sed -E "s#${api_key}#[REDACTED]#g; s#[0-9a-fA-F]{32,}#[REDACTED]#g" "${file}" >&2 || true
  else
    sed -E 's#[0-9a-fA-F]{32,}#[REDACTED]#g' "${file}" >&2 || true
  fi
  rm -f "${file}"
}

cleanup() {
  local prior_status=$?
  local cleanup_status=0
  trap - EXIT INT TERM

  if [[ "${local_env_created:-false}" == "true" && "${runtime:-}" == native ]]; then
    if [[ -n "${native_pid:-}" ]] && kill -0 "${native_pid}" 2>/dev/null; then
      kill "${native_pid}" 2>/dev/null || true
      for _ in $(seq 1 20); do kill -0 "${native_pid}" 2>/dev/null || break; sleep 1; done
      kill -9 "${native_pid}" 2>/dev/null || true
    fi
  elif [[ "${local_env_created:-false}" == "true" && "${runtime:-}" == docker && -n "${compose_file:-}" ]]; then
    if ! docker compose -f "${compose_file}" down --volumes --remove-orphans >/dev/null 2>&1; then
      echo "Disposable Chaptarr teardown failed." >&2
      cleanup_status=1
    fi
    if grep -q . <<<"$(docker ps -a --filter "label=com.docker.compose.project=${project_name}" --format '{{.ID}}')" ||
      grep -q . <<<"$(docker volume ls --filter "label=com.docker.compose.project=${project_name}" --quiet)" ||
      grep -q . <<<"$(docker network ls --filter "label=com.docker.compose.project=${project_name}" --quiet)"; then
      echo "Disposable Chaptarr project resources remain after teardown." >&2
      cleanup_status=1
    fi
  fi
  if [[ -n "${instance_dir:-}" ]]; then
    rm -rf -- "${instance_dir}" || true
    if [[ -e "${instance_dir}" ]]; then
      echo "Disposable consumer-replay state could not be removed." >&2
      cleanup_status=1
    fi
  fi

  if [[ ${prior_status} -ne 0 ]]; then
    exit "${prior_status}"
  fi
  exit "${cleanup_status}"
}

main() {
  repo_root="$(git rev-parse --show-toplevel)"
  cd "${repo_root}"
  if [[ -n "${CHAPTARR_URL:-}${CHAPTARR_API_KEY:-}" ]]; then
    echo "consumer replay: discarding ambient Chaptarr endpoint; only the disposable loopback instance is targeted" >&2
  fi
  unset CHAPTARR_URL CHAPTARR_API_KEY

  local fixture_src prev
  fixture_src="${repo_root}/test/consumer/arr-config"
  if [[ ! -f "${fixture_src}/main.tf" ]]; then
    echo "consumer replay FAILED: ${fixture_src}/main.tf is not in this commit" >&2
    exit 1
  fi
  if [[ "${CHAPTARR_IMAGE:-}" != *@sha256:* || -z "${CHAPTARR_VERSION:-}" ]]; then
    echo "consumer replay FAILED: set CHAPTARR_IMAGE to an immutable tag-and-digest image and CHAPTARR_VERSION to its expected version" >&2
    exit 1
  fi

  artifact_dir="${CHAPTARR_ARTIFACT_DIR:-${repo_root}/test-artifacts/consumer-replay}"
  mirror_root="${CHAPTARR_PROVIDER_MIRROR_ROOT:-${repo_root}/provider-mirror-consumer}"
  # Unpacked layout for the platform OpenTofu is running on. A binary placed
  # only under another platform directory is not installed, and init fails
  # closed instead of recording this platform in the lock file.
  provider_target_dir="${mirror_root}/registry.opentofu.org/josh-archer/chaptarr/${current_version}/$(go env GOOS)_$(go env GOARCH)"
  instance_dir="$(mktemp -d)"
  work_dir="${instance_dir}/work"
  api_key=""
  native_pid=""
  runtime="${CHAPTARR_RUNTIME:-}"
  if [[ -z "${runtime}" ]]; then
    if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
      runtime=docker
    else
      runtime=native
    fi
  fi
  case "${runtime}" in
    docker | native) ;;
    *) echo "CHAPTARR_RUNTIME must be docker or native." >&2; exit 1 ;;
  esac
  compose_file="${repo_root}/acceptance/compose.yaml"
  project_name="chaptarr-replay-${RANDOM}-$$"
  local_env_created=false

  run() {
    echo "+ $*" >&2
    "$@"
  }

  write_versions() {
    local version="$1"
    cat >"${work_dir}/versions.tf" <<VERSIONS
terraform {
  required_version = ">= 1.11.2"

  required_providers {
    chaptarr = {
      source  = "registry.opentofu.org/josh-archer/chaptarr"
      version = "${version}"
    }
  }
}
VERSIONS
  }

  init_with() {
    local mode="$1" version="$2"
    if [[ "${mode}" == "direct" ]]; then
      reject_registry_version "${version}"
    fi
    write_versions "${version}"
    use_cli_config "${mode}"
    rm -rf "${work_dir}/.terraform" "${work_dir}/.terraform.lock.hcl"
    (cd "${work_dir}" && run tofu init -input=false -no-color)
    if [[ "${mode}" == "local" ]]; then
      assert_local_candidate
    else
      assert_registry_baseline
    fi
  }

  tf() {
    local sub="$1" label="$2"
    shift 2
    local log="${instance_dir}/${label}.log" rc=0
    (cd "${work_dir}" && tofu "${sub}" -input=false -no-color "$@" >"${log}" 2>&1) || rc=$?
    if [[ ${rc} -ne 0 ]]; then
      echo "consumer replay FAILED: '${label}' exited ${rc}" >&2
      scrub_log "${log}"
      return 1
    fi
    rm -f "${log}"
    echo "consumer replay OK: ${label}" >&2
  }

  # Fails unless the plan is empty (exit 0). Exit 2 means drift or an inconsistent re-plan.
  assert_clean_plan() {
    local label="$1" rc=0
    local log="${instance_dir}/plan-${label// /-}.log"
    (cd "${work_dir}" && tofu plan -detailed-exitcode -input=false -no-color >"${log}" 2>&1) || rc=$?
    if [[ ${rc} -ne 0 ]]; then
      echo "consumer replay FAILED: '${label}' plan exited ${rc} (expected 0, no changes)" >&2
      scrub_log "${log}"
      return 1
    fi
    rm -f "${log}"
    echo "consumer replay OK: ${label} plan is clean" >&2
  }

  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM

  command -v tofu >/dev/null || { echo "tofu is required" >&2; exit 1; }
  command -v go >/dev/null || { echo "go is required" >&2; exit 1; }
  if [[ "${runtime}" == docker ]]; then
    command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }
  else
    command -v jq >/dev/null || { echo "jq is required for native Chaptarr" >&2; exit 1; }
  fi

  mkdir -p "${artifact_dir}" "${provider_target_dir}" "${work_dir}"
  cp -R "${fixture_src}/." "${work_dir}/"
  rm -f "${work_dir}/README.md"
  printf 'consumer replay started\n' >"${artifact_dir}/summary.txt"

  run go build -trimpath -o "${provider_target_dir}/terraform-provider-chaptarr" .

  echo "Starting disposable loopback-only Chaptarr ${CHAPTARR_VERSION} (${runtime})." >&2
  host_port=""
  export COMPOSE_PROJECT_NAME="${project_name}"
  export CHAPTARR_IMAGE
  local_env_created=true
  if [[ "${runtime}" == docker ]]; then
    run docker pull "${CHAPTARR_IMAGE}" >/dev/null
    docker compose -f "${compose_file}" config --quiet
    docker compose -f "${compose_file}" up -d >/dev/null
    api_key=""
    for _ in $(seq 1 90); do
      api_key="$(docker compose -f "${compose_file}" exec -T chaptarr sh -c "sed -n 's:.*<ApiKey>\\([^<]*\\)</ApiKey>.*:\\1:p' /config/config.xml 2>/dev/null" 2>/dev/null | tr -d '\r\n' || true)"
      if [[ -n "${api_key}" ]]; then
        break
      fi
      sleep 2
    done
    if [[ -z "${api_key}" ]]; then
      echo "consumer replay FAILED: Chaptarr did not create its disposable API credential in time." >&2
      exit 1
    fi
    local published
    published="$(docker compose -f "${compose_file}" port chaptarr 8789)"
    case "${published}" in
      127.0.0.1:*) ;;
      *)
        echo "consumer replay FAILED: disposable Chaptarr is not bound to loopback" >&2
        exit 1
        ;;
    esac
    host_port="${published##*:}"
    export TF_VAR_library_path="/audiobooks"
  else
    # shellcheck source=scripts/lib/native-chaptarr.sh
    source "${repo_root}/scripts/lib/native-chaptarr.sh"
    native_start "${CHAPTARR_IMAGE}" "${instance_dir}/native"
    mkdir -p "${instance_dir}/library"
    export TF_VAR_library_path="${instance_dir}/library"
  fi
  base_url="http://127.0.0.1:${host_port}"
  case "${base_url}" in
    http://127.0.0.1:*) ;;
    *)
      echo "consumer replay FAILED: refusing non-loopback target" >&2
      exit 1
      ;;
  esac
  for _ in $(seq 1 90); do
    if curl --silent --show-error --fail --output /dev/null -H "X-Api-Key: ${api_key}" "${base_url}/api/v1/system/status" 2>/dev/null; then
      break
    fi
    sleep 2
  done
  if ! curl --silent --show-error --fail --output /dev/null -H "X-Api-Key: ${api_key}" "${base_url}/api/v1/system/status" 2>/dev/null; then
    echo "consumer replay FAILED: Chaptarr did not become ready in time." >&2
    exit 1
  fi

  export TF_VAR_chaptarr_url="${base_url}"
  export TF_VAR_chaptarr_api_key="${api_key}"
  export TF_IN_AUTOMATION=1
  unset CHAPTARR_URL CHAPTARR_API_KEY

  echo "== Phase 1: fresh apply with local candidate build, then clean re-plan" >&2
  init_with local "${current_version}"
  tf apply fresh-apply -auto-approve
  assert_clean_plan "fresh apply (current provider)"
  printf 'phase 1 clean\n' >>"${artifact_dir}/summary.txt"

  if [[ "${CHAPTARR_REPLAY_SKIP_UPGRADE:-false}" == "true" ]]; then
    exit 0
  fi

  echo "== Phase 2: previous published release, upgrade to local candidate, clean re-plan" >&2
  if ! prev="$(previous_version)"; then
    exit 1
  fi
  echo "Previous released version: ${prev}" >&2
  printf 'previous %s\n' "${prev}" >>"${artifact_dir}/summary.txt"
  tf destroy destroy-before-upgrade -auto-approve
  init_with direct "${prev}"
  tf apply previous-apply -auto-approve
  init_with local "${current_version}"
  assert_clean_plan "after provider upgrade ${prev} -> current (no apply)"
  tf apply upgrade-apply -auto-approve
  assert_clean_plan "apply after provider upgrade"
  printf 'phase 2 clean\n' >>"${artifact_dir}/summary.txt"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
