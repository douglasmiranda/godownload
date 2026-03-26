APP_NAME := godownload
MAIN_PKG := ./cmd/godownload
DIST_DIR := dist
DOCKERFILE := Dockerfile

IMAGE ?= godownload
TAG ?= latest

GOOS_HOST := $(shell go env GOOS)
GOARCH_HOST := $(shell go env GOARCH)
HOST_BIN := $(DIST_DIR)/$(APP_NAME)-$(GOOS_HOST)-$(GOARCH_HOST)
DEFAULT_BIN := $(DIST_DIR)/$(APP_NAME)

UNAME_M := $(shell uname -m)
ifeq ($(UNAME_M),x86_64)
HOST_DOCKER_ARCH := amd64
else ifeq ($(UNAME_M),aarch64)
HOST_DOCKER_ARCH := arm64
else ifeq ($(UNAME_M),arm64)
HOST_DOCKER_ARCH := arm64
else
HOST_DOCKER_ARCH := amd64
endif
HOST_DOCKER_PLATFORM := linux/$(HOST_DOCKER_ARCH)

DOCKER_BIN_PLATFORMS ?= $(HOST_DOCKER_PLATFORM),linux/arm64
DOCKER_IMAGE_PLATFORMS ?= linux/amd64,linux/arm64
COMMA := ,

.PHONY: help test clean build build-host docker-binary docker-binaries docker-image docker-image-release

help:
	@echo "Targets:"
	@echo "  make test                    # Run tests"
	@echo "  make build                   # Build host binary to dist/"
	@echo "  make clean                   # Remove dist/"
	@echo "  make docker-binary           # Build host-platform binary via Docker buildx to dist/docker/<platform>/"
	@echo "  make docker-binaries         # Build multiple platform binaries via Docker buildx"
	@echo "  make docker-image            # Build runtime image for host platform and load locally"
	@echo "  make docker-image-release    # Build and push amd64/arm64 tags + manifest tag"

test:
	go test ./...

$(DIST_DIR):
	mkdir -p $(DIST_DIR)

build: $(DIST_DIR)
	go build -trimpath -ldflags="-s -w" -o $(HOST_BIN) $(MAIN_PKG)
	cp $(HOST_BIN) $(DEFAULT_BIN)
	@echo "Built: $(HOST_BIN)"
	@echo "Default binary: $(DEFAULT_BIN)"

clean:
	rm -rf $(DIST_DIR)

docker-binary: $(DIST_DIR)
	mkdir -p $(DIST_DIR)/docker/$(subst /,_,$(HOST_DOCKER_PLATFORM))
	docker buildx build \
		--file $(DOCKERFILE) \
		--target binary \
		--platform $(HOST_DOCKER_PLATFORM) \
		--output type=local,dest=$(DIST_DIR)/docker/$(subst /,_,$(HOST_DOCKER_PLATFORM)) \
		.
	@echo "Built binary via Docker: $(DIST_DIR)/docker/$(subst /,_,$(HOST_DOCKER_PLATFORM))/godownload"

docker-binaries: $(DIST_DIR)
	@set -e; \
	for p in $(subst $(COMMA), ,$(DOCKER_BIN_PLATFORMS)); do \
		safe=$$(printf '%s' "$$p" | tr '/' '_'); \
		mkdir -p $(DIST_DIR)/docker/$$safe; \
		echo "Building $$p -> $(DIST_DIR)/docker/$$safe"; \
		docker buildx build \
			--file $(DOCKERFILE) \
			--target binary \
			--platform $$p \
			--output type=local,dest=$(DIST_DIR)/docker/$$safe \
			.; \
	done

docker-image:
	docker buildx build \
		--file $(DOCKERFILE) \
		--platform $(HOST_DOCKER_PLATFORM) \
		--target runtime \
		--tag $(IMAGE):$(TAG) \
		--load \
		.
	@echo "Loaded image: $(IMAGE):$(TAG)"

docker-image-release:
	docker buildx build \
		--file $(DOCKERFILE) \
		--platform linux/amd64 \
		--target runtime \
		--tag $(IMAGE):$(TAG)-amd64 \
		--push \
		.
	docker buildx build \
		--file $(DOCKERFILE) \
		--platform linux/arm64 \
		--target runtime \
		--tag $(IMAGE):$(TAG)-arm64 \
		--push \
		.
	docker buildx imagetools create \
		--tag $(IMAGE):$(TAG) \
		$(IMAGE):$(TAG)-amd64 \
		$(IMAGE):$(TAG)-arm64
	@echo "Pushed tags: $(IMAGE):$(TAG)-amd64, $(IMAGE):$(TAG)-arm64, $(IMAGE):$(TAG)"
