FROM golang:1.24-alpine AS application

ARG TAG
ADD . /bundle

WORKDIR /bundle

RUN apk --no-cache add ca-certificates

RUN \
    revision=${TAG} && \
    echo "Building container. Revision: ${revision}" && \
    go build -ldflags "-X main.revision=${revision}" -o /srv/app ./cmd/webdav/main.go

# Финальная сборка образа
FROM scratch
COPY --from=application /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=application /srv /srv

ENV WDDL_CONFIG=/config/config.yaml
ENV WEBDAV_USER=""
ENV WEBDAV_PASSWORD=""
VOLUME [ "/data" ]

WORKDIR /srv
ENTRYPOINT ["/srv/app"]
