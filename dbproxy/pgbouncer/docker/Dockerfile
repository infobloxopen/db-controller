from alpine:3.16
ARG VERSION=1.17.0

ADD /pgbouncer.patch /tmp/pgbouncer.patch

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

ENTRYPOINT ["/usr/bin/tail", "-f", "/dev/null"]
