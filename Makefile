# SLO Autopilot — common tasks. Run `make help` for the list.
BINARY      := slo-autopilot
PKG         := ./cmd/slo-autopilot
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
DEMO_SPEC   := examples/checkout-api.demo.slo.yaml
COMPOSE     := docker compose -f deploy/docker-compose.yml

.DEFAULT_GOAL := help

## help: show this help
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | awk -F': ' '{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## build: compile the slo-autopilot binary into ./bin
.PHONY: build
build:
	@mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

## install: go install the CLI
.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)" $(PKG)

## test: run unit tests with the race detector
.PHONY: test
test:
	go test -race -count=1 ./...

## vet: run go vet
.PHONY: vet
vet:
	go vet ./...

## fmt: format all Go code
.PHONY: fmt
fmt:
	gofmt -w .

## lint: run golangci-lint (must be installed)
.PHONY: lint
lint:
	golangci-lint run

## generate: regenerate the committed demo rules + dashboard from the spec
.PHONY: generate
generate: build
	bin/$(BINARY) generate -f $(DEMO_SPEC) --policy fast --out-dir /tmp/slo-gen
	cp /tmp/slo-gen/checkout-api-demo.rules.yaml deploy/prometheus/rules/
	cp /tmp/slo-gen/checkout-api-demo.dashboard.json deploy/grafana/dashboards/

## demo-up: start the local Prometheus + Grafana + service demo
.PHONY: demo-up
demo-up:
	$(COMPOSE) up --build -d
	@echo
	@echo "  Grafana    http://localhost:3000  (dashboard: SLO — checkout-api-demo)"
	@echo "  Prometheus http://localhost:9090  (/alerts)"
	@echo "  Service    http://localhost:8080/checkout"
	@echo
	@echo "  Start a fire:  curl 'http://localhost:8080/chaos?errors=0.4'"
	@echo "  Run the gate:  make demo-gate"
	@echo "  Put it out:    curl 'http://localhost:8080/chaos?reset=1'"

## demo-gate: run the release gate against the running demo (fast policy)
.PHONY: demo-gate
demo-gate: build
	bin/$(BINARY) gate -f $(DEMO_SPEC) --policy fast --prometheus http://localhost:9090

## demo-budget: show the live error-budget report for the running demo
.PHONY: demo-budget
demo-budget: build
	bin/$(BINARY) budget -f $(DEMO_SPEC) --policy fast --prometheus http://localhost:9090

## demo-chaos: inject a 40% error rate into the demo service
.PHONY: demo-chaos
demo-chaos:
	curl -s 'http://localhost:8080/chaos?errors=0.4' && echo

## demo-heal: clear injected chaos
.PHONY: demo-heal
demo-heal:
	curl -s 'http://localhost:8080/chaos?reset=1' && echo

## demo-down: stop the demo and remove volumes
.PHONY: demo-down
demo-down:
	$(COMPOSE) down -v

## clean: remove build artifacts
.PHONY: clean
clean:
	rm -rf bin
