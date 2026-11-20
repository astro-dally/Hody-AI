#!/bin/bash
# Phase gate: gofmt, vet, build, test. Output is kept for the build log.
cd "$(dirname "$0")"
echo '$ gofmt -l .'; out=$(gofmt -l .); echo "${out:-(no files)}"; [ -z "$out" ] || exit 1
echo '$ go vet ./...'; go vet ./... || exit 1; echo ok
echo '$ go build ./...'; go build ./... || exit 1; echo ok
echo '$ go test ./...'; go test -count=1 ./... || exit 1
