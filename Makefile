.PHONY: build test race vet fmt fmt-check check lint workflow-lint vuln tools fuzz oracle release clean

VERSION ?= dev
FUZZTIME ?= 10s

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
	go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
	go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

fuzz:
	go test ./formats/json -run '^$$' -fuzz '^FuzzParse$$' -fuzztime $(FUZZTIME) -parallel 2
	go test ./formats/toml -run '^$$' -fuzz '^FuzzParse$$' -fuzztime $(FUZZTIME) -parallel 2
	go test ./formats/yaml -run '^$$' -fuzz '^FuzzParse$$' -fuzztime $(FUZZTIME) -parallel 2
	go test ./internal/query -run '^$$' -fuzz '^FuzzParse$$' -fuzztime $(FUZZTIME) -parallel 2
	go test . -run '^$$' -fuzz '^FuzzExec$$' -fuzztime $(FUZZTIME) -parallel 2
	go test . -run '^$$' -fuzz '^FuzzStyledInsertion$$' -fuzztime $(FUZZTIME) -parallel 2
	go test ./formats/yaml -run '^$$' -fuzz '^FuzzEdits$$' -fuzztime $(FUZZTIME) -parallel 2
	go test ./formats/yaml -run '^$$' -fuzz '^FuzzAliasEdits$$' -fuzztime $(FUZZTIME) -parallel 2
	go test ./syntax -run '^$$' -fuzz '^FuzzQuote$$' -fuzztime $(FUZZTIME) -parallel 2

oracle:
	python3 formats/toml/testdata/oracle/compare.py

release:
	go run ./scripts/release -version $(VERSION) -output dist

clean:
	rm -rf bin dist coverage.out coverage.html
