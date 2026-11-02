#!/bin/sh
# 构建 WASM 插件到 dist/plugins，并复制到运行时插件目录 plugins/。
# 用法：
#   bash build-plugins.sh                 # 构建全部插件
#   PLUGINS="netease" bash build-plugins.sh
#   NO_COPY=1 bash build-plugins.sh       # 只输出到 dist/plugins
set -eu

PLUGINS=${PLUGINS:-netease}
OUT_DIR=${OUT_DIR:-dist/plugins}
COMMIT_SHA=$(git rev-parse HEAD 2>/dev/null || true)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)

mkdir -p "$OUT_DIR"
for name in $PLUGINS; do
    echo "构建插件 $name -> $OUT_DIR/$name.wasm"
    CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm \
        go build -buildmode=c-shared -trimpath \
        -ldflags="-s -w -X main.commitSHA=$COMMIT_SHA -X main.buildTime=$BUILD_TIME" \
        -o "$OUT_DIR/$name.wasm" "./plugins/$name"
    if [ "${NO_COPY:-0}" != "1" ]; then
        mkdir -p plugins
        cp "$OUT_DIR/$name.wasm" "plugins/$name.wasm"
    fi
done
