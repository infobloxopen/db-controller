#!/bin/bash

# Pass no arguments to go through every databaseclaim in every namespace
# Usage: psql-sql-open.sh [<namespace>/<databaseclaim name>]

# Function to get the DSN and find table owners
get_table_owners() {
    local namespace="$1"
    local claim_name="$2"

    # Get the secret name from the databaseclaim
    secret_name=$(kubectl get databaseclaim "$claim_name" -n "$namespace" -o jsonpath='{.spec.secretName}')

    if [[ -z "$secret_name" ]]; then
        echo "Error: Unable to find secret name for $claim_name in namespace $namespace"
        return 1
    fi

    # Get the DSN from the secret
    dsn=$(kubectl get secret "$secret_name" -n "$namespace" -o jsonpath='{.data.uri_dsn\.txt}' | base64 --decode)

    if [[ -z "$dsn" ]]; then
        echo "Error: Unable to retrieve DSN from secret $secret_name in namespace $namespace"
        return 1
    fi
    printf "\nNamespace: $namespace\nDatabaseClaim: $claim_name\n"
    printf "Created: $(kubectl get databaseclaim "$claim_name" -n "$namespace" -o jsonpath='{.metadata.creationTimestamp}')\n"
    # Run psql command to find the owner of tables
    kubectl exec deploy/db-controller -n db-controller -c manager -- psql "$dsn" -c '\dt' \
        | awk '{if(NR>2 && $4!="")print $7}' | sort | uniq -c | sort -nr
}

# Check if an argument was provided
if [[ $# -eq 1 ]]; then
    # If an argument is provided, assume it is in the form <namespace>/<databaseclaim name>
    namespace=$(echo "$1" | cut -d '/' -f 1)
    claim_name=$(echo "$1" | cut -d '/' -f 2)
    get_table_owners "$namespace" "$claim_name"
else
    # If no arguments provided, iterate through all databaseclaims in all namespaces
    kubectl get databaseclaim --all-namespaces -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}' | while read -r line; do
        namespace=$(echo "$line" | cut -d '/' -f 1)
        claim_name=$(echo "$line" | cut -d '/' -f 2)
        get_table_owners "$namespace" "$claim_name"
    done
fi
