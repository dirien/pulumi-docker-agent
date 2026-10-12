PROJECT_NAME     := Pulumi Docker Agent Provider

SHELL            := /bin/bash
PACK             := docker-agent
GOPKG            := dockeragent
ORG              := dirien
PROJECT          := github.com/$(ORG)/pulumi-$(PACK)
PROVIDER         := pulumi-resource-$(PACK)
CONVERTER        := pulumi-converter-$(PACK)
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

.PHONY: help ensure provider provider_debug schema docs codegen generate_sdks generate_config_types check_config_types \
        upgrade_docker_agent check_docker_agent_upgrade \
        generate_nodejs generate_python generate_dotnet generate_go generate_java \
        build build_sdks build_nodejs build_python build_dotnet build_go build_java \
        converter install_converter \
        install install_plugin install_nodejs_sdk install_dotnet_sdk \
        test test_provider test_compat test_examples test_policy lint goreleaser_check snapshot clean

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

# The converter plugin (`pulumi convert --from docker-agent`) is released next to the provider,
# with the same version. It does not link the docker-agent runtime.
converter: ## Build bin/pulumi-converter-docker-agent (the `pulumi convert --from docker-agent` plugin)
	cd $(PROVIDER_PATH) && go build -o $(WORKING_DIR)/bin/$(CONVERTER) -ldflags "$(LDFLAGS)" ./cmd/$(CONVERTER)

schema: provider ## Regenerate the committed schema.json (version stripped)
	$(PULUMI) package get-schema $(WORKING_DIR)/bin/$(PROVIDER) | jq 'del(.version)' > $(SCHEMA_FILE)

# ---------------------------------------------------------------- docs (committed)
# The Example Usage sections of the API docs (provider/pkg/provider/embed/*-examples.md, embedded in
# the resource and function descriptions) and of docs/_index.md are generated from the Pulumi YAML
# programs in docs/yaml by provider/internal/docsgen: `pulumi convert --generate-only` against bin/
# turns each one into TypeScript, Python, Go, C#, Java and HCL. The YAML converter and the HCL
# language plugin are pinned so that their releases do not change the output (the other program
# generators come with the Pulumi CLI of .pulumi.version). bin/ is built again afterwards because
# it embeds the examples that `schema` reads.
PULUMI_CONVERTER_YAML_VERSION ?= 1.38.9
PULUMI_HCL_VERSION            ?= 0.18.4

docs: provider ## Regenerate the Example Usage sections from docs/yaml (all languages), then rebuild bin/
	$(PULUMI) plugin install converter yaml $(PULUMI_CONVERTER_YAML_VERSION)
	$(PULUMI) plugin install language hcl $(PULUMI_HCL_VERSION)
	cd $(PROVIDER_PATH) && go run ./internal/docsgen
	@$(MAKE) --no-print-directory provider

# ---------------------------------------------------------------- codegen (committed, version-agnostic)
# SDKs are generated WITHOUT --version so the committed tree never changes between releases
# (same model as dirien/pulumi-fly). The version is injected only into build outputs (bin/, gitignored).
codegen: generate_config_types docs schema generate_sdks ## Typed config inputs + examples + schema.json + all SDK sources

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
# upstream example configurations, which the coverage and round-trip tests read. Then
# provider/internal/examplegen turns every vendored example into a TypeScript and a Python program
# (examples/docker-agent-examples; the hand-written project files there are not generated).
EXAMPLES_GEN     := examples/docker-agent-examples
GENERATED_CONFIG := $(PROVIDER_PATH)/pkg/render/config_types.gen.go $(PROVIDER_PATH)/pkg/render/version.gen.go \
	$(PROVIDER_PATH)/pkg/render/schema $(PROVIDER_PATH)/pkg/render/testdata/upstream \
	$(PROVIDER_PATH)/pkg/validate/upstream.gen.go \
	$(EXAMPLES_GEN)/README.md $(EXAMPLES_GEN)/testdata \
	$(EXAMPLES_GEN)/typescript/index.ts $(EXAMPLES_GEN)/typescript/examples \
	$(EXAMPLES_GEN)/python/__main__.py $(EXAMPLES_GEN)/python/examples
DOCKER_AGENT_VERSION ?=

generate_config_types: ## Regenerate the typed AgentConfig inputs and the example programs from the pinned docker-agent
	cd $(PROVIDER_PATH) && go run ./internal/schemagen generate && go run ./internal/examplegen

check_config_types: generate_config_types ## Fail if the generated config types or example programs differ from provider/go.mod's docker-agent
	@git update-index -q --refresh; \
	if [ -n "$$(git status --porcelain -- $(GENERATED_CONFIG))" ]; then \
		echo "::error title=Stale config types::the generated config types or example programs do not match the docker-agent version in provider/go.mod. Run 'make generate_config_types' (or 'make upgrade_docker_agent') and commit the result."; \
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
build_sdks: build_nodejs build_python build_dotnet build_go build_java ## Build the nodejs, python, dotnet, go and java SDKs

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

# Builds exactly what the release publishes to Maven Central (io.github.dirien:docker-agent): the
# jar, the sources and javadoc jars and the POM. scripts/java-sdk.init.gradle completes the POM
# metadata that Central requires and pulumi-java leaves empty (sdk/java/build.gradle is generated).
build_java: generate_java ## -> sdk/java/build/libs (jar, sources, javadoc) and sdk/java/build/publications (POM)
	cd sdk/java && PACKAGE_VERSION=$(VERSION_GENERIC) gradle --console=plain --init-script ../../scripts/java-sdk.init.gradle \
		build sourcesJar javadocJar generatePomFileForMainPublicationPublication

# ---------------------------------------------------------------- local install / test
install_plugin: provider ## Install bin/ as plugin docker-agent v$(VERSION_GENERIC) for local `pulumi up`
	-$(PULUMI) plugin rm resource $(PACK) $(VERSION_GENERIC) --yes >/dev/null 2>&1
	$(PULUMI) plugin install resource $(PACK) $(VERSION_GENERIC) --file $(WORKING_DIR)/bin/$(PROVIDER) --reinstall
	@echo "Installed docker-agent $(VERSION_GENERIC). Programs without an explicit provider version can also"
	@echo "use the dev binary by putting $(WORKING_DIR)/bin first on PATH."

install_converter: converter ## Install bin/ as converter plugin docker-agent v$(VERSION_GENERIC) for local `pulumi convert --from docker-agent`
	-$(PULUMI) plugin rm converter $(PACK) $(VERSION_GENERIC) --yes >/dev/null 2>&1
	$(PULUMI) plugin install converter $(PACK) $(VERSION_GENERIC) --file $(WORKING_DIR)/bin/$(CONVERTER) --reinstall

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

test_examples: provider converter build_nodejs build_python build_dotnet ## E2E: run every program in examples/ (and the generated docker-agent examples) against a registry
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

# The policy pack in policy/ depends on the Node.js SDK built into sdk/nodejs/bin.
test_policy: ## Type-check and unit-test the policy pack in policy/ (needs `make build_nodejs`)
	cd policy && npm install --no-audit --no-fund && npm test

test: test_provider

lint: ## golangci-lint on the provider module
	cd $(PROVIDER_PATH) && golangci-lint run $(if $(wildcard .golangci.yml),-c ../.golangci.yml,) ./...

goreleaser_check:
	goreleaser check

snapshot: ## Cross-compile all archives into dist/ without publishing
	goreleaser release --snapshot --clean --skip=publish,sign,sbom

clean:
	rm -rf bin dist nuget sdk/nodejs/bin sdk/nodejs/node_modules sdk/python/bin sdk/python/venv \
		sdk/dotnet/bin sdk/dotnet/obj sdk/java/build sdk/java/.gradle \
		policy/node_modules policy/bin examples/automation-ts/node_modules examples/automation-ts/bin
