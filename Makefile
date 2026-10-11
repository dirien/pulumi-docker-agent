PROJECT_NAME     := Pulumi Docker Agent Provider

SHELL            := /bin/bash
PACK             := docker-agent
GOPKG            := dockeragent
ORG              := dirien
PROJECT          := github.com/$(ORG)/pulumi-$(PACK)
PROVIDER         := pulumi-resource-$(PACK)
PROVIDER_PATH    := provider
# The provider is its own Go module (github.com/dirien/pulumi-docker-agent/provider); the version
# variable lives in its root package.
VERSION_PATH     := $(PROJECT)/$(PROVIDER_PATH).Version
SCHEMA_FILE      := $(PROVIDER_PATH)/cmd/$(PROVIDER)/schema.json
WORKING_DIR      := $(shell pwd)
TESTPARALLELISM  := 4
PULUMI           := pulumi

# Exact tag on a tagged commit (e.g. 0.1.0), a pre-release otherwise. Override in CI:
#   make build_nodejs PROVIDER_VERSION=0.1.0
#
# Build metadata ("+...", e.g. the commit pulumictl appends) is dropped everywhere: PyPI cannot
# carry it (0.0.1-alpha.0+dev becomes 0.0.1a0), so the SDKs would ask Pulumi for a different
# plugin version than `make install_plugin` installs, and `pulumi install` would try to download it.
PROVIDER_VERSION ?= $(shell pulumictl get version --language generic 2>/dev/null || echo 0.0.1-alpha.0)
VERSION_GENERIC   = $(shell pulumictl convert-version --language generic --version "$(PROVIDER_VERSION)" | sed 's/+.*//')
VERSION_PYTHON    = $(shell pulumictl convert-version --language python  --version "$(PROVIDER_VERSION)")
VERSION_DOTNET    = $(shell pulumictl convert-version --language dotnet  --version "$(PROVIDER_VERSION)" | sed 's/+.*//')
LDFLAGS           = -s -w -X $(VERSION_PATH)=$(VERSION_GENERIC)

.PHONY: help ensure provider provider_debug schema codegen generate_sdks generate_config_types check_config_types \
        upgrade_docker_agent check_docker_agent_upgrade \
        generate_nodejs generate_python generate_dotnet generate_go generate_java \
        build build_sdks build_nodejs build_python build_dotnet build_go build_java \
        install install_plugin install_nodejs_sdk install_dotnet_sdk \
        test test_provider test_compat test_examples lint goreleaser_check snapshot clean

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

ensure: ## go mod tidy for all modules
	cd $(PROVIDER_PATH) && go mod tidy
	cd sdk && go mod tidy

# ---------------------------------------------------------------- provider
provider: ## Build bin/pulumi-resource-docker-agent
	cd $(PROVIDER_PATH) && go build -o $(WORKING_DIR)/bin/$(PROVIDER) -ldflags "$(LDFLAGS)" ./cmd/$(PROVIDER)

provider_debug: ## Build the provider without optimizations (for debuggers)
	cd $(PROVIDER_PATH) && go build -o $(WORKING_DIR)/bin/$(PROVIDER) -gcflags="all=-N -l" -ldflags "-X $(VERSION_PATH)=$(VERSION_GENERIC)" ./cmd/$(PROVIDER)

schema: provider ## Regenerate the committed schema.json (version stripped)
	$(PULUMI) package get-schema $(WORKING_DIR)/bin/$(PROVIDER) | jq 'del(.version)' > $(SCHEMA_FILE)

# ---------------------------------------------------------------- codegen (committed, version-agnostic)
# SDKs are generated WITHOUT --version so the committed tree never changes between releases
# (same model as dirien/pulumi-fly). The version is injected only into build outputs (bin/, gitignored).
codegen: generate_config_types schema generate_sdks ## Typed config inputs + schema.json + all SDK sources

generate_sdks: generate_nodejs generate_python generate_dotnet generate_go generate_java ## Generate all SDK sources from schema.json

generate_nodejs generate_python generate_dotnet generate_java: generate_%:
	rm -rf sdk/$*
	$(PULUMI) package gen-sdk --language $* $(SCHEMA_FILE) --out sdk

generate_go:
	rm -rf sdk/go
	$(PULUMI) package gen-sdk --language go $(SCHEMA_FILE) --out sdk
	cd sdk && go mod tidy

# ---------------------------------------------------------------- docker-agent config types and upgrades
# The typed AgentConfig inputs (provider/pkg/render/config_types.gen.go) and the version, toolset
# and provider constants are generated from agent-schema.json of the docker-agent module pinned in
# provider/go.mod, by provider/internal/schemagen. The generator also vendors that schema and the
# upstream example configurations, which the coverage and round-trip tests read.
GENERATED_CONFIG := $(PROVIDER_PATH)/pkg/render/config_types.gen.go $(PROVIDER_PATH)/pkg/render/version.gen.go \
	$(PROVIDER_PATH)/pkg/render/schema $(PROVIDER_PATH)/pkg/render/testdata/upstream \
	$(PROVIDER_PATH)/pkg/validate/upstream.gen.go
DOCKER_AGENT_VERSION ?=

generate_config_types: ## Regenerate the typed AgentConfig inputs from the pinned docker-agent schema
	cd $(PROVIDER_PATH) && go run ./internal/schemagen generate

check_config_types: generate_config_types ## Fail if the generated config types differ from provider/go.mod's docker-agent
	@git update-index -q --refresh; \
	if [ -n "$$(git status --porcelain -- $(GENERATED_CONFIG))" ]; then \
		echo "::error title=Stale config types::the generated config types do not match the docker-agent version in provider/go.mod. Run 'make generate_config_types' (or 'make upgrade_docker_agent') and commit the result."; \
		git status --porcelain -- $(GENERATED_CONFIG); \
		git --no-pager diff --stat -- $(GENERATED_CONFIG); \
		exit 1; \
	fi

upgrade_docker_agent: ## Upgrade docker-agent to DOCKER_AGENT_VERSION (default: latest release), regenerate, test, report
	DOCKER_AGENT_VERSION="$(DOCKER_AGENT_VERSION)" ./scripts/upgrade-docker-agent.sh

check_docker_agent_upgrade: ## Print whether a newer docker-agent release than provider/go.mod's exists
	DOCKER_AGENT_VERSION="$(DOCKER_AGENT_VERSION)" ./scripts/upgrade-docker-agent.sh --check

# ---------------------------------------------------------------- build (version injected)
build: provider build_sdks ## Provider + all SDKs
build_sdks: build_nodejs build_python build_dotnet build_go ## Build the nodejs, python, dotnet and go SDKs

build_nodejs: generate_nodejs ## -> sdk/nodejs/bin (npm publish dir)
	cd sdk/nodejs && \
		yarn install && \
		yarn run tsc && \
		cp ../../README.md ../../LICENSE package.json yarn.lock ./bin/ && \
		sed -i.bak -e 's/\$${VERSION}/$(VERSION_GENERIC)/g' ./bin/package.json && rm ./bin/package.json.bak

build_python: generate_python ## -> sdk/python/bin/dist (wheel + sdist)
	cd sdk/python && \
		cp ../../README.md . && \
		rm -rf ./bin/ ../python.bin/ && cp -R . ../python.bin && mv ../python.bin ./bin && \
		sed -i.bak -e 's/^  version = .*/  version = "$(VERSION_PYTHON)"/' ./bin/pyproject.toml && rm ./bin/pyproject.toml.bak && \
		python3 -m venv venv && ./venv/bin/python -m pip install -q build && \
		cd ./bin && ../venv/bin/python -m build .

build_dotnet: generate_dotnet ## -> sdk/dotnet/bin/Debug/*.nupkg
	cd sdk/dotnet && \
		echo "$(VERSION_DOTNET)" > version.txt && \
		dotnet build /p:Version=$(VERSION_DOTNET)

build_go: generate_go ## Compile check; consumers fetch the SDK via the sdk/vX.Y.Z tag
	cd sdk && go build ./...

build_java: generate_java ## -> sdk/java/build/libs (best effort, not published)
	cd sdk/java && PACKAGE_VERSION=$(VERSION_GENERIC) gradle --console=plain build

# ---------------------------------------------------------------- local install / test
install_plugin: provider ## Install bin/ as plugin docker-agent v$(VERSION_GENERIC) for local `pulumi up`
	-$(PULUMI) plugin rm resource $(PACK) $(VERSION_GENERIC) --yes >/dev/null 2>&1
	$(PULUMI) plugin install resource $(PACK) $(VERSION_GENERIC) --file $(WORKING_DIR)/bin/$(PROVIDER) --reinstall
	@echo "Installed docker-agent $(VERSION_GENERIC). Programs without an explicit provider version can also"
	@echo "use the dev binary by putting $(WORKING_DIR)/bin first on PATH."

install_nodejs_sdk: build_nodejs ## yarn link the built nodejs SDK
	-yarn unlink --cwd $(WORKING_DIR)/sdk/nodejs/bin
	yarn link --cwd $(WORKING_DIR)/sdk/nodejs/bin

install_dotnet_sdk: build_dotnet ## Copy the built NuGet package to ./nuget
	mkdir -p $(WORKING_DIR)/nuget
	find sdk/dotnet -name '*.nupkg' -print -exec cp -p {} $(WORKING_DIR)/nuget \;

install: install_plugin install_nodejs_sdk install_dotnet_sdk

test_provider: ## All provider tests (compat tests skip themselves without docker-agent and Docker)
	cd $(PROVIDER_PATH) && go test -count=1 -cover -timeout 30m -parallel $(TESTPARALLELISM) ./...

test_compat: ## Compatibility tests against the real docker-agent CLI and a registry:2 container
	cd $(PROVIDER_PATH) && TELEMETRY_ENABLED=false go test -count=1 -timeout 30m -run TestCompat -v ./pkg/ociagent/...

# The examples push to DOCKER_AGENT_TEST_REGISTRY (a plain-HTTP registry on localhost that allows
# deletes). When it is unset, a throwaway registry:2 container is started on a free port and
# removed afterwards. EXAMPLES_RUN selects tests, e.g. `make test_examples EXAMPLES_RUN=TestExampleGo`.
EXAMPLES_REGISTRY_IMAGE ?= registry:2
EXAMPLES_RUN            ?= .

test_examples: provider build_nodejs build_python build_dotnet ## E2E: up/preview/refresh/destroy every program in examples/ against a registry
	@set -euo pipefail; \
	registry="$${DOCKER_AGENT_TEST_REGISTRY:-}"; \
	if [ -z "$$registry" ]; then \
		container="docker-agent-examples-registry-$$$$"; \
		trap 'docker rm -f "$$container" >/dev/null 2>&1 || true' EXIT INT TERM; \
		docker run -d --rm --name "$$container" -p 127.0.0.1::5000 \
			-e REGISTRY_STORAGE_DELETE_ENABLED=true $(EXAMPLES_REGISTRY_IMAGE) >/dev/null; \
		registry="localhost:$$(docker port "$$container" 5000/tcp | head -n1 | sed 's/.*://')"; \
		for _ in $$(seq 1 30); do curl -fsS "http://$$registry/v2/" >/dev/null 2>&1 && break; sleep 1; done; \
		echo "Started $(EXAMPLES_REGISTRY_IMAGE) on $$registry ($$container)"; \
	fi; \
	cd examples && DOCKER_AGENT_TEST_REGISTRY="$$registry" PULUMI_SKIP_UPDATE_CHECK=true \
		go test -v -count=1 -timeout 1h -parallel $(TESTPARALLELISM) -run '$(EXAMPLES_RUN)' ./...

test: test_provider

lint: ## golangci-lint on the provider module
	cd $(PROVIDER_PATH) && golangci-lint run $(if $(wildcard .golangci.yml),-c ../.golangci.yml,) ./...

goreleaser_check:
	goreleaser check

snapshot: ## Cross-compile all archives into dist/ without publishing
	goreleaser release --snapshot --clean --skip=publish,sign,sbom

clean:
	rm -rf bin dist nuget sdk/nodejs/bin sdk/nodejs/node_modules sdk/python/bin sdk/python/venv \
		sdk/dotnet/bin sdk/dotnet/obj sdk/java/build sdk/java/.gradle
