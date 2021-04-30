# db-controller
DB Controller is a k8s operator that creates database instances using a claim pattern. 
Users create a DbClaim object that the operator observes. If the required database is
created if required. Then, a username & password is created for that database. The
connection info is then pushed into a secret for the app to use. On a periodic basis, the
operator also rotates the username/password and puts that info into the required secret.
