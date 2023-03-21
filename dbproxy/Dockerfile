FROM golang:1.18 as BUILDER

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
COPY dbproxy.go dbproxy.go
COPY pgbouncer/ pgbouncer/
COPY start-pgbouncer.sh start-pgbouncer.sh
COPY reload-pgbouncer.sh reload-pgbouncer.sh
COPY start-dbproxy.sh start-dbproxy.sh

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a

from alpine:3.13 AS PGBBUILDER
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

FROM alpine:3.13

RUN apk add -U --no-cache udns libevent openssl ca-certificates

RUN adduser -D -S -s /bin/sh bloxone

RUN mkdir -p /var/run/dbproxy
RUN chown -R bloxone /var/run/dbproxy

COPY --from=pgbbuilder /usr/bin/pgbouncer /usr/bin/pgbouncer
COPY --from=builder /workspace/dbproxy /var/run/dbproxy/dbproxy
COPY --from=builder /workspace/pgbouncer/pgbouncer.template /var/run/dbproxy/pgbouncer.template
COPY --from=builder /workspace/start-pgbouncer.sh /var/run/dbproxy/start-pgbouncer.sh
COPY --from=builder /workspace/reload-pgbouncer.sh /var/run/dbproxy/reload-pgbouncer.sh
COPY --from=builder /workspace/start-dbproxy.sh /var/run/dbproxy/start-dbproxy.sh

RUN chmod +x /usr/bin/pgbouncer
RUN chmod +x /var/run/dbproxy/start-pgbouncer.sh /var/run/dbproxy/reload-pgbouncer.sh

WORKDIR /var/run/dbproxy

USER bloxone

ENTRYPOINT ["/var/run/dbproxy/start-dbproxy.sh"]
