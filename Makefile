CONTAINER_ENGINE ?= docker
SBOM_OUTPUT ?= artifacts/sbom.cdx.json
SEVERITY ?= HIGH,CRITICAL
SYFT_IMAGE := anchore/syft:v1.51.1-nonroot@sha256:277f11d9e3dd8a6853f6e102156c79578d0d9adffe563db613b4397377bbbc0a
TRIVY_IMAGE := ghcr.io/aquasecurity/trivy:0.74.0@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969

export CONTAINER_ENGINE IMAGE SBOM_OUTPUT SEVERITY SYFT_IMAGE TRIVY_IMAGE

.PHONY: build check container-smoke sbom scan workflow-policy

build:
	npm run build
	mkdir -p ./bin
	go build -o ./bin/legal-callegarin ./cmd/web

check: workflow-policy
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then \
		echo "Go files need formatting:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi
	go vet ./...
	go tool staticcheck ./...
	go test ./...
	node --test scripts/*.test.mjs
	./scripts/check-clean-build.sh
	npm ci
	npm run format:check
	npm run typecheck
	npm test -- --run
	npm run build

container-smoke:
	./scripts/container-smoke.sh

workflow-policy:
	node scripts/check-workflows.mjs

sbom:
	@set -eu; \
	if [ -z "$${IMAGE:-}" ]; then \
		echo "IMAGE is required; use make sbom IMAGE=<local-image>" >&2; \
		exit 2; \
	fi; \
	if [ -z "$${SBOM_OUTPUT:-}" ]; then \
		echo "SBOM_OUTPUT must not be empty" >&2; \
		exit 2; \
	fi; \
	output_dir=$$(dirname -- "$$SBOM_OUTPUT"); \
	mkdir -p -- "$$output_dir"; \
	work_dir=$$(mktemp -d "$${TMPDIR:-/tmp}/legal-callegarin-sbom.XXXXXX"); \
	archive="$$work_dir/image.tar"; \
	output_tmp=; \
	cleanup() { \
		status=$$?; \
		trap - EXIT HUP INT TERM; \
		if [ -n "$$output_tmp" ]; then rm -f -- "$$output_tmp" || :; fi; \
		chmod 0700 "$$work_dir" 2>/dev/null || :; \
		rm -f -- "$$archive" || :; \
		rmdir -- "$$work_dir" 2>/dev/null || :; \
		exit "$$status"; \
	}; \
	trap cleanup EXIT HUP INT TERM; \
	output_tmp=$$(mktemp "$$output_dir/.sbom.cdx.json.tmp.XXXXXX"); \
	"$$CONTAINER_ENGINE" image save --output "$$archive" "$$IMAGE"; \
	chmod 0555 "$$work_dir"; \
	chmod 0444 "$$archive"; \
	"$$CONTAINER_ENGINE" run --rm --network none --read-only \
		--cap-drop ALL --security-opt no-new-privileges \
		--tmpfs /tmp:rw,nosuid,nodev,noexec,size=64m \
		--mount "type=bind,src=$$work_dir,dst=/scan,readonly" \
		"$$SYFT_IMAGE" "docker-archive:/scan/image.tar" \
		--source-name "$$IMAGE" --output cyclonedx-json >"$$output_tmp"; \
	if ! node -e 'const fs = require("node:fs"); const [path, image] = process.argv.slice(1); let document; try { document = JSON.parse(fs.readFileSync(path, "utf8")); } catch { process.exit(1); } if (document?.bomFormat !== "CycloneDX" || document?.metadata?.component?.name !== image) process.exit(1);' "$$output_tmp" "$$IMAGE"; then \
		echo "SBOM must be valid CycloneDX JSON for the requested image" >&2; \
		exit 1; \
	fi; \
	mv -f -- "$$output_tmp" "$$SBOM_OUTPUT"; \
	output_tmp=

scan:
	@set -eu; \
	if [ -z "$${IMAGE:-}" ]; then \
		echo "IMAGE is required; use make scan IMAGE=<local-image>" >&2; \
		exit 2; \
	fi; \
	work_dir=$$(mktemp -d "$${TMPDIR:-/tmp}/legal-callegarin-scan.XXXXXX"); \
	archive="$$work_dir/image.tar"; \
	cleanup() { \
		status=$$?; \
		trap - EXIT HUP INT TERM; \
		chmod 0700 "$$work_dir" 2>/dev/null || :; \
		rm -f -- "$$archive" || :; \
		rmdir -- "$$work_dir" 2>/dev/null || :; \
		exit "$$status"; \
	}; \
	trap cleanup EXIT HUP INT TERM; \
	"$$CONTAINER_ENGINE" image save --output "$$archive" "$$IMAGE"; \
	chmod 0555 "$$work_dir"; \
	chmod 0444 "$$archive"; \
	"$$CONTAINER_ENGINE" run --rm \
		--cap-drop ALL --security-opt no-new-privileges \
		--mount "type=bind,src=$$work_dir,dst=/scan,readonly" \
		"$$TRIVY_IMAGE" image --input /scan/image.tar \
		--scanners vuln --severity "$$SEVERITY" --ignore-unfixed=false \
		--exit-code 1 --no-progress
