#!/bin/sh

set -eu

artifact=./bin/legal-callegarin
backup_directory="$(mktemp -d)"
had_existing_artifact=false

cleanup() {
	rm -f "$artifact"
	if [ "$had_existing_artifact" = true ]; then
		mv "$backup_directory/legal-callegarin" "$artifact"
	fi
	rmdir "$backup_directory"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [ -e "$artifact" ]; then
	mv "$artifact" "$backup_directory/legal-callegarin"
	had_existing_artifact=true
fi

unexpected_artifacts="$(find ./bin -mindepth 1 ! -name .keep -print)"
if [ -n "$unexpected_artifacts" ]; then
	echo "Unexpected binary artifacts:" >&2
	echo "$unexpected_artifacts" >&2
	exit 1
fi

go build -o "$artifact" ./cmd/web
test -x "$artifact"
