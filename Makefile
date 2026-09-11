.PHONY: build check

build:
	npm run build
	mkdir -p ./bin
	go build -o ./bin/legal-callegarin ./cmd/web

check:
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Go files need formatting:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	go tool staticcheck ./...
	go test ./...
	npm ci
	npm run typecheck
	npm test -- --run
	npm run build
