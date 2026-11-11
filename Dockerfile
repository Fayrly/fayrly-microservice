# syntax=docker/dockerfile:1

# One image definition, one binary per build. Compose passes SERVICE=<name>.
FROM golang:1.26-alpine AS build

ARG SERVICE
ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /src

RUN test -n "$SERVICE" || (echo "SERVICE build arg is required (auth|user|event|report|gateway)" >&2; exit 1)

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/service ./cmd/${SERVICE}

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app -H -s /sbin/nologin app

COPY --from=build /out/service /usr/local/bin/service

USER app
EXPOSE 8080 8081 8082 8083 8084

ENTRYPOINT ["/usr/local/bin/service"]
