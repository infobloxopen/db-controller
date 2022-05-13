# db-controller
This project implements a database controller. It introduces a CRD that allows
someone to create a database claim. That database claim will create a database in
an existing postgres server, or it could create a cloud claim that will attach a
database on demand. Additionally it will create user/password to the database and rotate them.

See [db-controller docs](https://infobloxopen.github.io/db-controller) for more
detail on the db-controller.

## Installation
***TBD***

The installation should create:
* a stable role to own the schema and schema objects
* at least two logins that the controller can jump between

## Documentation
The docs are developed using [mkdocs](https://www.mkdocs.org/) you can run a 
local server to view the rendered content before checking them into git:
```bash
mkdocs serve
```
## Issues
   - The code doesn't implement the rotation strategy correctly.

## Debug
The project was not setup for local debugging. The first time
I ran it it crashed at the viper config load was looking for the
configFile at "/etc/config/config.yaml"

Looking at the helm chart you see that a config file is
loaded, but all the values are left as defaults in this
case.

```yaml
controllerConfig:
  config.yaml: |
    # master credentials source can be 'aws' or 'secret'
    #authSource: "aws"
....
```
The deployment resource passes this mounted as configmap
to the application:

```yaml
          args:
...
            - --config-file=/etc/config/config.yaml
```
For local debugging created a config file:
```bash
cmd/config/config.yaml
```
Added program argument when running it local:
```bash
--config-file=cmd/config/config.yaml
```
After these changes you can now run the db-controller locally:
```bash
API server listening at: 127.0.0.1:53715
debugserver-@(#)PROGRAM:LLDB  PROJECT:lldb-1316.0.9.41
 for x86_64.
Got a connection, launched process /private/var/folders/49/3pxhjsps4fx4q21nkbj1j6n00000gp/T/GoLand/___db_controller (pid = 32109).
{"level":"info","ts":1650469489.2526941,"msg":"loading config"}
```
The db-controller wont go far since we have not loaded the
CRDs yet. Here are the steps to setup the local environment:
```bash
kind create cluster
cd helm/db-controller-crds
helm install db-controller-crds .
```
The install gives errors but the CRDs are installed!! Need
further debugging.

```bash
❯ helm install db-controller-crds .
Error: rendered manifests contain a resource that already exists. Unable to continue with install: CustomResourceDefinition "databaseclaims.persistance.atlas.infoblox.com" in namespace "" exists and cannot be imported into the current release: invalid ownership metadata; label validation error: missing key "app.kubernetes.io/managed-by": must be set to "Helm"; annotation validation error: missing key "meta.helm.sh/release-name": must be set to "db-controller-crds"; annotation validation error: missing key "meta.helm.sh/release-namespace": must be set to "default"
❯ k get crds | grep infoblox
databaseclaims.persistance.atlas.infoblox.com   2022-04-20T16:12:11Z
```
Now with the DatabaseClaim CRD installed you can see that
controller is able to startup:
```bash
....
API server listening at: 127.0.0.1:53852
....
{"level":"info","ts":1650471244.296891,"msg":"Starting server","kind":"health probe","addr":"[::]:8081"}
{"level":"info","ts":1650471244.297001,"logger":"controller.databaseclaim","msg":"Starting EventSource","reconciler group":"persistance.atlas.infoblox.com","reconciler kind":"DatabaseClaim","source":"kind source: *v1.DatabaseClaim"}
{"level":"info","ts":1650471244.297049,"logger":"controller.databaseclaim","msg":"Starting Controller","reconciler group":"persistance.atlas.infoblox.com","reconciler kind":"DatabaseClaim"}
{"level":"info","ts":1650471244.3978748,"logger":"controller.databaseclaim","msg":"Starting workers","reconciler group":"persistance.atlas.infoblox.com","reconciler kind":"DatabaseClaim","worker count":1}
```
To get the reconciler running we will apply a DatabaseClaim:
```bash
cd helm/dbclaim-sample
k create namespace dbclaim-sample
helm install dbclaim-sample --namespace dbclaim-sample .
```
This gets us further but db-controller namespace is not set
in deployment it gets set in environment variable SERVICE_NAMESPACE:
```bash
        - name: {{ .Chart.Name }}-manager
          ports:
          - containerPort: 8443
            name: https
          env:
            - name: SERVICE_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
```
So for local debugging we set environment variable:
```bash
export SERVICE_NAMESPACE=db-controller
kubectl create namespace db-controller
```
Create namespace if not already created:
```bash
kubectl create namespace db-controller
```

Create postgres database server:
```bash
cd helm/sample-postgres
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
Here is what the config.yaml configuration for this
local setup looks like for local setup:

```yaml
sample-connection:
  username: postgres
  host: localhost
  port: 5432
  sslMode: disable
  passwordSecretRef: postgres-postgresql
  passwordSecretKey: postgresql-password
  ```

Here is what the config.yaml configuration for this
local setup looks like for local setup:

This make us get to get the client secret:
```bash
Got a connection, launched process /private/var/folders/49/3pxhjsps4fx4q21nkbj1j6n00000gp/T/GoLand/___2db_controller (pid = 59179).
{"level":"info","ts":1650554772.588421,"msg":"loading config"}
{"level":"info","ts":1650554773.195811,"logger":"controller-runtime.metrics","msg":"Metrics server is starting to listen","addr":"0.0.0.0:8080"}
{"level":"info","ts":1650554773.196074,"logger":"setup","msg":"starting manager"}
{"level":"info","ts":1650554773.196287,"msg":"Starting server","path":"/metrics","kind":"metrics","addr":"[::]:8080"}
{"level":"info","ts":1650554773.196287,"msg":"Starting server","kind":"health probe","addr":"[::]:8081"}
{"level":"info","ts":1650554773.196388,"logger":"controller.databaseclaim","msg":"Starting EventSource","reconciler group":"persistance.atlas.infoblox.com","reconciler kind":"DatabaseClaim","source":"kind source: *v1.DatabaseClaim"}
{"level":"info","ts":1650554773.1964228,"logger":"controller.databaseclaim","msg":"Starting Controller","reconciler group":"persistance.atlas.infoblox.com","reconciler kind":"DatabaseClaim"}
{"level":"info","ts":1650554773.297556,"logger":"controller.databaseclaim","msg":"Starting workers","reconciler group":"persistance.atlas.infoblox.com","reconciler kind":"DatabaseClaim","worker count":1}
{"level":"info","ts":1650554773.2979488,"logger":"controllers.DatabaseClaim","msg":"creating database client","databaseclaim":"dbclaim-sample/databaseclaim-sample"}
{"level":"info","ts":1650554773.298023,"logger":"controllers.DatabaseClaim","msg":"using credentials from secret"}
{"level":"info","ts":1650554782.1417012,"logger":"controllers.DatabaseClaim","msg":"processing DBClaim: databaseclaim-sample namespace: dbclaim-sample AppID: sample-app","databaseclaim":"dbclaim-sample/databaseclaim-sample"}
{"level":"info","ts":1650554794.11509,"logger":"controllers.DatabaseClaim","msg":"creating DB:","databaseclaim":"dbclaim-sample/databaseclaim-sample","database name":"sample_app"}
{"level":"info","ts":1650554794.399101,"logger":"controllers.DatabaseClaim","msg":"database has been created","databaseclaim":"dbclaim-sample/databaseclaim-sample","DB":"sample_app"}
{"level":"info","ts":1650554803.734873,"logger":"controllers.DatabaseClaim","msg":"creating a ROLE","databaseclaim":"dbclaim-sample/databaseclaim-sample","role":"sample_user"}
{"level":"info","ts":1650554803.740366,"logger":"controllers.DatabaseClaim","msg":"role has been created","databaseclaim":"dbclaim-sample/databaseclaim-sample","role":"sample_user"}
{"level":"info","ts":1650554803.740391,"logger":"controllers.DatabaseClaim","msg":"rotating users","databaseclaim":"dbclaim-sample/databaseclaim-sample"}
{"level":"info","ts":1650554803.744957,"logger":"controllers.DatabaseClaim","msg":"creating a user","databaseclaim":"dbclaim-sample/databaseclaim-sample","user":"sample_user_a"}
{"level":"info","ts":1650554803.749567,"logger":"controllers.DatabaseClaim","msg":"user has been created","databaseclaim":"dbclaim-sample/databaseclaim-sample","user":"sample_user_a"}
{"level":"info","ts":1650554803.756309,"logger":"controllers.DatabaseClaim","msg":"creating connection info secret","secret":"databaseclaim-sample","namespace":"dbclaim-sample"}
```
Check that secret is managed:
```bash
kubectl -n dbclaim-sample get secrets
```
The "databaseclaim-sample" is the secret name specified
by the claim:
```bash
NAME                                   TYPE                                  DATA   AGE
databaseclaim-sample                   Opaque                                2      7m30s
default-token-mnrfg                    kubernetes.io/service-account-token   3      22h
sh.helm.release.v1.dbclaim-sample.v1   helm.sh/release.v1                    1      22h
```

### Debug Dynamic Database
The implementation is currently using crossplane but
it could use any provider to overlay over the DatabaseClaim.

We need to configure a dynamic database connection in the
configuration. TODO - this could be omitted and we could
setup everything from the claim. The configuration looks like:

```yaml
# host omitted, allocates database dynamically
dynamic-connection:
  username: root
  port: 5432
  sslMode: require
  engineVersion: 12.8
  passwordSecretRef: dynamic-connection-secret
```

There will be two local debugging environment:

* one will be to setup crossplane on kind cluster and debug as that will be the fastest way to bring up the environment.
  - This will require having a EKS cluster that you can reference for setting up the subnet-group and security group which tied to EKS vpc
  - It will also require setting up a different ProviderConfig right now it is using IRSA and will need local credentials
* second will be to setup crossplane on EKS and this will the closest to production testing

For kind environment run the deploy setps:
***TODO - This is not working yet, see above notes***
```bash
cd deploy
make kind
```

For EKS we assume you have run:
```bash
cd deploy
make eks
kubectl create namespace db-controller    
```

### Problems with Integration Tests

I raised an 
[issue on Kubebuilder community](https://github.com/kubernetes-sigs/kubebuilder/issues/2642)
seems to be problem with MacOS based on my investigation. If you running MacOS you can 
[load the tools locally](https://book.kubebuilder.io/reference/envtest.html?highlight=integration#configuring-envtest-for-integration-tests)
and tests should run.

### Problems with EKS Cluster

This was a problem that was raised about this not working on a cluster that was not
setup using the Deploy project here that is using eksctl but using other tooling.

I looked at the logs:
```bash
kubectl logs -f db-controller-5c475895b4-44cqc -c db-controller-manager  
```

There is this error about connectivity problem:
```json
{
  "level": "error",
  "ts": 1651940225.8717563,
  "logger": "controllers.DatabaseClaim",
  "msg": "error creating database postgresURI postgres://root:@db-controller-dynamic.<some region>.rds.amazonaws.com:5432/sample_app_claim_1?sslmode=require",
  "databaseclaim": "<some namespace>/databaseclaim-dynamic-1",
  "error": "dial tcp 10.0.0.206:5432: connect: connection timed out",
  "stacktrace": "github.com/infobloxopen/db-controller/controllers.(*DatabaseClaimReconciler).Reconcile\n\t/workspace/controllers/databaseclaim_controller.go:116\nsigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller).Reconcile\n\t/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.11.0/pkg/internal/controller/controller.go:114\nsigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller).reconcileHandler\n\t/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.11.0/pkg/internal/controller/controller.go:311\nsigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller).processNextWorkItem\n\t/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.11.0/pkg/internal/controller/controller.go:266\nsigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller).Start.func2.2\n\t/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.11.0/pkg/internal/controller/controller.go:227"
}
```
You can use a command like:
```bash
aws --profile <some profile> --region <some region> rds describe-db-instances  --db-instance-identifier db-controller-dynamic
```

* DBInstances->Endpoint->Address : Should match the endpoint for Postgres msg db-controller-dynamic.<some region>.rds.amazonaws.com
* DBInstances->Endpoint->Port : 5432
* DBInstances->VpcSecurityGroups->VpcSecurityGroupId : "sg-..." Make sure that it allows connectivity to cluster
* DBInstances->DBSubnetGroup->VpcId: Matches EKS Cluster
* DBInstances->DBSubnetGroup->Subnets[]: Matches EKS Cluster Subnets

To check the security group you can use a command like:
```bash
❯ aws --profile <some-profile> --region <some-region> ec2 describe-security-groups --group-ids sg-...
```

* SecurityGroups->IpPermissions[]: Should match the port above

In the above case we had:
```json
{
  "SecurityGroups": [
    {
      "Description": "<some description>",
      "GroupName": "<some group name>",
      "IpPermissions": [
        {
          "FromPort": 10000,
          "IpProtocol": "tcp",
          "IpRanges": [
            {
              "CidrIp": "0.0.0.0/0",
              "Description": "all ips"
            }
          ],
          "Ipv6Ranges": [],
          "PrefixListIds": [],
          "ToPort": 10000,
          "UserIdGroupPairs": []
        }
      ]
    }
  ]
}
```

Note that this Security Group is programmed to accept connection on 10000 while
db-controller is setup to connect to 5432 so that is the cause of the issue.

How would you test this with postgres client?
First lets run a container with psql client in it:
```bash
cd helm/sample-postgres
kubectl create ns postgres
helm install postgres -n postgres .
```
After the database is running we can use the bundled psql client to test our database,
using the postgres dsn from the db-controller log. Note that the password is missing from
the logs and you will need to find the value from the secret in db-controller namespace for
this database as part of your connection string.
```bash
kubectl -n postgres exec -it postgres-postgresql-0 /bin/sh
PGPASSWORD=mleYrpUpOz... createdb -h db-controller-dynamic.<some-region>.rds.amazonaws.com  -U root -p 10000 sample_app_claim_1
psql postgres://root:<password>@db-controller-dynamic.<some region>.rds.amazonaws.com:5432/sample_app_claim_1?sslmode=require
```
psql postgres://root:mleYrpUpOzqvKC7jC8FtKDAbyQb@db-controller-dynamic-bjeevan-connection.ctzctg1lnwhq.us-east-1.rds.amazonaws.com:5432/sample_app_claim_1?sslmode=require
psql postgres://root:mleYrpUpOzqvKC7jC8FtKDAbyQb@db-controller-dynamic-bjeevan-connection.ctzctg1lnwhq.us-east-1.rds.amazonaws.com:10000/sample_app_claim_1?sslmode=require

PGPASSWORD=mleYrpUpOzqvKC7jC8FtKDAbyQb createdb -h db-controller-dynamic-bjeevan-connection.ctzctg1lnwhq.us-east-1.rds.amazonaws.com  -U root -p 5432 sample_app_claim_1
PGPASSWORD=EonpdKzIqu0kIU8WpzmUcEFtfMm  createdb -h db-controller-databaseclaim-dynamic-1.crs4bvboouwp.us-west-1.rds.amazonaws.com -U root -p 5432 sample_app_claim_1
