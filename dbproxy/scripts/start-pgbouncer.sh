#!/bin/sh -x

# Needed to emit an error for unit tests to catch
set -e
command -v pgbouncer
set +e

# Function to test PostgreSQL connection
test_postgres() {
    local dsn="$1"
    if psql "$dsn" -c 'SELECT 1' >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

# Function to run all connection tests with global timeout
run_connection_tests() {
    local start_time=$(date +%s)
    local timeout=60
    local ssl_ok=0
    local nonssl_ok=0

    while [ $ssl_ok -eq 0 ] || [ $nonssl_ok -eq 0 ]; do
        current_time=$(date +%s)
        if [ $((current_time - start_time)) -ge $timeout ]; then
            echo "Timed out waiting for PostgreSQL after ${timeout} seconds"
            return 1
        fi

        if [ $ssl_ok -eq 0 ] && test_postgres "postgres://localhost:5432/?sslmode=require"; then
            echo "SSL connection successful"
            ssl_ok=1
        fi

        if [ $nonssl_ok -eq 0 ] && test_postgres "postgres://localhost:5432/?sslmode=disable"; then
            echo "Non-SSL connection successful"
            nonssl_ok=1
        fi

        if [ $ssl_ok -eq 0 ] || [ $nonssl_ok -eq 0 ]; then
            echo "Waiting for PostgreSQL connections to be ready..."
            sleep 2
        fi
    done
    return 0
}

# Initial PostgreSQL check
if ! test_postgres "$(cat /dbproxy/uri_dsn.txt)"; then
    echo "Initial PostgreSQL connection failed"
    exit 1
fi
echo "Initial PostgreSQL connection successful!"

# Generate certificates
openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout dbproxy-server.key -out dbproxy-server.crt -subj "/C=US/CN=dbproxy-server/ST=CA/L=Santa Clara/O=Infoblox/OU=Blox in a Box"
openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout dbproxy-client.key -out dbproxy-client.crt -subj "/C=US/CN=dbproxy-client/ST=CA/L=Santa Clara/O=Infoblox/OU=Blox in a Box"

# Start pgbouncer
pgbouncer -d -v pgbouncer.ini

# Test both SSL and non-SSL connections concurrently
if ! run_connection_tests; then
    echo "Connection tests failed"
    exit 1
fi

echo "All PostgreSQL connections are ready!"
