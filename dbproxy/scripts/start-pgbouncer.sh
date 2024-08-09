#!/bin/sh -x

openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout dbproxy-server.key -out dbproxy-server.crt -subj "/C=US/CN=dbproxy-server/ST=CA/L=Santa Clara/O=Infoblox/OU=Blox in a Box"
openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 -keyout dbproxy-client.key -out dbproxy-client.crt -subj "/C=US/CN=dbproxy-client/ST=CA/L=Santa Clara/O=Infoblox/OU=Blox in a Box"

pgbouncer -d -v pgbouncer.ini
