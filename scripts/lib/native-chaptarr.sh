#!/usr/bin/env bash
# Purpose: run a digest-pinned Chaptarr image as a plain loopback-only process
# on runners without Docker. Sourced by scripts/test-acceptance.sh.
# Sets: native_pid, host_port, api_key.

native_start() {
  local image="$1" work="$2"
  local repo="${image%%:*}" digest="${image##*@}"
  [[ "${repo}" == */* ]] || repo="library/${repo}"
  repo="${repo#docker.io/}"
  local arch
  case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "Unsupported architecture for native Chaptarr: $(uname -m)" >&2; return 1 ;;
  esac
  local accept='application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json,application/vnd.oci.image.manifest.v1+json,application/vnd.docker.distribution.manifest.v2+json'
  local registry=https://registry-1.docker.io/v2 token index manifest layer
  token="$(curl -fsS "https://auth.docker.io/token?service=registry.docker.io&scope=repository:${repo}:pull" | jq -r .token)"
  mkdir -p "${work}/root" "${work}/data"
  index="$(curl -fsS -H "Authorization: Bearer ${token}" -H "Accept: ${accept}" "${registry}/${repo}/manifests/${digest}")"
  if [[ "$(printf '%s' "${index}" | sha256sum | cut -d' ' -f1)" != "${digest#sha256:}" ]]; then
    echo "Image index digest mismatch for ${image}." >&2; return 1
  fi
  manifest="$(printf '%s' "${index}" | jq -r --arg a "${arch}" '.manifests[]? | select(.platform.os=="linux" and .platform.architecture==$a) | .digest' | head -n1)"
  if [[ -n "${manifest}" ]]; then
    index="$(curl -fsS -H "Authorization: Bearer ${token}" -H "Accept: ${accept}" "${registry}/${repo}/manifests/${manifest}")"
    [[ "$(printf '%s' "${index}" | sha256sum | cut -d' ' -f1)" == "${manifest#sha256:}" ]] || { echo "Platform manifest digest mismatch." >&2; return 1; }
  fi
  for layer in $(printf '%s' "${index}" | jq -r '.layers[].digest'); do
    curl -fsSL -H "Authorization: Bearer ${token}" -o "${work}/layer" "${registry}/${repo}/blobs/${layer}"
    [[ "$(sha256sum "${work}/layer" | cut -d' ' -f1)" == "${layer#sha256:}" ]] || { echo "Layer digest mismatch." >&2; return 1; }
    tar -xzf "${work}/layer" -C "${work}/root" --no-same-owner --no-same-permissions --exclude='dev/*' 2>/dev/null || true
    rm -f "${work}/layer"
  done
  [[ -x "${work}/root/usr/share/dotnet/dotnet" && -f "${work}/root/app/Chaptarr.dll" ]] || { echo "Unexpected Chaptarr image layout." >&2; return 1; }

  host_port=""
  local candidate
  for _ in $(seq 1 50); do
    candidate=$((20000 + RANDOM % 20000))
    if ! (exec 3<>"/dev/tcp/127.0.0.1/${candidate}") 2>/dev/null; then host_port="${candidate}"; break; fi
  done
  [[ -n "${host_port}" ]] || { echo "No free loopback port." >&2; return 1; }
  cat >"${work}/data/config.xml" <<XML
<Config>
  <BindAddress>127.0.0.1</BindAddress>
  <Port>${host_port}</Port>
  <AnalyticsEnabled>False</AnalyticsEnabled>
  <UpdateMechanism>External</UpdateMechanism>
</Config>
XML
  DOTNET_ROOT="${work}/root/usr/share/dotnet" DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 DOTNET_CLI_TELEMETRY_OPTOUT=1 TZ=UTC \
    "${work}/root/usr/share/dotnet/dotnet" "${work}/root/app/Chaptarr.dll" -nobrowser "-data=${work}/data" >"${work}/chaptarr.log" 2>&1 &
  native_pid=$!

  # Chaptarr writes <ApiKey> during startup, after its main + log DB
  # migrations. Locally that takes ~15s, but on uecb-shared (where setup-go's
  # cache save alone ran >6 min) the process was still alive when the old fixed
  # 90x2s window expired. Wait on a deadline while the process is alive, and on failure show
  # a redacted log tail so the cause is visible in CI instead of guessed at.
  local deadline=$((SECONDS + ${CHAPTARR_STARTUP_TIMEOUT:-600}))
  api_key=""
  while ((SECONDS < deadline)); do
    kill -0 "${native_pid}" 2>/dev/null || { echo "Native Chaptarr exited early." >&2; native_log_tail "${work}"; return 1; }
    api_key="$(sed -n 's:.*<ApiKey>\([^<]*\)</ApiKey>.*:\1:p' "${work}/data/config.xml" 2>/dev/null | tr -d '\r\n' || true)"
    [[ -n "${api_key}" ]] && break
    sleep 2
  done
  if [[ -z "${api_key}" ]]; then
    echo "Chaptarr did not create its disposable API credential in time." >&2
    native_log_tail "${work}"
    return 1
  fi
}

# Print the last Chaptarr log lines with anything key-like redacted.
native_log_tail() {
  local log="$1/chaptarr.log"
  [[ -f "${log}" ]] || return 0
  echo "--- last 40 lines of native Chaptarr log (redacted) ---" >&2
  tail -n 40 "${log}" | sed -E 's/[0-9a-fA-F]{32,}/[REDACTED]/g; s/(api[_-]?key|apikey|token|password|secret)([^A-Za-z0-9]+)[^ ]+/\1\2[REDACTED]/Ig' >&2
}
