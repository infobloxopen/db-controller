#!/bin/bash -x

# This script performs a common maintenance operation of updating
# status fields to version "15.3" from "15".
# Usage: DRY_RUN="--dry-run=server" ./scripts/patch-databases.sh

kubectl get databaseclaim -A -o json | jq -r '.items[] | select((.spec.dbVersion == null or .spec.dbVersion == "") and (.status.activeDB.dbversion != "15.3")) | [.metadata.namespace, .metadata.name] | @tsv' | while IFS=$'\t' read -r namespace name; do
    kubectl get databaseclaim -n "$namespace" "$name" -ojsonpath='{.metadata.namespace}/{.metadata.name}:{.status.activeDB.dbversion}{"\n"}'
    kubectl patch ${DRY_RUN} databaseclaim "$name" -n "$namespace" --type=merge --subresource=status -p '{"status":{"activeDB":{"dbversion":"15.3"}}}'

    kubectl patch ${DRY_RUN} databaseclaim "$name" -n "$namespace" --type=merge -p '{"metadata":{"annotations":{"reconcile-now":"true"}}}'
done
