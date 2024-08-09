VERSION ?= 1.17.0

test:
	@echo "No tests"

docker-build:
	$(MAKE) -C ../.. docker-build-pgbouncer

install: pgbouncer
	cd pgbouncer && \
	patch -p 1 < ../docker/pgbouncer.patch && \
	./configure --prefix=/usr --with-udns && \
	make && \
	make install

pgbouncer: pgbouncer-${VERSION}.tar.gz
	tar xvfz pgbouncer-${VERSION}.tar.gz
	mv pgbouncer-${VERSION} $@

pgbouncer-${VERSION}.tar.gz:
	curl -o $@ -L https://pgbouncer.github.io/downloads/files/${VERSION}/pgbouncer-${VERSION}.tar.gz

clean:
	rm -rf pgbouncer pgbouncer-${VERSION}.tar.gz
