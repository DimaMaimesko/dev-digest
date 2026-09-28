.PHONY: check fmt fmt-check vet lint test

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

test:
	go test -race ./...
