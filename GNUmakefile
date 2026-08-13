.PHONY: fmt generate check test race plan-smoke acceptance snapshot

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './third_party/*')
	tofu fmt -recursive examples acceptance

generate:
	go run ./tools/openapi generate
	go run ./tools/compatibility generate

check:
	go run ./tools/openapi check
	go run ./tools/compatibility check

test:
	bash ./scripts/test-all-locally.sh

race:
	go test -race ./...

plan-smoke:
	bash ./scripts/test-plan-leak.sh

acceptance:
	bash ./scripts/test-acceptance.sh

snapshot:
	goreleaser release --snapshot --clean --skip=sign --parallelism=2
