# Docker hub repo
REGISTRY ?= ghcr.io/infobloxopen
# image name
IMAGE_NAME ?= db-controller
DBPROXY_IMAGE_NAME ?= dbproxy
# commit tag info from git repo
GIT_COMMIT	   := $(shell git describe --always || echo pre-commit)
# image tag
TAG ?= ${GIT_COMMIT}
# Image Path to use all building/pushing image targets
IMG_PATH ?= ${REGISTRY}/${IMAGE_NAME}
DBPROXY_IMG_PATH ?= ${REGISTRY}/${DBPROXY_IMAGE_NAME}
GOBIN := ~/go/bin
K8S_VERSION := 1.22.1
ACK_GINKGO_DEPRECATIONS := 1.16.5

SHELL := $(shell which bash)

CMD := "cmd/manager"

#configuration for helm
CWD=$(shell pwd)
KUBECONFIG ?= ${HOME}/.kube/config
HELM_IMAGE := infoblox/helm:3.2.4-5b243a2
CHART_VERSION ?= $(TAG)
APP_VERSION ?= ${TAG}
DB_CONTROLLER_CHART := helm/${IMAGE_NAME}
DB_CONTROLLER_CHART_YAML := ${DB_CONTROLLER_CHART}/Chart.yaml
DB_CONTROLLER_CHART_IN := ${DB_CONTROLLER_CHART}/Chart.in
CRDS_CHART := helm/${IMAGE_NAME}-crds
CHART_FILE := $(IMAGE_NAME)-$(CHART_VERSION).tgz
CHART_FILE_CRD := $(IMAGE_NAME)-crds-$(CHART_VERSION).tgz

ifeq ($(AWS_ACCESS_KEY_ID),)
export AWS_ACCESS_KEY_ID     := $(shell aws configure get aws_access_key_id)
endif
ifeq ($(AWS_SECRET_ACCESS_KEY),)
export AWS_SECRET_ACCESS_KEY := $(shell aws configure get aws_secret_access_key)
endif
ifeq ($(AWS_REGION),)
export AWS_REGION            := $(shell aws configure get region)
endif
ifeq ($(AWS_SESSION_TOKEN),)
export AWS_SESSION_TOKEN     := $(shell aws configure get aws_session_token)
endif

HELM ?= docker run \
	--rm \
	--net host \
	-w /pkg \
	-v ${CWD}:/pkg \
	-v ~/.aws:/root/.aws \
	-v ${KUBECONFIG}:/root/.kube/config \
	-e AWS_PROFILE \
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
	-v ~/.aws:/root/.aws \
	-v ${KUBECONFIG}:/root/.kube/config \
	-e AWS_PROFILE \
	-e AWS_REGION \
	-e AWS_ACCESS_KEY_ID \
	-e AWS_SECRET_ACCESS_KEY \
	-e AWS_SESSION_TOKEN \
	 ${HELM_IMAGE} sh -c


.id:
	git config user.email | awk -F@ '{print $$1}' > .id


# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.23

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "	\033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: .id controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" go test ./... -coverprofile cover.out

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -o bin/manager main.go
	cd dbproxy && go build -o ../bin/dbproxy

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

#TODO
.PHONY: docker-buildx
docker-buildx: generate fmt vet manifests ## Build and optionally push a multi-arch db-controller container image to the Docker registry
	@docker buildx build --push \
		--build-arg api_version=$(API_VERSION) \
		--build-arg srv_version=$(SRV_VERSION) \
		-f $(SERVER_DOCKERFILE) \
		-t $(SERVER_IMAGE):$(IMAGE_VERSION) .

docker-build-dbproxy:
	cd dbproxy && docker build -t ${DBPROXY_IMG_PATH}:${TAG} .

.PHONY: docker-build
docker-build: #test ## Build docker image with the manager.
	docker build -t ${IMG_PATH}:${TAG} .

docker-push-dbproxy: docker-build-dbproxy
	docker push ${DBPROXY_IMG_PATH}:${TAG}

.PHONY: docker-push
docker-push: docker-build
	docker push ${IMG_PATH}:${TAG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply --namespace `cat .id` -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --namespace `cat .id` --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy-kustomize: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG_PATH}:${TAG}
	$(KUSTOMIZE) build config/default | kubectl apply --namespace `cat .id` -f -

.PHONY: undeploy
undeploy-kustomize: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete	--namespace `cat .id` --ignore-not-found=$(ignore-not-found) -f -


export CONTROLLER_GEN = $(shell pwd)/bin/controller-gen

.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@latest)

ENVTEST = $(shell pwd)/bin/setup-envtest
.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-get-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# Updates helm chart db-controller-crds to be in sync
update_crds: manifests
	cp ./config/crd/bases/persistance.atlas.infoblox.com_databaseclaims.yaml ./helm/db-controller-crds/crd/persistance.atlas.infoblox.com_databaseclaims.yaml

# find or download controller-gen
# download controller-gen if necessary
.PHONY: controller-gen
controller-gen:
ifeq (, $(shell which controller-gen))
	{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.9.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

KUBEBUILDER_ASSETS=/usr/local/kubebuilder/bin
export KUBEBUILDER_ASSETS

# find or download kubebuilder
# download kubebuilder if necessary
kubebuilder:
ifeq (, $(shell which kubebuilder))
	@{ \
	set -e ;\
	os=$(shell go env GOOS) ;\
	arch=$(shell go env GOARCH) ;\
	sudo mkdir -p /usr/local/kubebuilder/bin/ ; \
	sudo curl -k -Lo /usr/local/kubebuilder/bin/kubebuilder https://github.com/kubernetes-sigs/kubebuilder/releases/download/v3.2.0/kubebuilder_$${os}_$${arch} ; \
	curl -k -sSLo /tmp/envtest-bins.tar.gz "https://go.kubebuilder.io/test-tools/${K8S_VERSION}/$${os}/$${arch}" ; \
	sudo tar -C /usr/local/kubebuilder --strip-components=1 -zvxf /tmp/envtest-bins.tar.gz ; \
	export PATH=$$PATH:/usr/local/kubebuilder/bin ;\
	}
endif
	mkdir -p .build
	${KUBEBUILDER_ASSETS}/kube-apiserver --version
	${KUBEBUILDER_ASSETS}/kubectl version || true

create-namespace:
	kubectl create namespace `cat .id`

delete-namespace:
	kubectl delete namespace `cat .id`

install-crds: .id
	helm template db-controller-crd helm/db-controller-crds/ --namespace=`cat .id` |kubectl apply -f -

uninstall-crds: .id
	helm template db-controller-crd helm/db-controller-crds/ --namespace=`cat .id` |kubectl delete -f -

deploy-controller: .id
	helm template db-controller ./helm/db-controller/ --namespace=`cat .id` -f helm/db-controller/minikube.yaml | kubectl apply --namespace `cat .id` -f -

uninstall-controller: .id
	helm template db-controller ./helm/db-controller/ --namespace=`cat .id` -f helm/db-controller/minikube.yaml | kubectl delete --namespace `cat .id` -f -

deploy-samples: .id
	helm template dbclaim-sample helm/dbclaim-sample --namespace=`cat .id` | kubectl apply -f -

uninstall-samples:
	helm template dbclaim-sample helm/dbclaim-sample --namespace=`cat .id` | kubectl delete -f -

deploy-all: create-namespace install-crds deploy-controller

uninstall-all: uninstall-controller uninstall-crds delete-namespace

build-properties:
	@sed 's/{CHART_FILE}/$(CHART_FILE)/g' build.properties.in > build.properties

build-properties-crd:
	@sed 's/{CHART_FILE}/$(CHART_FILE_CRD)/g' build.properties.in > crd.build.properties

build-chart:
	sed "s/appVersion: \"APP_VERSION\"/appVersion: \"${TAG}\"/g" ${DB_CONTROLLER_CHART_IN} > ${DB_CONTROLLER_CHART_YAML}
	${HELM_ENTRYPOINT} "helm package ${DB_CONTROLLER_CHART} --version ${CHART_VERSION} --app-version ${APP_VERSION}"

build-chart-crd: update_crds
	${HELM_ENTRYPOINT} "helm package ${CRDS_CHART} --version ${CHART_VERSION}"


# Emit local aws credentials to environment
push-chart:
	${HELM} \
		s3 push ${CHART_FILE} infobloxcto

push-chart-crd:
	${HELM} \
		s3 push ${CHART_FILE_CRD} infobloxcto

clean:
	@rm -f *build.properties || true
	@rm -f *.tgz || true
	sudo rm -rf /usr/local/kubebuilder

deploy-crds: .id build-chart-crd
	${HELM} upgrade --install --namespace $(cat .id) ${CRDS_CHART} --version $(CHART_VERSION)

helm/db-controller/Chart.yaml: helm/db-controller/Chart.in
	sed "s/appVersion: \"APP_VERSION\"/appVersion: \"${TAG}\"/g" $^ > $@

deploy: .id docker-push helm/db-controller/Chart.yaml
	helm upgrade --install `cat .id`-db-ctrl helm/db-controller \
		 --debug --wait --namespace "`cat .id`" \
	 	--create-namespace \
                 -f helm/db-controller/minikube.yaml \
	 	--set dbController.class=`cat .id` \
		--set image.tag="${TAG}" \
	 	--set db.identifier.prefix=`cat .id` ${HELM_SETFLAGS}


undeploy: .id
	helm delete --namespace `cat .id` `cat .id`-db-ctrl
