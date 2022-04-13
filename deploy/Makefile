export AWS_REGION   = us-west-1
export AWS_PROFILE ?= bloxinabox
export AWS_ACCOUNT ?= $(shell aws --profile $(AWS_PROFILE) sts get-caller-identity --output text --query 'Account')

K8S_VERSION := 1.21
K8S_WAIT := wait --timeout=120s
K8S_DEST_SERVER := https://kubernetes.default.svc

POSTGRES_VERSION := 12.8

default: eks

eks: cluster crossplane crossplane_trust crossplane_aws

.id:
	git config user.email | awk -F@ '{print $$1}' > .id

.cluster: .id
	echo "$(shell cat .id)-bloxinabox" > .cluster

cluster.yaml: force .cluster cluster.yaml.in
	sed "s/{{ .Name }}/`cat .id`/g; s/{{ .ClusterName }}/`cat .cluster`/g; s/{{ .KubeVersion }}/$(K8S_VERSION)/g ;s/{{ .Region }}/$(AWS_REGION)/g" cluster.yaml.in > $@

eks-deploy: cluster.yaml
	eksctl create cluster -f cluster.yaml

eks-oidc: cluster.yaml
eks-oidc: cluster_name=$(shell cat .cluster )
eks-oidc:
	eksctl utils associate-iam-oidc-provider --cluster $(cluster_name) --region $(AWS_REGION) --approve

eks-config: cluster.yaml
eks-config: cluster_name=$(shell cat .cluster )
eks-config:
	@echo "Get Cluster Config"
	aws eks update-kubeconfig --name $(cluster_name)

cluster: eks-deploy eks-oidc eks-config
	@echo "Create NGINX Ingress Controller"
	# FIXME - The ingress on main has bug
	# kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/master/deploy/static/provider/aws/deploy.yaml
	kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/a8408cdb51086a099a2c71ed3e68363eb3a7ae60/deploy/static/provider/aws/deploy.yaml
	kubectl $(K8S_WAIT) --namespace ingress-nginx --for=condition=ready pod --selector=app.kubernetes.io/component=controller
	@echo 'Done with cluster build'

crossplane-config.yaml: provider_role=$(shell cat .cluster )-provider-role
crossplane-config.yaml: force crossplane-config.yaml.in
	sed "s/{{ .Account }}/$(AWS_ACCOUNT)/g; s/{{ .IamRoleName }}/$(provider_role)/g" crossplane-config.yaml.in > $@

crossplane: crossplane-config.yaml
	@echo "Install Cross-Plane"
	kubectl apply -f crossplane-ns.yaml
	helm repo add crossplane-stable https://charts.crossplane.io/stable
	helm install crossplane --namespace crossplane-system crossplane-stable/crossplane
	kubectl $(K8S_WAIT) -n crossplane-system --for=condition=Available deployment.apps/crossplane
	kubectl $(K8S_WAIT) -n crossplane-system --for=condition=Available deployment.apps/crossplane-rbac-manager
	kubectl apply -f crossplane-config.yaml
	# Need CRDs to install
	kubectl $(K8S_WAIT) --for=condition=Established crd/providers.pkg.crossplane.io
	@echo "Done with Cross-Plane Installation"

provider-trust.json: OIDC_PROVIDER=$(shell aws --profile $(AWS_PROFILE) eks describe-cluster --name $(shell cat .cluster) --region $(AWS_REGION) --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")
provider-trust.json: OIDC_PROVIDER_ESCAPED=$(shell echo $(OIDC_PROVIDER) | sed 's/\//\\\//g' )
provider-trust.json: force provider-trust.json.in
	sed "s/{{ .Account }}/$(AWS_ACCOUNT)/g; s/{{ .OidcProvider }}/$(OIDC_PROVIDER_ESCAPED)/g; s/{{ .NameSpace }}/crossplane-system/g" provider-trust.json.in > $@


crossplane_trust: cluster_name=$(shell cat .cluster )
crossplane_trust: provider_role=$(shell cat .cluster )-provider-role
crossplane_trust: svc_acct_name=$(shell kubectl get providers.pkg.crossplane.io provider-aws -o jsonpath="{.status.currentRevision}")
crossplane_trust: svc_acct_ns=crossplane-system
crossplane_trust: provider-config.yaml
	@echo "Setup CrossPlane Trust"
	# FIXME - Setup CrossPlane with minimum access right now it has full admin access
	eksctl create iamserviceaccount --cluster $(cluster_name) --region $(AWS_REGION) --name $(svc_acct_name) --namespace $(svc_acct_ns) --role-name $(provider_role) --role-only --attach-policy-arn="arn:aws:iam::aws:policy/AdministratorAccess" --approve
	kubectl apply -f provider-config.yaml
	@echo "CrossPlane Trust Established"

vpcid: vpcid=$(shell aws --profile $(AWS_PROFILE) eks describe-cluster --region $(AWS_REGION) --name $(shell cat .cluster) --query "cluster.resourcesVpcConfig.vpcId" --output text)
vpcid:
	echo $(vpcid)

rds-subnet.yaml: vpcid=$(shell aws --profile $(AWS_PROFILE) eks describe-cluster --region $(AWS_REGION) --name $(shell cat .cluster) --query "cluster.resourcesVpcConfig.vpcId" --output text)
rds-subnet.yaml: pubSubIds=$(shell aws --profile $(AWS_PROFILE) ec2 describe-subnets --region $(AWS_REGION) --filters '[{"Name":"vpc-id","Values":["$(vpcid)"]}]' | jq '.Subnets | .[] | select(.MapPublicIpOnLaunch == true) | .SubnetId')
rds-subnet.yaml: pubSub1=$(shell echo $(pubSubIds) | awk '{print $$1}')
rds-subnet.yaml: pubSub2=$(shell echo $(pubSubIds) | awk '{print $$2}')
rds-subnet.yaml: pubSub3=$(shell echo $(pubSubIds) | awk '{print $$3}')
rds-subnet.yaml: force rds-subnet.yaml.in
	sed "s/{{ .Name }}/$(shell cat .cluster)-rds-subnetgroup/g; s/{{ .Region }}/$(AWS_REGION)/g; s/{{ .VpcId }}/$(vpcid)/g; s/{{ .Sub1 }}/$(pubSub1)/g; s/{{ .Sub2 }}/$(pubSub2)/g; s/{{ .Sub3 }}/$(pubSub2)/g; s/{{ .Description }}/$(shell cat .cluster) EKS map vpc to rds/g" rds-subnet.yaml.in > $@

rds-sg.yaml: vpcid=$(shell aws --profile $(AWS_PROFILE) eks describe-cluster --region $(AWS_REGION) --name $(shell cat .cluster) --query "cluster.resourcesVpcConfig.vpcId" --output text)
rds-sg.yaml: force rds-sg.yaml.in
	sed "s/{{ .Name }}/$(shell cat .cluster)-rds-sg/g; s/{{ .Region }}/$(AWS_REGION)/g; s/{{ .VpcId }}/$(vpcid)/g; s/{{ .Port }}/5432/g; s/{{ .Description }}/$(shell cat .cluster) EKS open rds for workloads/g" rds-sg.yaml.in > $@

rds-composition.yaml: vpcid=$(shell aws --profile $(AWS_PROFILE) eks describe-cluster --region $(AWS_REGION) --name $(shell cat .cluster) --query "cluster.resourcesVpcConfig.vpcId" --output text)
rds-composition.yaml: force rds-composition.yaml.in
	sed "s/{{ .Region }}/$(AWS_REGION)/g; s/{{ .DbInstanceClass }}/db.t2.small/g; s/{{ .DbEngineVersion }}/$(POSTGRES_VERSION)/g; s/{{ .VpcSgRef }}/$(shell cat .cluster)-rds-sg/g; s/{{ .SubnetRef }}/$(shell cat .cluster)-rds-subnetgroup/g" rds-composition.yaml.in > $@

postgres-direct.yaml: force postgres-direct.yaml.in
	sed "s/{{ .Region }}/$(AWS_REGION)/g; s/{{ .DbInstanceClass }}/db.t2.small/g; s/{{ .DbEngineVersion }}/$(POSTGRES_VERSION)/g; s/{{ .VpcSgRef }}/$(shell cat .cluster)-rds-sg/g; s/{{ .SubnetRef }}/$(shell cat .cluster)-rds-subnetgroup/g" postgres-direct.yaml.in > $@

rds-subnetgroup: rds-subnet.yaml
	kubectl apply -f rds-subnet.yaml
	kubectl $(K8S_WAIT) --for=condition=Ready dbsubnetgroup.database.aws.crossplane.io/$(shell cat .cluster)-rds-subnetgroup
	kubectl $(K8S_WAIT) --for=condition=Synced dbsubnetgroup.database.aws.crossplane.io/$(shell cat .cluster)-rds-subnetgroup

rds-sg: rds-sg.yaml
	kubectl apply -f rds-sg.yaml
	kubectl $(K8S_WAIT) --for=condition=Ready securitygroup.ec2.aws.crossplane.io/$(shell cat .cluster)-rds-sg
	kubectl $(K8S_WAIT) --for=condition=Synced securitygroup.ec2.aws.crossplane.io/$(shell cat .cluster)-rds-sg

rds-composition: rds-composition.yaml
	kubectl apply -f rds-composition.yaml

rds-comp-resource-def: rds-comp-resource-def.yaml
	kubectl apply -f rds-comp-resource-def.yaml

crossplane_aws: rds-subnetgroup rds-sg rds-composition rds-comp-resource-def
	@echo "Setup CrossPlane AWS Composition Resources"

postgres-direct: postgres-direct.yaml
	kubectl apply -f postgres-direct.yaml

postgres-claim: postgres-claim.yaml
	kubectl apply -f postgres-claim.yaml

clean_rds_comp:
	kubectl delete -f rds-comp-resource-def.yaml
	kubectl delete -f rds-composition.yaml

clean:
	eksctl delete cluster --name $(shell cat .cluster) --region $(AWS_REGION)
	rm -f .id .cluster cluster.yaml crossplane-config.yaml provider-trust.json
	rm -f rds-composition.yaml rds-sg.yaml rds-subnet.yaml postgres-direct.yaml

force:
