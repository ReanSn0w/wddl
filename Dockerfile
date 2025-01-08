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

ENV WEBDAV_SERVER=https://dav.yandex.ru
ENV WEBDAV_USER=guest
ENV WEBDAV_PASSWORD=guest
ENV INPUT=/
ENV TEMP=./tmp
ENV OUTPUT=./data
ENV THREADS=4
ENV TIMEOUT=600
ENV DB_FILE=./wddl.db
VOLUME [ "/data" ]

WORKDIR /srv
ENTRYPOINT ["/srv/app"]
