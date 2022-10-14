#!/bin/bash

pg_ctl -D /tmp/$1_db  stop
set -ex

rm -rf /tmp/$1_db
initdb -D /tmp/$1_db -U $3
sed -i '' 's/#wal_level = replica/wal_level = logical/g' /tmp/$1_db/postgresql.conf
sed -i '' "s/#port = 5432/port = $2/g"  /tmp/$1_db/postgresql.conf
pg_ctl -D /tmp/$1_db  start
psql -d postgres  -p $2 -U $3 -c "create database $1;"
