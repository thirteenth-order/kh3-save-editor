# kh3-save-editor: offline save tools for Kingdom Hearts III (PC)
#
# Repo is named for search; the binary is kh3save because you have to type it.
#
# `make` on its own lists the targets. `make ci` runs exactly what the CI
# workflow runs, minus the container job -- that one needs Docker, which the
# rest of the project does not, so it is `make docker-smoke` on its own.

SHELL      := /bin/sh
.DEFAULT_GOAL := help

GO         ?= go
# Only tools/gen_tables.py is left, and it is stdlib only, so this is a plain
# interpreter -- no virtualenv anywhere in the build any more.
PYTHON     ?= python3
BIN_DIR    := bin
BIN        := $(BIN_DIR)/kh3save
DIST       := dist
GOSRC      := $(shell find cmd internal -name '*.go' 2>/dev/null)
# The browser UI is embedded with //go:embed, so editing an asset changes the
# binary without changing a single .go file. Leaving these out of the
# prerequisites is why `make build` could answer "up to date" after the whole
# interface had been rewritten, and then `make gui` served the old page from a
# stale binary with nothing anywhere saying why.
EMBEDS     := $(shell find internal/gui/assets -type f 2>/dev/null)

# Version comes from the current tag when there is one, so a local build is
# stamped the same way the release workflow stamps it.
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X main.version=$(VERSION)

TARGETS := windows/amd64 windows/arm64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

# macOS ships shasum rather than sha256sum, so `make dist` would fail there.
SHA256 := $(shell command -v sha256sum >/dev/null 2>&1 && echo "sha256sum" || echo "shasum -a 256")

.PHONY: help
help: ## list the targets
	@echo "kh3-save-editor $(VERSION)  (binary: kh3save)"
	@echo
	@awk 'BEGIN {FS = ":.*##"} \
	  /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[1m%-16s\033[0m %s\n", $$1, $$2 } \
	  /^##@/ { printf "\n%s\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build



$(BIN): $(GOSRC) $(EMBEDS)
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/kh3save
	@echo "built $(BIN) ($(VERSION))"

.PHONY: build
build: $(BIN) ## build the Go binary into bin/

.PHONY: dist
dist: lint test ## cross-compile every target into dist/ with checksums
	@rm -rf $(DIST) && mkdir -p $(DIST)
	@for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; \
	  [ "$$os" = windows ] && ext=".exe"; \
	  out="$(DIST)/kh3save-$(VERSION)-$$os-$$arch$$ext"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build \
	     -trimpath -ldflags "$(LDFLAGS)" -o "$$out" ./cmd/kh3save || exit 1; \
	  echo "  $$out"; \
	done
	@cd $(DIST) && $(SHA256) * > SHA256SUMS
	@echo "==> $(DIST)/SHA256SUMS written"

##@ Run

.PHONY: gui
gui: $(BIN) ## build and open the browser UI
	@./$(BIN) gui

.PHONY: fixture
fixture: ## write a synthetic save tree to /tmp/kh3-fixture
	@rm -rf /tmp/kh3-fixture
	@$(GO) run ./tools/genfixture -layout /tmp/kh3-fixture

.PHONY: fixture-full
fixture-full: ## same, at the size of a real save so the tail regions are there
	@rm -rf /tmp/kh3-fixture-full
	@$(GO) run ./tools/genfixture -layout -full /tmp/kh3-fixture-full

.PHONY: demo
demo: ## re-record the README terminal GIFs (needs asciinema and agg)
	@tools/demo/record.sh $(SCENES)

.PHONY: shots
shots: ## re-shoot the README browser screenshots (needs a Chromium)
	@tools/demo/shoot.sh $(SHOTS)


##@ Docker

DOCKER    ?= docker
COMPOSE   ?= docker compose
IMAGE     ?= kh3save:local
DEV_IMAGE ?= kh3save-dev:local
# The folder that holds "KINGDOM HEARTS III" -- or that folder itself. Mounting
# it whole matters: the account id is a directory name on the way down to the
# save, so a mount of the data/ directory alone leaves nothing to derive the
# key from.
KH3_SAVES ?= saves

# Compose reads a bare "saves:/saves" as a reference to a named volume, not as
# a path, so a relative KH3_SAVES needs its ./ back. Tested with $(filter)
# rather than $(abspath) because abspath splits on spaces, and the folder this
# points at is called KINGDOM HEARTS III.
KH3_MOUNT = $(if $(filter /%,$(KH3_SAVES)),$(KH3_SAVES),./$(KH3_SAVES))

# A container writing into a bind mount writes as whatever uid it runs as, so
# it runs as yours. Without this the edited save comes back owned by 65532.
DOCKER_ENV = KH3_SAVES="$(KH3_MOUNT)" KH3_UID=$$(id -u) KH3_GID=$$(id -g)

define require_saves
@test -e "$(KH3_SAVES)" || { \
  echo "no such path: $(KH3_SAVES)"; \
  echo; echo "point KH3_SAVES at your save folder, for example:"; \
  echo "  make $@ KH3_SAVES=\"$$HOME/Documents/KINGDOM HEARTS III\""; \
  exit 1; }
endef

.PHONY: docker-build
docker-build: ## build the container image
	@$(DOCKER) build --build-arg VERSION=$(VERSION) -t $(IMAGE) .
	@echo "built $(IMAGE) ($(VERSION))"

.PHONY: docker-gui
docker-gui: ## run the UI in a container: make docker-gui KH3_SAVES=<folder>
	$(require_saves)
	@$(DOCKER_ENV) $(COMPOSE) up --build gui

.PHONY: docker-cli
docker-cli: ## run one command, no network: make docker-cli ARGS="info /saves"
	$(require_saves)
	@$(DOCKER_ENV) $(COMPOSE) --progress quiet --profile cli run --rm --build cli $(ARGS)

.PHONY: docker-dev
docker-dev: ## build from source with no toolchain installed: make docker-dev [ARGS="make ci"]
	@$(DOCKER_ENV) $(COMPOSE) --profile dev build dev
	@$(DOCKER_ENV) $(COMPOSE) --profile dev run --rm dev $(or $(ARGS),bash)

.PHONY: docker-smoke
docker-smoke: docker-build fixture ## build the image and check it end to end
	@KH3_FIXTURE=/tmp/kh3-fixture tools/docker-smoke.sh $(IMAGE)

.PHONY: docker-clean
docker-clean: ## remove the containers, images and the dev cache volume
	@$(COMPOSE) --profile cli --profile dev down --remove-orphans --volumes 2>/dev/null || true
	@$(DOCKER) image rm -f $(IMAGE) $(DEV_IMAGE) 2>/dev/null || true
	@echo "removed $(IMAGE) and $(DEV_IMAGE)"

##@ Test

.PHONY: test
test: test-go ## run the test suite

.PHONY: test-go
test-go: ## go test
	@$(GO) test ./... -count=1

.PHONY: test-race
test-race: ## go test under the race detector
	@$(GO) test ./... -race -count=1

# The browser half is about twenty-three hundred lines that no Go test reaches.
# `go test ./internal/gui` already runs tools/jscheck, which executes it against
# the real schema and a dump of the fixture. This is the other half of the
# safety net: a type checker that reads the same files where they sit and finds
# the undefined name and the misspelled property without needing a DOM to run
# them in. It emits nothing -- the page stays classic scripts with no build
# step, and the binary embeds the assets exactly as written.
#
# Both skip when node is not installed, so a machine without it can still run
# the suite. CI pins node, so there they run by declaration and not by luck.
.PHONY: typecheck
typecheck: ## type-check the browser assets (needs node; skipped without it)
	@if ! command -v node >/dev/null 2>&1; then 	  echo "node not installed; skipping the browser type check"; exit 0; fi; 	  cd tools/jscheck && 	  if [ ! -x node_modules/.bin/tsc ]; then npm ci --silent --no-fund --no-audit; fi && 	  ./node_modules/.bin/tsc -p tsconfig.json && echo "==> browser assets type-check clean"

.PHONY: ci
ci: lint test test-race typecheck tables-check emblem-check ## everything the CI workflow runs
	@echo
	@echo "==> all green"

##@ Lint and codegen

.PHONY: lint
lint: fmt-check vet ## gofmt check + go vet

.PHONY: fmt
fmt: ## format the Go sources
	@gofmt -w cmd internal

.PHONY: fmt-check
fmt-check: ## fail if anything needs gofmt
	@unformatted=$$(gofmt -l cmd internal); \
	  if [ -n "$$unformatted" ]; then \
	    echo "these files need gofmt:"; echo "$$unformatted"; exit 1; \
	  fi

.PHONY: vet
vet: ## go vet
	@$(GO) vet ./...

.PHONY: tables
tables: ## regenerate the ability/item tables from upstream
	@$(PYTHON) tools/gen_tables.py

.PHONY: tables-check
tables-check: ## fail if a generated table is stale
	@$(PYTHON) tools/gen_tables.py --check

.PHONY: emblem
emblem: ## redraw the rose-window artwork in the web UI
	@$(PYTHON) tools/gen_emblem.py

.PHONY: emblem-check
emblem-check: ## fail if the committed artwork is stale
	@$(PYTHON) tools/gen_emblem.py --check

##@ Housekeeping

.PHONY: set-module
set-module: ## rewrite the Go module path: make set-module TO=github.com/org/repo
	@test -n "$(TO)" || { echo "usage: make set-module TO=github.com/org/repo"; exit 1; }
	@old=$$($(GO) list -m); \
	  grep -rIl "$$old" --include='*.go' --include='*.mod' --include='*.yml' --include='*.md' . \
	    | grep -v '^./refs\|^./.venv' \
	    | xargs sed -i "s|$$old|$(TO)|g"; \
	  echo "module path: $$old -> $(TO)"
	@$(GO) build ./... && echo "builds clean"


.PHONY: clean
clean: ## remove build output and caches
	@rm -rf $(BIN_DIR) $(DIST)
	@find . -name __pycache__ -type d -prune -exec rm -rf {} + 2>/dev/null || true
	@echo "cleaned (kept refs/; use distclean to drop that too)"

.PHONY: distclean
distclean: clean ## also remove the cloned upstream refs
	@rm -rf refs
	@echo "removed refs/"
