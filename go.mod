module github.com/infobloxopen/db-controller

go 1.15

require (
	github.com/armon/go-radix v0.0.0-20180808171621-7fddfc383310
	github.com/fsnotify/fsnotify v1.4.7
	github.com/go-logr/logr v0.1.0
	github.com/lib/pq v1.9.0
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	github.com/sethvargo/go-password v0.2.0
	github.com/spf13/viper v1.7.1
	k8s.io/api v0.17.2
	k8s.io/apimachinery v0.17.2
	k8s.io/client-go v0.17.2
	sigs.k8s.io/controller-runtime v0.5.0
)
