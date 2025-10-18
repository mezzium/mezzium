.PHONY: buildx push run build test build-all build-platform build-go \
	build-linux-amd64 build-linux-arm64 build-mac-amd64 build-mac-arm64 \
	generate-evm-networks build-evm-networks

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


# Default platforms list; can be overridden when calling:
# make build-all PLATFORMS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"
PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# Cross-build variables
CGO_ENABLED ?= 0
LDFLAGS := -X 'main.Version=$(VERSION)' -X 'main.CommitHash=$(COMMIT_HASH)'

# Standard target: build binary for the current host configuration (now uses LDFLAGS)
build:
	@echo " Build local [commit: $(COMMIT_HASH)]"
	CGO_ENABLED=$(CGO_ENABLED) go build -ldflags "$(LDFLAGS)" -o mezzium ./cmd/app

# build-go: build for a specific GOOS/GOARCH, outputs to build/<platform>/mezzium
# Usage: make build-go GOOS=linux GOARCH=arm64
build-go:
	@if [ -z "$(GOOS)" ] || [ -z "$(GOARCH)" ]; then \
	echo "Specify GOOS and GOARCH, e.g. make build-go GOOS=linux GOARCH=arm64"; exit 1; \
	fi
	@PLAT="$$(echo $(GOOS)/$(GOARCH))"; \
	echo "Cross-compiling for $$PLAT [commit: $(COMMIT_HASH)]"; \
	outdir=build/$$(echo $$PLAT | tr '/' '-'); \
	mkdir -p $$outdir; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o $$outdir/mezzium ./cmd/app; \
	echo "Wrote $$outdir/mezzium"

# build-platform: build for a platform in os/arch format, outputs to build/<platform>/mezzium
# Usage: make build-platform PLATFORM=linux/arm64
build-platform:
	@if [ -z "$(PLATFORM)" ]; then \
	echo "Specify PLATFORM, e.g. make build-platform PLATFORM=linux/arm64"; exit 1; \
	fi
	@OS="$$(echo $(PLATFORM) | cut -d'/' -f1)"; \
	ARCH="$$(echo $(PLATFORM) | cut -d'/' -f2)"; \
	outdir=build/$$(echo $(PLATFORM) | tr '/' '-'); \
	echo "Cross-compiling for $(PLATFORM) -> $$outdir [commit: $(COMMIT_HASH)]"; \
	mkdir -p $$outdir; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$$OS GOARCH=$$ARCH go build -ldflags "$(LDFLAGS)" -o $$outdir/mezzium ./cmd/app; \
	echo "Wrote $$outdir/mezzium"

# build-all: build for all platforms defined in PLATFORMS
# Usage: make build-all
build-all:
	@for p in $(PLATFORMS); do \
	echo "-> building $$p"; \
	$(MAKE) build-platform PLATFORM=$$p || exit 1; \
	done

# Preset convenience targets
build-linux-amd64:
	@$(MAKE) build-platform PLATFORM=linux/amd64

build-linux-arm64:
	@$(MAKE) build-platform PLATFORM=linux/arm64

build-mac-amd64:
	@$(MAKE) build-platform PLATFORM=darwin/amd64

build-mac-arm64:
	@$(MAKE) build-platform PLATFORM=darwin/arm64

# Generate EVM networks
generate-evm-networks:
	@echo "Generating EVM networks into configs/networks"
	@mkdir -p configs/networks
	@bash -c '\
	curl -s https://chainlist.org/rpcs.json | \
	jq -c '"'"'.[] | select(.chainSlug != null and .rpc != null)'"'"' | \
	while read -r obj; do \
	slug=$$(echo "$$obj" | jq -r ".chainSlug" | tr "[:upper:]" "[:lower:]" | sed "s/ /-/g"); \
	chainId=$$(echo "$$obj" | jq -r ".chainId"); \
	echo "$$obj" | jq --arg route "/$$slug" --arg protocol "evm" --argjson chainId "$$chainId" '"'"'{ \
	route: $route, \
	protocol: $protocol, \
	chainId: $chainId, \
	timeoutMs: 1500, \
	gas: { externalUrl: "", minPriorityGwei: 2, ttlSeconds: 10 }, \
	nodes: (.rpc | map(select(.url | test("^https://")) | {url: .url, priority: 1})) \
	}'"'"' | yq -P > "configs/networks/$$slug.yaml"; \
	done'
	@echo "Wrote files into configs/networks"

# convenience alias (keeps naming consistent with other build targets)
build-evm-networks: generate-evm-networks


test:
	@echo " Running test [commit: $(COMMIT_HASH)]"
	go test ./...
