#!/usr/bin/env bash
# Chaptarr v0.9.929 populates /health asynchronously after /system/status is up.
# Complete explicit CheckHealth commands before Terraform captures its baseline.
# This only runs against the harness-created disposable loopback instance.
wait_for_replay_health() {
  local url="$1" key="$2" command_id status summary previous='' round poll
  [[ "$url" =~ ^http://127\.0\.0\.1:[0-9]+$ ]] || {
    echo 'consumer replay FAILED: health check requires disposable loopback URL' >&2; return 1;
  }
  for round in 1 2; do
    command_id="$(curl --silent --show-error --fail --max-time 30 \
      -H "X-Api-Key: ${key}" -H 'Content-Type: application/json' \
      --data '{"name":"CheckHealth"}' "${url}/api/v1/command" | jq -er '.id | select(type == "number" and . > 0 and . == floor)')" || return 1
    status=''
    for poll in $(seq 1 60); do
      status="$(curl --silent --show-error --fail --max-time 10 \
        -H "X-Api-Key: ${key}" "${url}/api/v1/command/${command_id}" | jq -er '.status')" || return 1
      case "$status" in
        completed) break ;;
        queued|started) sleep 2 ;;
        *) echo 'consumer replay FAILED: health command did not complete successfully' >&2; return 1 ;;
      esac
    done
    [[ "$status" == completed ]] || { echo 'consumer replay FAILED: health command timed out' >&2; return 1; }
    # Match the provider's secret-free count contract; never log raw health text.
    summary="$(curl --silent --show-error --fail --max-time 10 \
      -H "X-Api-Key: ${key}" "${url}/api/v1/health" | jq -ce '
        if type != "array" then error("invalid health response") else
          {errors: map(select((.type | ascii_downcase) == "error")) | length,
           warnings: map(select((.type | ascii_downcase) as $t | $t == "warning" or $t == "notice")) | length}
        end')" || return 1
    if [[ "$round" == 2 && "$summary" != "$previous" ]]; then
      echo 'consumer replay FAILED: health summary did not converge before baseline' >&2; return 1
    fi
    previous="$summary"
  done
  echo 'consumer replay OK: health checks completed with a stable summary' >&2
}
