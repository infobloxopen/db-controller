# Database Controller Docs

## Introduction
The motivation for this project is declaration of databases in code. In addition, the controller manages credential rotation and zero downtime major database migrations.

This project implements a kubebuilder based controller and a mutating webhook. The controller is responsible for lifecycle of the database. The mutating webhook injects helpful sidecars for communicating with the database.

Refer to the Go Doc for most up to date information: [godoc](https://pkg.go.dev/github.com/infobloxopen/db-controller)

## Operation

The controller will connect to an existing unmanaged database or create/manage a new database created by the controller. Once connected it will create schema specific users and rotate the password on those credentials. There is a [hotload](https://github.com/infobloxopen/hotload) library that is useful for making sure an application watches for these credential changes.

In some situations, it makes sense to offload postgres connections to a sidecar for manageing rotating credentials and connecting to the database. The controller will set this up for you using dbproxy. dbproxy watches for credential changes and bounces internal postgres connectings using a library called pgbouncer. The app should not be aware of any credential or connection changes when using dbproxy. More instructions below on how to use it.

dsnexec sidecar that enables reading sql exec statements from disk and executing them.

## Quick Start

Deploy db-controller. db-controller will need credentials to provision databases in the cloud. For credential and connection only management, it is possible db-controller does not need any credentials.

    make deploy

Create a databaseclaim describing the database. The sourceDataFrom field is used to connect to
an existing database. Otherwise, db-controller will create the database for you.


    apiVersion: persistance.atlas.infoblox.com/v1
    kind: DatabaseClaim
    metadata:
      name: identity
      namespace: default
    spec:
      class: default
      databaseName: mydb
      dbVersion: "15.2"
      enableReplicationRole: false
      enableSuperUser: false
      minStorageGB: 0
      secretName: identity-dsn
      shape: ""
      sourceDataFrom:
        database:
          dsn: postgres://root@name.hostname.region.rds.amazonaws.com:5432/mydb?sslmode=require
          secretRef:
            name: mydb-master
            namespace: default
        type: database
      tags:
      - key: Environment
        value: dev-cluster
      type: aurora-postgresql
      useExistingSource: false
      userName: identity-api

To use the dbproxy mutating webhook, add labels to the pod:

    apiVersion: v1
    kind: Pod
    metadata:
      name: dbproxy-test
      namespace: default
      labels:
        persistance.atlas.infoblox.com/databaseclaim: "identity"
        persistance.atlas.infoblox.com/class: "default"
      containers:
        - name: client
          image: postgres
          env:
            - name: PGCONNECT_TIMEOUT
              value: "2"
          command:
            - /bin/sh
            - -c
            - |
              set -x
              until timeout 10 psql -h localhost -c 'SELECT 1'; do
                echo "Waiting for sidecar to be ready..."
                sleep 3
              done
              echo "Connection successful!"
              sleep 10000

## Requirements
| Requirements           | Description                                                                                   |
|------------------------|-----------------------------------------------------------------------------------------------|
| controller             | Establish a pattern (and possible code) for apps to reload connection information from a file |
| mutatingwebhook        | Establish a pattern for mounting a secret that contains connection information in a secret    |
| controller             | Support dynamically changing the connection information                                       |
| controller             | CR Change create new database instance and migrate data and delete infrastructure CR          |
| controller             | Migration should with Canary pattern once tests pass all traffic to the new database          |

## Demo

### Configuration
```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#fffcbb', 'lineColor': '#ff0000', 'primaryBorderColor': '#ff0000'}}}%%

flowchart TB
subgraph shared
app1-->claim1
app2-->claim2
end
subgraph dedicated
app3-->claim3
end

claim1 --> rdsinstance1[(RDS Instance 1)]
claim2 --> rdsinstance1
claim3 --> rdsinstance2[(RDS Instance 2)]
```

### Policy Demo
```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#fffcbb', 'lineColor': '#ff0000', 'primaryBorderColor': '#ff0000'}}}%%

flowchart TB
subgraph shared policy
reclaim1[shared ReclaimPolicy] --> shared[delete]
end
subgraph default policy
reclaim2[default ReclaimPolicy] --> dedicated[retain]
end

reclaim1 --> rdsinstance1[(RDS Instance 1)]
reclaim2 --> rdsinstance2[(RDS Instance 2)]
```

## Service Architecture
The first component is a db-controller service that will be responsible 
for reading DBClaims and then provisioning the database and user(s) 
for that database. The service will also publish this information 
into kubernetes secrets, so they can propagate to the other components.  
The db-controller is also responsible for rotating the user password 
based on the password related config values, and updating the user 
password in the associated database and kubernetes secrets store.

The db-controller will support config values and master 
DB instance connection information, defined in a configMap, 
as well as the references to the kubernetes basic auth secrets 
that contain the root password for each database instance.
At startup of the db-controller, the db-controller will scan 
any existing DBClaims in its datastore and use the ConnectionInfoUpdatedAt 
timestamp as the starting point for the passwordRotation timers.

The second component is to modify the pod definition of the application 
service to support secrets in files.  In this model, each key within 
the secret will be mapped into a separate file in the mountPath of the volume

The third component is a database proxy package for the application service. This proxy 
will be used in place of the existing database driver. It is configured to register 
the real driver for the database type,so that it can forward all the calls to this 
driver. The proxy driver will also be configured with a strategy for where it 
can find and monitor the secrets associated with the connection string. When the 
proxy detects that any of the connection string information has changed 
it can close any open connections that the application has with the real database 
driver. When the proxy closes the connections, it will trigger the connection 
pool to flush any bad connections, and allow the client application to reconnect. The 
new database connections for the application will then transparently use the 
new connection string information provided by the proxy.

```mermaid
  sequenceDiagram
    autonumber
    CD ->> K8S: apply manifest
    K8S ->> CD: ok
    rect rgb(0, 255, 0, .3)
        loop
            K8S->>db-controller: new DBClaim
            rect rgba(0, 0, 255, .2)
                Note over db-controller: Check for Dynamic Database Creation 
                db-controller->>db-controller: Instance exists?
                db-controller->>infra-operator: No -> CloudDatabaseClaim
                loop Wait for Database Instance
                    infra-operator->>infra-operator: provision instance
                end
                loop Wait for Database Connection Secret
                    db-controller->>db-controller: Connection Secret exists?
                end
            end
            db-controller->>RDS: DB exists?
            RDS->>db-controller: no
            db-controller->>RDS: create DB
            RDS->>db-controller: ok
            db-controller->>RDS: create user &<br/> set permissions
            RDS->>db-controller: ok
            db-controller->>K8S: create secret with<br/> connection string
            K8S->>db-controller: ok
        end
    end
    rect rgb(0, 255, 0, .3)
        loop
            Kubelet->>K8S: get secrets
            K8S->>Kubelet: secrets
            Kubelet->>tempfs: store
            tempfs->>Kubelet: ok
        end
    end
    rect rgb(0, 255, 0, .3)
        loop
            App->>tempfs: read
            tempfs->>App: ok
        end
    end
```

## Data Model
The database proxy will include a DatabaseClaim Custom Resource. In the first
version of the application the DatabaseClaim contained information related 
to a specific instance of a database. In the next major release the DatabaseClaim
has information to help an infrastructure operator select a database for the
client application. This pattern will allow us to make the database selection be
pre-provisioned or dynamic on demand. It will also allow the database selction to
be multi-cloud e.g. AWS or Azure.

Secrets created by db-controller follow this format:

    apiVersion: v1
    data:
      database: b64enc(mydb)
      dsn.txt: b64enc(host=dbproxy-test.default.svc port=5432 user=myuser_a password=&#xmetlife1s35gj dbname=mydb sslmode=disable)
      hostname: b64enc(dbproxy-test.default.svc)
      password: b64enc(&#xmetlife1s35gj)
      port: b64enc(5432)
      sslmode: b64enc(disable)
      uri_dsn.txt: b64enc(postgres://myuser_a:&#xmetlife1s35gj@dbproxy-test.default.svc:5432/mydb?sslmode=disable)
      username: b64enc(myuser_a)
    kind: Secret
    metadata:
      labels:
        app.kubernetes.io/managed-by: db-controller
      name: identity-dsn
      namespace: default
      ownerReferences:
      - apiVersion: persistance.atlas.infoblox.com/v1
        blockOwnerDeletion: true
        controller: true
        kind: DatabaseClaim
        name: dbproxy-test
        uid: 23042801-8076-435c-a9f6-a9bec9ac9c8b
      resourceVersion: "1801545567"
      uid: db5330b7-564d-4cc1-bf47-d907e7307465
    type: Opaque

DatabaseClaim will read the set of CR’s describing the DatabaseClaim and store 
them in-memory. In the case of dynamic database provisioning
a CR is created to request infrastructure operator to create the database instance.
db-controller will check the CR satus and to make sure that the database is provisioned
and information about it is available in a secret refrenced by the CR.

The db-controller will listen for any changes to the DatabaseClaim CR’s and 
remain in-sync with the K8 definitions. In the case of dynamic database provisioning
a change will cause a new infrastructure CR to be created, when the new database
instance is created we should migrate all password, data and connection to it and delete
the old infrastructure CR. It will be infrastructure operator concern if the cloud
database instances are deleted immediately or there is a higher level operations
workflow to reclaim them. We can break the database migration into phases and deliver
just new infrastructure CR on change, this might be fine for development environments
and work on the full feature for production. There are also advanced use case like 
canary where where an new release of the application creates the new database, tests
the migration and once everything is working all traffic is migrated away from the
old database and it can be reclaimed.

The following shows the mapping of the DatabaseClaim to a CloudDatabase.
The CloudDatabaseClaim could be custom or use a infrastructure provider like
[crossplane resource composition](https://github.com/crossplane/crossplane/blob/master/design/design-doc-composition.md#resource-composition).
The DatabaseClaim could also be mapped to a Cloud provider like
[AWS ACK](https://github.com/aws-controllers-k8s/community), providing multi-cloud support by transforming
DatabaseClaim to a specific cloud provider CR.
In the case of AWS you would leverage
[ACK RDS Provider](https://github.com/aws-controllers-k8s/rds-controller).

```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#fffcbb', 'lineColor': '#ff0000', 'primaryBorderColor': '#ff0000'}}}%%
erDiagram
    DatabaseClaim ||--o{ ClaimMap : ""
    DatabaseClaim ||--|| CloudDatabaseClaim : ""
    CloudDatabaseClaim ||--o{ ClaimMap : ""
```

This the deploying with the CR scheme:
**Relationship of a DatabaseClaim to a Secret and a Pod:**
```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#fffcbb', 'lineColor': '#ff0000', 'primaryBorderColor': '#ff0000'}}}%%
stateDiagram
  direction LR
  Application --> DatabaseClaim : Manifest
  DatabaseClaim --> CloudDatabaseClaim : Claim Map
  CloudDatabaseClaim --> rdsInstance : create instance
```

In this example db-controller-postgres-con-some-app Secret that is written by the
infrastructure operator has the connection string and password information for the 
db-controller to manage the instance, similar to the pre-provisioned use case.

### ConfigMap
The db-controller will consume a configMap that contains global config values.
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: db-controller-config
  namespace: db-controller-namespace
data:
  config.yaml: |
    passwordConfig:
      passwordComplexity: enabled
      minPasswordLength: "15"
      passwordRotationPeriod: "60"
    atlas:
      username: root
      host: some.service
      port: 5432
      sslMode: disable
      passwordSecretRef: atlas-master-password
      passwordSecretKey: password
    atlas.recyclebin:
      username: root
      host: some.other.service
      port: 5412
      sslMode: disable
      passwordSecretRef: atlas-recyclebin-password
    athena:
      username=root
      host=some.service
      port=5432
      sslMode: disable
      passwordSecretRef=athena-master-password
    athena.hostapp:
      username=root
      host=some.service
      port=5432
      sslMode: require
      passwordSecretRef=athena-hostapp-password
```

* authSource: Determines how database host master password is retrieved. The possible values are "secret" and "aws". In the case of "secret" the value from the passwordSecretRef Secret is used for the password. In the case of "aws" the RDS password is retrieved using AWS APIs and db-controller should have IAM credentials to make the necessary calls.
* region: The region where dynamic database is allocated
* passwordComplexity: Determines if the password adheres to password complexity rules or not.  Values can be enabled or disable.  When enabled, would require the password to meet specific guidelines for password complexity.  The default value is enabled.  Please see the 3rd party section for a sample package that could be used for this.
* minPasswordLength: Ensures that the generated password is at least this length.  The value is in the range [15, 99].  The default value is 15.  Upper limit is Postgresql max password length limit.
* passwordRotationPeriod: Defines the period of time (in minutes) before a password is rotated.  The value can be in the range [60, 1440] minutes.  The default value is 60 minutes.

* defaultMasterPort: Value of MasterPort
* defaultMasterUsername: Value of MasterUsername
* defaultSslMode: Value of sslMode
* defaultShape: Value of Shape if not specified in DatabaseClaim
* defaultMinStorageGB: Value of MinStorageGB if not specified in DatabaseClaim 
* defaultEngineVersion: Value of EngineVersion if not specified in DatabaseClaim
* defaultDeletionPolicy: The DeletionPolicy for CloudDatabase, possible values: delete, orphan
* defaultReclaimPolicy: Used as default value for ReclaimPolicy for CloudDatabase, possible values are "delete" and "retain"

The configMap and credential secrets must be mounted to volumes within the 
pod for the db-controller.  This ensures that when the keys are updated, the 
projected keys will also be updated in the pod volume. Below is a sample
Pod spec with secret and volumes configured:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: db-controller
spec:
  containers:
    - name: db-controller
      image: infobloxopen/db-controller-abcdefg123
      command: [ "/main" ]
      volumeMounts:
      - name: config-volume
        mountPath: /etc/config
      - name: root-user
        mountPath: "/etc/credentials"
        readOnly: true
  volumes:
    - name: config-volume
      configMap:
        name: db-controller-config
    - name: athena-root-user
      secret:
        secretName: athena-hostapp-password
  restartPolicy: Never
```
The root user passwords will be stored in kubernetes secrets and the name 
must match the passwordSecretRef value in the configMap. Here is sample Secret
that match above example:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: athena-hostapp-password
type: Opaque
stringData:
  password: t0p-Secr3t!@
```

### DatabaseClaim Custom Resource
The DatabaseClaim custom resource describes the connection information for 
a database instance.  It includes the following properties:

## Secrets
During the processing of each DatabaseClaim, the db-controller will generate the 
connection info and also create a secret with the relevant information. The secret 
will have the same name as the DatabaseClaim, and contain keys that match the 
values of the properties under the DatabaseClaim *status.connectionInfo* property.

The keys in the secret are shown below:
* "dsn.txt" : postgres dsn string value specified by DatabaseClaim
* "uri_dsn.txt" + dsn : url path value is "uri_" prefix added to dsn
* "hostname" : postgres host of dsn
* "port" : port used for connecting to the database
* "database" : postgres database created on host
* "username" : username to access the database
* "password" : password to access the database
* "sslmode" : SSL Mode value as specified by dsn spec

### Using Secrets as Files
Modify the Pod definition, for the service that you will add the proxy package, to 
add a volume under *.spec.volumes[]*. Name the volume anything, and have 
a *.spec.volumes[].secret.secretName* field equal to the name of the Secret 
object, which in this case is also the name of the DatabaseClaim.

Add a *.spec.containers[].volumeMounts[]* to each container that needs 
the secret. Specify *.spec.containers[].volumeMounts[].readOnly = true* 
and *.spec.containers[].volumeMounts[].mountPath* to an unused directory 
name where you would like the secrets to appear.

Modify your image or command line so that the proxy package looks 
for files in that directory. Each key in the secret data map becomes the 
filename under mountPath.

### Example Secret Config
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
spec:
  containers:
  - name: mypod
    image: redis
    volumeMounts:
    - name: foo
      mountPath: "/etc/connection"
      readOnly: true
  volumes:
  - name: foo
    secret:
      secretName: mysecret
```

**Relationship of a DatabaseClaim to a Secret and a Pod:**
```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#fffcbb', 'lineColor': '#ff0000', 'primaryBorderColor': '#ff0000'}}}%%
stateDiagram
  direction LR
  Pod --> Secret : references
  Secret --> DatabaseClaim : ownedBy
```

## API
***N/A***

## Implementation

The DatabaseClaim
Status is where keep the state used by the reconciler. The important part is:

* ConnectionInfo[]: connection info about the database that is projected into a client accessible secret

In this part we will look at the UpdateStatus function sequence and the
proposed changes to support dynamic database creation while leaving much of
the working db-controller to interoperate:

```mermaid
  sequenceDiagram
      rect rgba(255, 0, 255, .2) 
        par New Dynamic Database Binding
          getClient->>getHost: dbClaim
          getHost->>getHost: Spec.Host
          getHost->>getHost: Yes
          Status->>getHost: Status.CloudDatabase.Host == "" ?
          getHost->>getHost: Yes        
          getHost->>createCloudDatabase: dbClaim
          createCloudDatabase->>createCloudDatabase: Wait for resource ready
          createCloudDatabase->>getHost: CloudDatabase
          getHost->>Status: Status.CloudDatabase.{Name, Host, Port, User, Password}
          getHost->>getClient: {Host, Port, User, Password}
        end
      end
      rect rgba(255, 255, 0, .2) 
        par Existing Static Database Binding
          getHost->>getHost: Spec.Host
          getHost->>getHost: No
          getProvisionedHost->>getHost: {Host, Port, User, Password}
          getHost->>getClient: {Host, Port, User, Password}
        end
    getClient->>Status: Status.ConnectionInfo.Host
    getClient->>Status: Status.ConnectionInfo.Port
    getClient->>Status: Status.ConnectionInfo.SSLMode
    getClient->>Status: Status.ConnectionInfo.ConnectionInfoUpdatedAt
    getClient->>DBClientFactory: host, port, user, password, sslmode
    DBClientFactory->>getClient: dbClient
    getClient->>UpdateStatus: dbClient
    UpdateStatus->>GetDBName: dbClaim
    GetDBName->>UpdateStatus: Spec.DatabaseName or Spec.DBNameOverride            
```

### Lifecycle
***TODO - Document the full lifecycle not just for Dynamic Host***

The dynamic host allocation will have the following lifecycle:

The create and update lifecycles are fairly simple:
```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#fffcbb', 'lineColor': '#ff0000', 'primaryBorderColor': '#ff0000'}}}%%
stateDiagram-v2
CDH: Create Dynamic Host
CDU: Update Dynamic Host

[*] --> DbClaim
state DbClaim {
    Create --> CDH
    --
    Update --> CDU
}
```

The delete lifecycle is more complex shown below:

```mermaid
%%{init: {'theme': 'base', 'themeVariables': { 'primaryColor': '#fffcbb', 'lineColor': '#ff0000', 'primaryBorderColor': '#ff0000'}}}%%
stateDiagram-v2
DCD: Delete CloudDatabase
DDC: Delete DatabaseClaim

[*] --> DbClaim
state DbClaim {
    Delete --> Finalizer
    state Finalizer {
        state check_host <<choice>>
        state check_label <<choice>>
        [*] --> check_host
        check_host --> check_label: if Claim.Host == ""
        check_host --> [*] : if Claim.Host != ""
        check_label --> DCD: if ReclaimPolicy == Delete &&<br/> DbClaims using CloudDB == 1
        check_label --> [*] : if ReclaimPolicy == Retain
        DCD --> [*]
    }
    Finalizer --> DDC
    DDC --> [*]
}
```

The sharing of database by multiple applicaitons (Claims) by Crossplane
[is an open issue](https://github.com/crossplane/provider-gcp/issues/157).
The db-controller creates the CloudDatabase Claim and the associated
connection secret is created in the db-controller namespace.

[Crossplane](https://github.com/crossplane/crossplane-runtime/issues/21) started 
with the intent of using a similar
[pattern used by pv and pvc](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#reclaiming)
in Kubernetes. The ReclaimPolicy had a lot off issues and it
[got backed out](https://github.com/crossplane/crossplane-runtime/issues/179) and
it got renamed to DeletionPolicy on just the CloudDatabase resource.

DatabaseClaim resources are namespaced, but the
CloudDatabase resources are dynamically provisioned as cluster scoped.
A namespaced resource cannot, by design, own a cluster scoped resource.
This could be an issue in how we manage lifecycle, see
[kubernetes](https://github.com/kubernetes/kubernetes/issues/65200) and
[crossplane](https://github.com/crossplane/provider-gcp/issues/99) references.

We need some more research PoC to figure out if
we can use 
[Kubernetes Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/)
on DatabaseClaim to honor a ReclaimPolicy with values of delete and retain.
Then have ReclaimPolicy delete a dynamically allocated database
that is no longer shared. The reason for the 
[deprecation of ReclaimPolicy from CrossPlane](https://github.com/crossplane/crossplane-runtime/issues/179#issuecomment-637462056)
needs to be examined in the light of our requirements and why Crossplane
does not offer a 
[Shared Database Pattern](https://github.com/crossplane/provider-gcp/issues/157).
A argument made [here](https://github.com/crossplane/crossplane/issues/1229#issuecomment-585382715) 
suggest that in most cases sharing infrastructure within namespace 
boundaries with a  single claim is valid, as namespace == application 
team boundary. Does forcing applciations to share namespace for
database sharing cause any issues, e.g. RBAC?

The Crossplane Infrastructure Operator, has a
resource DeletionPolicy which specifies what will happen 
to the underlying external cloud resource when this managed resource is 
deleted - either "Delete" or "Orphan" the external resource. This will
be managed by defaultDeletionPolicy.

Another features we have is to share resources like RDS among
different clusters that are deployed. We don't have a design yet on
how to best support this requirement.
Crossplane Infrastructure Operator supports multiple clusters,
using an admin cluster and then resources like CloudDatabases
are pushed to the managed clusters,
[Crossplane workload reference](https://blog.crossplane.io/crossplane-v0-7-schedule-workloads-to-any-kubernetes-cluster-including-bare-metal/).
Crossplane also have an [observerable pattern](https://github.com/crossplane/provider-gcp/issues/157)
to deal with this coupling between clusters trying to share resources.

### Deployment
When API is updated need to update the crd definition
and helm chart that updates it. The following make target
is setup to do this:
```bash
make update_crds
```
### Authentication
### Authorization
This db-controller service will require access to credentials that have
permissions to provision the database as well as access to the master
user account to manage users for the target database.

### Ingress
***N/A***

### Audit
***N/A***

### Logging
***TBD***

### Alerting
Prometheus alerts based on errors from the db-controller.

### Performance
***N/A***

### Security
***TBD***

### Third-Party Software
| Name/Version | License      | Description/Reason for usage                                                         |
|--------------|--------------|--------------------------------------------------------------------------------------|
| fsnotify     | BSD-3-Clause | Golang cross platform file system notifications                                      |
| go-password  | MIT License  | Generation of random passwords with provided requirements as described by  AgileBits |

### Metrics/Telemetry
* The following metrics should be exposed:
* Total database users created
* Time to create a new database user
* Total database provisioning errors
* Total database users created with errors
* Total passwords rotated
* Time to rotate user password
* Total password rotated with error
* Total DBClaim load errors
* Total DBClaims loaded
* Time to load a DBClaim

### Rate Limits
***N/A***

### UX/UI
***N/A***

### High Availability/Resiliency
***TBD***

### Scalability
The db-controller service needs to be able to scale out based on demand.

### Backup
***N/A***

### Disaster Recovery
***TBD***

### Data Retention
***N/A***

## Testing
### Test Plan
***TBD***

### Test Matrix
***TBD***

## Migration
***N/A***

## Deployment Architecture
***TBD***

## Upgrades
***N/A***

## Service Dependencies

This service depends on these services:
* kube-api-server
* [SpaceController](https://github.com/seizadi/space-controller) or similar operator: updates the RDS root passwords that the db-controller can use

These services depend on this service:
***N/A***

## Service Killers

| Service Name   | Impact                                                                        |
|----------------|-------------------------------------------------------------------------------|
| RDS            | Can't create new databases, can't rotate passwords. can't process new claims. |
| kube-apiserver | full down.                                                                    |

## Data Analytics
***N/A***

## References
[Hotload database driver](https://github.com/infobloxopen/hotload)






