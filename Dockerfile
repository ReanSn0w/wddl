FROM golang:1.22-alpine AS application

ARG GITHUB_REF
ADD . /bundle

WORKDIR /bundle

RUN apk --no-cache add ca-certificates

RUN \
    revision=${GITHUB_REF} && \
    echo "Building container. Revision: ${revision}" && \
    go build -ldflags "-X main.revision=${revision}" -o /srv/app ./cmd/webdav/main.go

# Финальная сборка образа
FROM scratch
COPY --from=application /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=application /srv /srv

ENV SERVER=https://dav.yandex.ru
ENV USER=guest
ENV PASSWORD=
ENV SYNC=false
ENV INPUT=/
ENV OUTPUT=/data

VOLUME [ "/data" ]
WORKDIR /srv
ENTRYPOINT ["/srv/app"]
