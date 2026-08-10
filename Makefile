# Makefile
# ---------------- Project ----------------
BINARY_NAME ?= rttys
UI_DIR      ?= ui
GO_MAIN     ?= ./cmd/glkvm-cloud

# Go build flags
BUILD_FLAGS ?= -ldflags "-s -w"
DIST_DIR    ?= dist

# Image name
IMAGE_NAME  ?= glkvm-cloud
IMAGE_TAG   ?= build

GOARCH ?= $(shell go env GOARCH)

# ---------------- Commands ----------------
.PHONY: all ui build debug-local debug-dev-server \
        build-linux-amd64 build-linux-arm64 build-linux-all \
        docker-buildx docker-buildx-full

all: build-linux-amd64 build-linux-arm64

# Build frontend files only.
#
# yarn, not npm: the lockfile here is yarn.lock, and package.json carries a
# `resolutions` block that only yarn honours -- it is what holds minimatch and
# brace-expansion on patched majors that eslint and typescript-estree would
# otherwise pull in below. Under `npm install` that block is silently ignored
# and the lockfile is not consulted at all, so the build is neither pinned nor
# the one the alerts were cleared against.
ui:
	cd $(UI_DIR) && yarn install --frozen-lockfile && yarn build

# ---------------- Cross compile (generic) ----------------
# One target for any GOOS/GOARCH, so CI's build matrix and the release job
# share these flags rather than each carrying their own copy.
#
#   make build BUILD_OS=windows BUILD_ARCH=amd64
#
# Produces dist/rttys-<os>-<arch>, with .exe appended for windows.
BUILD_OS   ?= $(shell go env GOOS)
BUILD_ARCH ?= $(shell go env GOARCH)
BUILD_EXT   = $(if $(filter windows,$(BUILD_OS)),.exe,)

build:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=$(BUILD_OS) GOARCH=$(BUILD_ARCH) \
		go build $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-$(BUILD_OS)-$(BUILD_ARCH)$(BUILD_EXT) $(GO_MAIN)

# ---------------- Cross compile (Linux) ----------------
# Produce: dist/rttys-linux-amd64 , dist/rttys-linux-arm64
build-linux-amd64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 $(GO_MAIN)

build-linux-arm64:
	@mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build $(BUILD_FLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 $(GO_MAIN)

# ---------------- Docker Buildx ----------------
# Multi-arch build
# Usage:
#   make docker-buildx GOARCH=amd64 IMAGE_TAG=build-amd64
#   make docker-buildx GOARCH=arm64 IMAGE_TAG=build-arm64
REGISTRY  ?=

# If REGISTRY is set, tag becomes: REGISTRY/IMAGE_NAME:IMAGE_TAG
ifdef REGISTRY
  IMAGE_REF := $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)
else
  IMAGE_REF := $(IMAGE_NAME):$(IMAGE_TAG)
endif

docker-buildx:
	@docker buildx version >/dev/null 2>&1 || (echo "docker buildx not available" && exit 1)
	@echo "==> buildx (load local image): $(IMAGE_REF) [linux/$(GOARCH)]"
	docker buildx build \
		--platform linux/$(GOARCH) \
		-t $(IMAGE_REF) \
		--load .

docker-buildx-full: ui
	@$(MAKE) docker-buildx


DEBUG_HOST  ?= root@xxxxxxxxxx
DEBUG_PATH  ?= /root/glkvmcloudbuild.tar
# Local debug bundle (amd64 image + save tar), upload to debug host, then load
debug-local: build-linux-amd64 docker-buildx
	docker save $(IMAGE_NAME):$(IMAGE_TAG) -o glkvmcloudbuild.tar
	ssh $(DEBUG_HOST) "rm -f $(DEBUG_PATH)"
	scp glkvmcloudbuild.tar $(DEBUG_HOST):$(DEBUG_PATH)
	ssh $(DEBUG_HOST) "docker load < $(DEBUG_PATH)"
	ssh $(DEBUG_HOST) "cd /root/glkvm_cloud && docker-compose down && docker-compose up -d"

# ---------------- Dev server debug ----------------
# Set DEBUG_DEV_SERVER_IP via environment variable, e.g.:
#   export DEBUG_DEV_SERVER_IP=1.2.3.4
#   make debug-dev-server
DEBUG_DEV_SERVER_IP   ?= $(error DEBUG_DEV_SERVER_IP is not set)
DEBUG_DEV_SERVER_USER ?= ubuntu
DEBUG_DEV_SERVER_HOST  = $(DEBUG_DEV_SERVER_USER)@$(DEBUG_DEV_SERVER_IP)
DEBUG_DEV_SERVER_PATH ?= /home/$(DEBUG_DEV_SERVER_USER)/glkvmcloudbuild.tar
DEBUG_DEV_SERVER_DIR  ?= /home/$(DEBUG_DEV_SERVER_USER)/glkvm_cloud
debug-dev-server: build-linux-amd64 docker-buildx
	docker save $(IMAGE_NAME):$(IMAGE_TAG) -o glkvmcloudbuild.tar
	ssh $(DEBUG_DEV_SERVER_HOST) "rm -f $(DEBUG_DEV_SERVER_PATH)"
	scp glkvmcloudbuild.tar $(DEBUG_DEV_SERVER_HOST):$(DEBUG_DEV_SERVER_PATH)
	ssh $(DEBUG_DEV_SERVER_HOST) "sudo docker load < $(DEBUG_DEV_SERVER_PATH)"
	ssh $(DEBUG_DEV_SERVER_HOST) "cd $(DEBUG_DEV_SERVER_DIR) && sudo docker-compose down && sudo docker-compose up -d"
