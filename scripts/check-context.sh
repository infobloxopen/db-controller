#!/bin/bash

# Approved contexts
approved_contexts=("box-3" "kind" "gcp-ddi-dev-use1")

# Get the current kubectl context
current_context=$(kubectl config current-context)

# Check if the current context is one of the approved values
is_approved=false
for context in "${approved_contexts[@]}"; do
    if [[ "$current_context" == "$context" ]]; then
        is_approved=true
        break
    fi
done

if $is_approved; then
    echo "Current context is valid: $current_context" >&2
else
    echo "Error: Current context is invalid: $current_context" >&2
    exit 1
fi

# if current context has gcp in it, export HELM_SETFLAGS
if [[ "$current_context" == *"gcp"* ]]; then
    echo "$HELM_SETFLAGS -f helm/db-controller/minikube_gcp.yaml"
fi
