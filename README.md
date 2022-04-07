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

