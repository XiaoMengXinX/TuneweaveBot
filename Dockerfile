FROM golang:1.26-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git gzip

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN (cd pluginsdk && go mod download) && (cd plugins/netease && go mod download)
RUN sh build.sh

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && mkdir -p /app/cache /app/log /app/plugins

WORKDIR /app

COPY --from=builder /build/TuneweaveBot .
COPY --from=builder /build/dist/plugins/ /app/plugins/

CMD [ "./TuneweaveBot" ]
