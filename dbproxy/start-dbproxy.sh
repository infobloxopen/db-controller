#!/bin/sh -x

cd /var/run/dbproxy && pwd && exec ./dbproxy -dbc /etc/config/dbproxy/db-credential -dbp /etc/config/dbproxy/db-password
