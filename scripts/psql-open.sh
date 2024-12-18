#!/bin/bash

# Print usage
usage() {
    echo "Usage:"
    echo "  $0 <namespace>/<databaseclaim>                 # Opens psql prompt for the specified claim"
    echo "  $0 <namespace>/<databaseclaim> -c '<SQL command>'  # Executes a command on the specified claim"
    echo "  $0 -c '<SQL command>'                          # Executes a command on all databaseclaims"
    exit 1
}

# Function to get the DSN and open a psql prompt or execute a command
open_psql() {
    local namespace="$1"
    local claim_name="$2"
    local psql_command="$3"

    # Get the secret name from the databaseclaim
    secret_name=$(kubectl get databaseclaim "$claim_name" -n "$namespace" -o jsonpath='{.spec.secretName}')

    if [[ -z "$secret_name" ]]; then

        secret_name=$(kubectl get dbroleclaim "$claim_name" -n "$namespace" -o jsonpath='{.spec.secretName}')
        if [[ -z "$secret_name" ]]; then
            echo "Error: Unable to find secret name for dbc: $claim_name in namespace $namespace"
            echo "Error: Unable to find secret name for dbroleclaim: $claim_name in namespace $namespace"
            return 1
        fi

    fi

    # Get the DSN from the secret
    dsn=$(kubectl get secret "$secret_name" -n "$namespace" -o jsonpath='{.data.uri_dsn\.txt}' | base64 --decode)

    if [[ -z "$dsn" ]]; then
        echo "Error: Unable to retrieve DSN from secret $secret_name in namespace $namespace"
        return 1
    fi

    printf "Claim: %s/%s\n" "$namespace" "$claim_name"
    # If a psql command is provided, execute it; otherwise, open a psql prompt
    if [[ -n "$psql_command" ]]; then
        kubectl exec deploy/db-controller -c manager -n db-controller -- psql "$dsn" -c "$psql_command"
    else
        kubectl exec -it deploy/db-controller -c manager -n db-controller -- psql "$dsn"
    fi
}

# Check if arguments are provided correctly
if [[ $# -eq 1 ]]; then
    # If a single argument is provided, assume it is in the form <namespace>/<databaseclaim>
    if [[ "$1" =~ ^(.+)/(.+)$ ]]; then
        namespace=$(echo "$1" | cut -d '/' -f 1)
        claim_name=$(echo "$1" | cut -d '/' -f 2)
        open_psql "$namespace" "$claim_name"
    else
        usage
    fi
elif [[ $# -eq 3 && "$2" == "-c" ]]; then
    # If a claim and command are provided, execute the command on the specified claim
    if [[ "$1" =~ ^(.+)/(.+)$ ]]; then
        namespace=$(echo "$1" | cut -d '/' -f 1)
        claim_name=$(echo "$1" | cut -d '/' -f 2)
        psql_command="$3"
        open_psql "$namespace" "$claim_name" "$psql_command"
    else
        usage
    fi
elif [[ $# -eq 2 && "$1" == "-c" ]]; then
    # If the -c option is provided with a command, execute it on all databaseclaims
    psql_command="$2"
    kubectl get databaseclaim --all-namespaces -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}' | while read -r line; do
        namespace=$(echo "$line" | cut -d '/' -f 1)
        claim_name=$(echo "$line" | cut -d '/' -f 2)
        open_psql "$namespace" "$claim_name" "$psql_command"
    done
else
    usage
fi
