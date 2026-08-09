.PHONY: fmt generate check test race plan-smoke snapshot

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './third_party/*')
	tofu fmt -recursive examples

generate:
	go run ./tools/openapi generate

check:
	go run ./tools/openapi check

test:
	bash ./scripts/test-all-locally.sh

race:
	go test -race ./...

plan-smoke:
	bash ./scripts/test-plan-leak.sh

snapshot:
	goreleaser release --snapshot --clean --skip=sign --parallelism=2
