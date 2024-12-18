#!/bin/bash -x


# Optional namespace argument
namespace_filter=${1:-}

# Determine the kubectl command based on whether a namespace filter is provided
if [[ -n "$namespace_filter" ]]; then
  echo "Filtering by namespace: $namespace_filter"
  kubectl_command="kubectl -n $namespace_filter get pods -l persistance.atlas.infoblox.com/dbproxy -o=jsonpath='{range .items[*]}{.metadata.namespace} {.metadata.name}{\"\\n\"}{end}'"
else
  echo "Checking all namespaces"
  kubectl_command="kubectl -A get pods -l persistance.atlas.infoblox.com/dbproxy -o=jsonpath='{range .items[*]}{.metadata.namespace} {.metadata.name}{\"\\n\"}{end}'"
fi

# Get the pods
pods=$(eval "$kubectl_command")

# Iterate over each pod and run the psql command
while read -r namespace pod; do
    echo "Processing pod $pod in namespace $namespace"

     # Execute the SELECT 1 command and capture output
    kubectl -n "$namespace" -c dbproxy exec "$pod" -- sh -c "psql \$(cat /dbproxy/uri_dsn.txt) -c 'SELECT 1'"
    kubectl -n "$namespace" -c dbproxy exec "$pod" -- sh -c "cat /run/dbproxy/pgbouncer.ini"
    kubectl -n "$namespace" -c dbproxy exec "$pod" -- sh -c "cat /run/dbproxy/userlist.txt"

done <<< "$pods"
