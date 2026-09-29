.PHONY: check fmt fmt-check vet lint test generate build

# Everything CI runs. Run it before every commit.
check: fmt-check vet lint test

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then echo "Files need gofmt (run: make fmt):"; echo "$$out"; exit 1; fi

vet:
	go vet ./...

# Install with: go install honnef.co/go/tools/cmd/staticcheck@2026.1
lint:
	staticcheck ./...

# Tests that need a database start Postgres in Docker, and skip without it.
test:
	go test -race ./...

# Build the commands into bin/.
build:
	go build -o bin/ ./cmd/...

# Regenerate internal/postgres from the SQL in internal/postgres/queries.
# The first run compiles sqlc's Postgres parser, which takes a few minutes.
generate:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
