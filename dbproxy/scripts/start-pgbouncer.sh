#!/bin/sh -x

until  timeout 10 psql "$(cat /dbproxy/uri_dsn.txt)" -c 'SELECT 1'; do
  echo "Waiting for PostgreSQL to be ready..."
  sleep 1
done
echo "PostgreSQL is ready!"

openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout dbproxy-server.key -out dbproxy-server.crt -subj "/C=US/CN=dbproxy-server/ST=CA/L=Santa Clara/O=Infoblox/OU=Blox in a Box"
openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout dbproxy-client.key -out dbproxy-client.crt -subj "/C=US/CN=dbproxy-client/ST=CA/L=Santa Clara/O=Infoblox/OU=Blox in a Box"

pgbouncer -d -v pgbouncer.ini

until timeout 10 psql -h localhost -c 'SELECT 1'; do
  echo "Waiting for PostgreSQL to be ready..."
  sleep 1
done
echo "PostgreSQL is ready!"
