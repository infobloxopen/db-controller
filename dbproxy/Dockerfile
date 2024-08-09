FROM golang:1.22-alpine3.20 AS builder

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
COPY dbproxy.go dbproxy.go
COPY pgbouncer/ pgbouncer/
COPY *.sh .

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a

FROM alpine:3.20 AS PGBBUILDER
ARG VERSION=1.17.0

ADD /pgbouncer/docker/pgbouncer.patch /tmp/pgbouncer.patch

RUN apk add -U --no-cache --upgrade busybox && \
  apk add -U --no-cache autoconf autoconf-doc automake udns udns-dev curl gcc libc-dev libevent libevent-dev libtool make openssl-dev patch pkgconfig postgresql-client && \
  curl -o  /tmp/pgbouncer-$VERSION.tar.gz -L https://pgbouncer.github.io/downloads/files/$VERSION/pgbouncer-$VERSION.tar.gz && \
  cd /tmp && \
  tar xvfz /tmp/pgbouncer-$VERSION.tar.gz && \
  cd pgbouncer-$VERSION && \
  patch -p 1 < /tmp/pgbouncer.patch && \
  ./configure --prefix=/usr --with-udns && \
  make && \
  cp pgbouncer /usr/bin

FROM alpine:3.20

RUN apk add -U --no-cache udns libevent openssl ca-certificates postgresql-client

RUN adduser -D -S -s /bin/sh bloxone

RUN mkdir -p /var/run/dbproxy
RUN chown -R bloxone /var/run/dbproxy

COPY --from=pgbbuilder /usr/bin/pgbouncer /usr/bin/pgbouncer
COPY --from=builder /workspace/dbproxy /usr/bin/dbproxy
COPY --from=builder /workspace/pgbouncer/pgbouncer.template /var/run/dbproxy/pgbouncer.template
COPY --from=builder /workspace/*.sh /var/run/dbproxy/

RUN chmod +x /usr/bin/pgbouncer
RUN chmod +x /var/run/dbproxy/start-pgbouncer.sh /var/run/dbproxy/reload-pgbouncer.sh

WORKDIR /var/run/dbproxy

USER bloxone

# TODO: remove this entrypoint and hardcode it in mutating webhook
ENTRYPOINT /usr/bin/dbproxy
