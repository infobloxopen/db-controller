# sample-postgres

This project helm chart creates a postgres database, it can be used for local development
where a postgres database is needed.

## Installation
The namespace for the helm install should match the
namespace of the db-controller since the secret will be
placed there. 

Create namespace if not already created:
```bash
kubectl create namespace db-controller
```

Create postgres database server:
```bash
cd infobloxopen/db-controller/helm/sample-postgres
helm install -n db-controller postgres .
```
Make sure it is running
```bash
kubectl get pods
NAME                    READY   STATUS    RESTARTS   AGE
postgres-postgresql-0   1/1     Running   0          16h
```

port-forward so that you can reach it from localhost:5432
```bash
kubectl port-forward postgres-postgresql-0 5432:5432
```
