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

# 面板前端构建到 internal/webapp/dist 后嵌入二进制；SKIP_WEB=1 时沿用该目录的现有内容
if [ "${SKIP_WEB:-0}" != "1" ]; then
    (cd web && npm ci && npm run build)
fi

CGO_ENABLED=0 go build -trimpath -ldflags "${LDFlags}" -o TuneweaveBot ./cmd/tuneweavebot

# SKIP_PLUGINS=1 时只构建主程序（插件由 plugin 工作流单独构建）
if [ "${SKIP_PLUGINS:-0}" != "1" ]; then
    sh build-plugins.sh
fi
