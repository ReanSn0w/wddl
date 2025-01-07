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
ENV PASSWORD=guest
ENV INPUT=/
ENV OUTPUT=/data
ENV THREADS=4
ENV TIMEOUT=600

VOLUME [ "/data" ]
WORKDIR /srv
ENTRYPOINT ["/srv/app"]
