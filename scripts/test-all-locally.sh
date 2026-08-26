#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.13+auto}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

unformatted="$(gofmt -l main.go internal tools)"
if [[ -n "${unformatted}" ]]; then
  echo "Go files require gofmt:" >&2
  echo "${unformatted}" >&2
  exit 1
fi

go mod verify
go vet ./...
go test ./...
go run ./tools/openapi check
go run ./tools/compatibility check
tofu fmt -check -recursive examples acceptance
CHAPTARR_IMAGE='chaptarr/chaptarr:0.9.929@sha256:2f5409fad4b02386fdd57169d93f7533342eafd036357a2c2b7256df19cda7eb' \
  docker compose -f acceptance/compose.yaml config --quiet
bash ./scripts/test-plan-leak.sh
bash ./scripts/test-release-assets.sh
git diff --check
