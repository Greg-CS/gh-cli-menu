.PHONY: build run test clean install fmt vet

BINARY_NAME := gh-gum
BUILD_FLAGS := -ldflags="-s -w"

build:
	go build $(BUILD_FLAGS) -o $(BINARY_NAME) .

run: build
	.\$(BINARY_NAME)

test:
	go test -race -cover ./...

clean:
	go clean
	if exist $(BINARY_NAME) del $(BINARY_NAME)

install: build
	go install .

fmt:
	go fmt ./...

vet:
	go vet ./...

check: fmt vet test
