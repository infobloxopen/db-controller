module github.com/infobloxopen/db-controller

go 1.15

require (
	github.com/armon/go-radix v1.0.0
	github.com/aws/aws-sdk-go v1.42.0
	github.com/crossplane/provider-aws v0.26.0
	github.com/fsnotify/fsnotify v1.5.1
	github.com/go-logr/logr v1.2.0
	github.com/infobloxopen/atlas-app-toolkit v0.24.1
	github.com/lib/pq v1.10.1
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/ory/dockertest/v3 v3.6.4
	github.com/prometheus/client_golang v1.11.0
	github.com/sethvargo/go-password v0.2.0
	github.com/spf13/viper v1.8.1
	golang.org/x/net v0.0.0-20211209124913-491a49abca63 // indirect
	k8s.io/api v0.23.0
	k8s.io/apimachinery v0.23.0
	k8s.io/client-go v0.23.0
	sigs.k8s.io/controller-runtime v0.11.0
	sigs.k8s.io/structured-merge-diff/v4 v4.2.1 // indirect
)
