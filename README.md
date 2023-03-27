# db-controller
A controller to create databases using a claim pattern.

This project implements a database controller. It introduces a CRD that allows
someone to create a database claim. That database claim will create a database in
an existing postgres server. Additionally it will create user/password and rotate them.
The code doesn't implement the rotation strategy correctly.

The target code should create:
* a stable role to own the schema and schema objects
* at least two logins that the controller and jump between

Other strategies are possible and may be appropriate to implement.

## Branch: `dbc-v1`
This branch is used for maintaining the original db-controller code base, that is still used by some projects.

**DO NOT MERGE WITH MASTER**
