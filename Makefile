.PHONY: buildx push run build test

VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.0.1")
COMMIT_HASH := $(shell git rev-parse --short HEAD)
IMAGE_NAME := shuliakovsky/mezzium:$(VERSION)

push:
	@echo " Building and pushing $(IMAGE_NAME) [commit: $(COMMIT_HASH)]"
	docker buildx build --no-cache \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_HASH=$(COMMIT_HASH) \
		-t $(IMAGE_NAME) \
		--push .

buildx:
	@echo " Building $(IMAGE_NAME) [commit: $(COMMIT_HASH)]"
	docker buildx build --no-cache \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_HASH=$(COMMIT_HASH) \
		-t $(IMAGE_NAME) \
		--load .

run:
	@echo " Running $(IMAGE_NAME) [commit: $(COMMIT_HASH)]"
	docker compose build --no-cache \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_HASH=$(COMMIT_HASH)
	docker compose up --detach

build:
	@echo " Build local [commit: $(COMMIT_HASH)]"
	go build -ldflags "-X main.Version=${VERSION} -X main.CommitHash=${COMMIT_HASH}" -o mezzium ./cmd/app

test:
	@echo " Running test [commit: $(COMMIT_HASH)]"
	go test ./...
