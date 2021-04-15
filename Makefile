# Docker hub repo
REGISTRY ?= infoblox
# image name
IMAGE_NAME ?= db-controller
# image tag
TAG ?= latest
# Image URL to use all building/pushing image targets
IMG ?= ${REGISTRY}/${IMAGE_NAME}:${TAG}
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

CMD := "cmd/manager"

#configuration for helm
CWD=$(shell pwd)
KUBECONFIG ?= ${HOME}/.kube/config
HELM_IMAGE := infoblox/helm:3.2.4-5b243a2
CHART_VERSION ?= $(TAG)
DB_CONTROLLER_CHART := helm/${IMAGE_NAME}
CRDS_CHART := helm/${IMAGE_NAME}-crds
CHART_FILE := $(IMAGE_NAME)-$(CHART_VERSION).tgz
CHART_FILE_CRD := $(IMAGE_NAME)-crds-$(CHART_VERSION).tgz
HELM ?= docker run \
	--rm \
	--net host \
	-w /pkg \
	-v ${CWD}:/pkg \
	-v ${KUBECONFIG}:/root/.kube/config \
	-e AWS_REGION \
	-e AWS_ACCESS_KEY_ID \
	-e AWS_SECRET_ACCESS_KEY \
	-e AWS_SESSION_TOKEN \
	 ${HELM_IMAGE}

HELM_ENTRYPOINT ?= docker run \
	--rm \
	--entrypoint "" \
	--net host \
	-w /pkg \
	-v ${CWD}:/pkg \
	-v ${KUBECONFIG}:/root/.kube/config \
	-e AWS_REGION \
	-e AWS_ACCESS_KEY_ID \
	-e AWS_SECRET_ACCESS_KEY \
	-e AWS_SESSION_TOKEN \
	 ${HELM_IMAGE} sh -c

DBCTL_NAMESPACE ?= db-controller-namespace
# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: manager

# Run tests
test: kubebuilder generate fmt vet manifests
	go test -cover -v ./...
# Run test in docker db test can't because separate postgres pod should be installed
docker-test: generate fmt vet manifests
	docker build \
		--build-arg REPO="${REPO}" \
		-f build/Dockerfile.test.env \
		. -t test-image

# Build manager binary
manager: generate fmt vet
	go build -o bin/manager $(CMD)/main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./$(CMD)/main.go

# Install CRDs into a cluster
install: manifests
	kustomize build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests
	kustomize build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	cd config/manager && kustomize edit set image controller=${IMG}
	kustomize build config/default | kubectl apply -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Build the docker image
docker-build: generate fmt vet manifests
	docker build \
		--build-arg CMD="${CMD}" \
		-f build/Dockerfile \
		-t ${IMG} \
		.

# Push the docker image
docker-push:
	docker push ${IMG}

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.5.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# find or download kubebuilder
# download kubebuilder if necessary
kubebuilder:
ifeq (, $(shell which kubebuilder))
	@{ \
	set -e ;\
	os=$(shell go env GOOS) ;\
	arch=$(shell go env GOARCH) ;\
	echo $$arhch ;\
	curl -L https://go.kubebuilder.io/dl/2.3.1/$${os}/$${arch} | tar -xz -C /tmp/ ;\
	(sudo mv /tmp/kubebuilder_2.3.1_$${os}_$${arch}/ /usr/local/kubebuilder) ;\
	export PATH=$$PATH:/usr/local/kubebuilder/bin ;\
	}
KUBEBUILDER_ASSETS=$(GOBIN)/kubebuilder
else
KUBEBUILDER_ASSETS=$(shell which kubebuilder)
endif

create-namespace:
	kubectl create namespace ${DBCTL_NAMESPACE}

delete-namespace:
	kubectl delete namespace ${DBCTL_NAMESPACE}

install-crds:
	helm template db-controller-crd helm/db-controller-crds/ --namespace=${DBCTL_NAMESPACE} |kubectl apply -f -

uninstall-crds:
	helm template db-controller-crd helm/db-controller-crds/ --namespace=${DBCTL_NAMESPACE} |kubectl delete -f -

deploy-controller:
	helm template db-controller ./helm/db-controller/ --namespace=${DBCTL_NAMESPACE} -f helm/db-controller/minikube.yaml | kubectl apply -f -

uninstall-controller:
	helm template db-controller ./helm/db-controller/ --namespace=${DBCTL_NAMESPACE} -f helm/db-controller/minikube.yaml | kubectl delete -f -

deploy-samples:
	helm template dbclaim-sample helm/dbclaim-sample --namespace=${DBCTL_NAMESPACE} | kubectl apply -f -

uninstall-samples:
	helm template dbclaim-sample helm/dbclaim-sample --namespace=${DBCTL_NAMESPACE} | kubectl delete -f -

deploy-all: create-namespace install-crds deploy-controller

uninstall-all: uninstall-controller uninstall-crds delete-namespace

build-properties:
	@sed 's/{CHART_FILE}/$(CHART_FILE)/g' build.properties.in > build.properties

build-properties-crd:
	@sed 's/{CHART_FILE}/$(CHART_FILE_CRD)/g' build.properties.in > crd.build.properties

build-chart:
	${HELM_ENTRYPOINT} "helm package ${DB_CONTROLLER_CHART} --version ${CHART_VERSION}"

build-chart-crd:
	${HELM_ENTRYPOINT} "helm package ${CRDS_CHART} --version ${CHART_VERSION}"

push-chart:
	${HELM} s3 push ${CHART_FILE} infobloxcto

push-chart-crd:
	${HELM} s3 push ${CHART_FILE_CRD} infobloxcto

clean:
	@rm -f *build.properties || true
	@rm -f *.tgz || true
	sudo rm -rf /usr/local/kubebuilder
