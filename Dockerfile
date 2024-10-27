FROM golang:1.22-alpine AS application

ARG GITHUB_REF
ADD . /bundle

WORKDIR /bundle

RUN \
    revision=${GITHUB_REF} && \
    echo "Building container. Revision: ${revision}" && \
    go build -ldflags "-X main.revision=${revision}" -o /srv/app ./cmd/webdav/main.go

# Финальная сборка образа
FROM scratch
COPY --from=application /srv /srv

ENV SERVER https://dav.yandex.ru
ENV USER guest
ENV PASSWORD guest
ENV SYNC false
ENV INPUT /
ENV OUTPUT /data

WORKDIR /srv
ENTRYPOINT ["/srv/app"]
