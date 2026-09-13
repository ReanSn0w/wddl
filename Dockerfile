# syntax=docker/dockerfile:1.12

ARG GO_VERSION=1.24.13
ARG ALPINE_VERSION=3.22

FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG TARGETOS
ARG TARGETARCH
ARG TAG=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.revision=${TAG}" -o /out/wddl ./cmd/webdav

FROM alpine:${ALPINE_VERSION} AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 10001 wddl \
    && adduser -S -D -H -u 10001 -G wddl wddl \
    && mkdir -p /var/lib/wddl /run/wddl \
    && chown wddl:wddl /var/lib/wddl /run/wddl
COPY --from=builder /out/wddl /usr/local/bin/wddl

ENV WDDL_CONFIG=/config/config.yaml

WORKDIR /var/lib/wddl
USER wddl:wddl
ENTRYPOINT ["wddl"]
CMD ["run"]
