FROM golang:1.26-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# 插件不打进镜像：从 plugin release 下载到挂载的 /app/plugins，才能通过 /checkupdate 更新
RUN SKIP_PLUGINS=1 sh build.sh

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && mkdir -p /app/cache /app/log /app/plugins

WORKDIR /app

COPY --from=builder /build/TuneweaveBot .

CMD [ "./TuneweaveBot" ]
