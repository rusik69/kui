GOLANGCI_LINT_VERSION := v1.64.8

.PHONY: build run test vet fmt fmt-fix lint tidy clean ci

build:
	go build -o bin/kui ./cmd/kui

run:
	go run ./cmd/kui

test:
	go test ./... -race -coverprofile=coverage.out -covermode=atomic

vet:
	go vet ./...

fmt:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needs to be run on:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

fmt-fix:
	gofmt -w .

lint:
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

tidy:
	go mod tidy

clean:
	rm -rf bin/

ci: fmt vet lint test build