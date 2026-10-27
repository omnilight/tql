.PHONY: build test race vet fmt fmt-check check lint workflow-lint vuln tools fuzz conformance yaml-suite release clean \
	fuzz-json fuzz-toml fuzz-yaml fuzz-yaml-graph fuzz-query fuzz-exec fuzz-insertion fuzz-yaml-edits fuzz-yaml-aliases fuzz-quote

VERSION ?= dev
FUZZTIME ?= 10s
FUZZMINIMIZETIME ?= 3s

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/tql ./cmd/tql

test:
	go test ./...

race:
	go test -race -cover ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

check: fmt-check vet race

lint:
	golangci-lint run ./...

workflow-lint:
	actionlint .github/workflows/*.yml

vuln:
	govulncheck ./...

# Install development tools without adding runtime dependencies to go.mod.
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
	go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
	go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

fuzz: fuzz-json fuzz-toml fuzz-yaml fuzz-query fuzz-exec fuzz-insertion fuzz-yaml-edits fuzz-yaml-aliases fuzz-quote

fuzz-json:
	go test ./formats/json -run '^$$' -fuzz '^FuzzParse$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-toml:
	go test ./formats/toml -run '^$$' -fuzz '^FuzzParse$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-yaml: fuzz-yaml-graph
	go test ./formats/yaml -run '^$$' -fuzz '^FuzzParse$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-yaml-graph:
	go test ./formats/yaml -run '^$$' -fuzz '^FuzzGraph$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-query:
	go test ./internal/query -run '^$$' -fuzz '^FuzzParse$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-exec:
	go test . -run '^$$' -fuzz '^FuzzExec$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-insertion:
	go test . -run '^$$' -fuzz '^FuzzStyledInsertion$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-yaml-edits:
	go test ./formats/yaml -run '^$$' -fuzz '^FuzzEdits$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-yaml-aliases:
	go test ./formats/yaml -run '^$$' -fuzz '^FuzzAliasEdits$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

fuzz-quote:
	go test ./syntax -run '^$$' -fuzz '^FuzzQuote$$' -fuzztime $(FUZZTIME) -fuzzminimizetime $(FUZZMINIMIZETIME) -parallel 2

conformance:
	go test ./conformance -count=1

yaml-suite:
	go test ./conformance -run '^TestYAMLTestSuite$$' -count=1

release:
	go run ./scripts/release -version $(VERSION) -output dist

clean:
	rm -rf bin dist coverage.out coverage.html
