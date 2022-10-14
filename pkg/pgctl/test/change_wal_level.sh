#!/bin/bash

set -ex

container_id=`docker ps -q -f "status=running" -f ancestor=$1:$2 -f publish=$3`
docker exec $container_id sed -i 's/#wal_level = replica/wal_level = logical/g' /var/lib/postgresql/data/postgresql.conf
docker restart --time 0 $container_id
sleep 3
wal_level=`docker exec $container_id  cat /var/lib/postgresql/data/postgresql.conf | grep wal_level | cut -d " " -f 3`
[[ $wal_level = logical* ]] && exit 0  || exit 2