#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.25.12+auto}"

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
CHAPTARR_IMAGE='chaptarr/chaptarr:0.9.925@sha256:8e29f4941acaf74c80bba4322237dfd2549816b3dd1b581f176b1be5d1ccb46b' \
  docker compose -f acceptance/compose.yaml config --quiet
bash ./scripts/test-plan-leak.sh
bash ./scripts/test-release-assets.sh
git diff --check
