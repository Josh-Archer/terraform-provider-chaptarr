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
tofu fmt -check -recursive examples
bash ./scripts/test-plan-leak.sh
bash ./scripts/test-release-assets.sh
git diff --check
