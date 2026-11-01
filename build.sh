#!/bin/bash
set -euo pipefail

COMMIT_SHA=$(git rev-parse HEAD)
VERSION=$(git describe --tags 2>/dev/null || echo "dev")
BUILD_TIME=$(date +'%Y-%m-%d %T')

LDFlags="\
    -s -w \
    -X 'main.versionName=${VERSION}' \
    -X 'main.commitSHA=${COMMIT_SHA}' \
    -X 'main.buildTime=${BUILD_TIME}'\
"

CGO_ENABLED=0 go build -trimpath -ldflags "${LDFlags}" -o TuneweaveBot ./cmd/tuneweavebot

sh build-plugins.sh
