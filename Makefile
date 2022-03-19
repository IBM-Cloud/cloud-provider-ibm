# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2021, 2022 All Rights Reserved.
#
# SPDX-License-Identifier: Apache2.0
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
# ******************************************************************************
GO111MODULE := on

CALICOCTL_VERSION=$(shell cat addons/calicoctl.yml | awk '/^version:/{print $$2}')
CALICOCTL_CHECKSUM=$(shell cat addons/calicoctl.yml | awk '/^checksum:/{print $$2}')

# When ARTIFACTORY_API_KEY is set then artifactory builds are enabled and the
# following environment variables will also be set:
# - ARTIFACTORY_USER
# - ARTIFACTORY_API_KEY_FILE
# - ARTIFACTORY_AUTH_HEADER_FILE
ifdef ARTIFACTORY_API_KEY
GOPROXY := https://${ARTIFACTORY_USER}:${ARTIFACTORY_API_KEY}@na.artifactory.swg-devops.com/artifactory/api/go/wcp-alchemy-containers-team-go-virtual
IMAGE_SOURCE := wcp-alchemy-containers-team-gcr-docker-remote.artifactory.swg-devops.com
CALICOCTL_CURL_HEADERS := "-H @${ARTIFACTORY_AUTH_HEADER_FILE}"
CALICOCTL_CURL_URL=$(shell cat addons/calicoctl.yml | awk '/^source_artifactory:/{print $$2}')
else
IMAGE_SOURCE := gcr.io
CALICOCTL_CURL_URL=$(shell cat addons/calicoctl.yml | awk '/^source_git:/{print $$2}')
endif
export
GOPACKAGES=$(shell go list ./...)
GOFILES=$(shell find . -type f -name '*.go' -not -path "./test-fixtures/*")
SHFILES=$(shell find . -type f -name '*.sh' -not -path "./build-tools/*")
AWKFILES=$(shell find . -type f -name '*.awk' -not -path "./build-tools/*")
PYFILES=$(shell find . -type f -name '*.py' -not -path "./build-tools/*")
YAML_FILES=$(shell find . -type f -name '*.y*ml' -not -path "./build-tools/*" -print | sort)
INI_FILES=$(shell find . -type f -name '*.ini' -not -path "./build-tools/*")
OSS_FILES := go.mod

GOLANGCI_LINT_VERSION := 1.40.1
GOLANGCI_LINT_EXISTS := $(shell golangci-lint --version 2>/dev/null)

REGISTRY ?= armada-master
TAG ?= v1.22.8
VPCCTL_SOURCE=$(shell cat addons/vpcctl.yml | awk '/^source:/{print $$2}')
VPCCTL_CHECKSUM=$(shell cat addons/vpcctl.yml | awk '/^checksum:/{print $$2}')

NANCY_VERSION := 1.0.17

WORKSPACE=$(GOPATH)/src/k8s.io

ifeq (,$(BUILD_ID))
	BUILD_ID = 0
endif
ifeq (,$(BUILD_SHA))
	BUILD_SHA = 0
endif
ifeq (,$(BUILD_TAG))
	BUILD_TAG = $(TAG)-$(BUILD_SHA)
endif

.PHONY: all
all: oss fmt lint lint-sh lint-copyright vet test coverage commands fvttest containers

.PHONY: setup-artifactory-build
setup-artifactory-build:
ifdef IKS_PIPELINE_IAM_APIKEY
	echo "Preparing artifactory build setup."
	curl -s https://s3.us.cloud-object-storage.appdomain.cloud/armada-build-tools-prod-us-geo/build-tools/build-tools.tar.gz | tar -xvz
	./build-tools/install.sh
	./build-tools/key-protect/get-key-data.sh --bluemix-api-key "${IKS_PIPELINE_IAM_APIKEY}" --keyprotect-instance-id "${IKS_PIPELINE_KEYPROTECT_INSTANCE_ID}" --keyprotect-root-key-name icdevops-artifactory-api-key --keyprotect-host "us-south.kms.cloud.ibm.com" | base64 -d > "${ARTIFACTORY_API_KEY_FILE}"
	cat "${ARTIFACTORY_API_KEY_FILE}" | docker login wcp-alchemy-containers-team-access-redhat-docker-remote.artifactory.swg-devops.com --username "${ARTIFACTORY_USER}" --password-stdin
	cat "${ARTIFACTORY_API_KEY_FILE}" | docker login wcp-alchemy-containers-team-gcr-docker-remote.artifactory.swg-devops.com --username "${ARTIFACTORY_USER}" --password-stdin
	mkdir -p ~/.pip/
	echo "[global]" > ~/.pip/pip.conf
	echo "index-url = https://na.artifactory.swg-devops.com/artifactory/api/pypi/wcp-alchemy-containers-team-pypi-remote/simple" >> ~/.pip/pip.conf
	printf "machine na.artifactory.swg-devops.com login ${ARTIFACTORY_USER} password " >> ~/.netrc
	cat "${ARTIFACTORY_API_KEY_FILE}" >> ~/.netrc
	printf "X-JFrog-Art-Api:" > "${ARTIFACTORY_AUTH_HEADER_FILE}"
	cat "${ARTIFACTORY_API_KEY_FILE}" >> "${ARTIFACTORY_AUTH_HEADER_FILE}"
else
	echo "Skipping artifactory build setup."
endif

.PHONY: setup-build
setup-build: setup-artifactory-build
	sudo apt-get install shellcheck -y || sudo snap install shellcheck
	pip install PyYAML yamllint

.PHONY: install-golangci-lint
install-golangci-lint:
ifdef ARTIFACTORY_API_KEY
	curl -H @${ARTIFACTORY_AUTH_HEADER_FILE} -L "https://na.artifactory.swg-devops.com/artifactory/wcp-alchemy-containers-team-github-generic-remote/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz" | sudo tar -xvz -C $(go env GOPATH)/bin --strip-components=1 golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64/golangci-lint
else
	curl -L "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64.tar.gz" | sudo tar -xvz -C $(go env GOPATH)/bin --strip-components=1 golangci-lint-${GOLANGCI_LINT_VERSION}-linux-amd64/golangci-lint
endif

.PHONY: oss
oss:
	test -f "LICENSE" || (echo "LICENSE file does not exist" && exit 1)

.PHONY: kube-update
kube-update:
	./kube-update.sh ${KUBE_VERSION}

.PHONY: fmt
fmt:
ifdef GOLANGCI_LINT_EXISTS
	golangci-lint run --disable-all --no-config --enable=gofmt
else
	@echo "golangci-lint is not installed"
	exit 1
endif

.PHONY: lint
lint:
ifdef GOLANGCI_LINT_EXISTS
	# NOTE(cjschaef): golangci-lint can take a while to run, bump deadline
	echo "Running gosec"
	golangci-lint run --deadline 5m -e exitAfterDefer
else
	@echo "golangci-lint is not installed"
	exit 1
endif

.PHONY: lint-sh
lint-sh:
	# NOTE(cjschaef) Travis CI xenial uses shellcheck version 0.7.0
	shellcheck -x -V
	shellcheck ${SHFILES}

.PHONY: lint-copyright
lint-copyright:
	scripts/checkCopyrights.sh ${GOFILES} ${SHFILES} ${AWKFILES} ${PYFILES} ${YAML_FILES} ${INI_FILES}

.PHONY: yamllint
yamllint:
	yamllint --version
	yamllint -c .yamllint ${YAML_FILES}

.PHONY: vet
vet:
	go vet ${GOPACKAGES}

.PHONY: test
test:
	go test -v -race -covermode=atomic -coverprofile=cover.out ${GOPACKAGES}

.PHONY: coverage
coverage:
	go tool cover -html=cover.out -o=cover.html

.PHONY: commands
commands:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ibm-cloud-controller-manager -ldflags '-w -X cloud.ibm.com/cloud-provider-ibm/ibm.Version=${BUILD_TAG}' .

.PHONY: fvttest
fvttest:
	cd tests/fvt && CGO_ENABLED=0 GOOS=linux go build ibm_loadbalancer.go

.PHONY: runanalyzedeps
runanalyzedeps:
	which nancy || $(MAKE) install-nancy-dep-scanner
	if ! go list -json -m all | nancy sleuth; then scripts/open_depcheck_issue.sh; fi

.PHONY: install-nancy-dep-scanner
install-nancy-dep-scanner:
ifdef ARTIFACTORY_API_KEY
	curl -L -H @${ARTIFACTORY_AUTH_HEADER_FILE} "https://na.artifactory.swg-devops.com/artifactory/wcp-alchemy-containers-team-github-generic-remote/sonatype-nexus-community/nancy/releases/download/v$(NANCY_VERSION)/nancy-v$(NANCY_VERSION)-linux-amd64" -o nancy
else
	curl -L "https://github.com/sonatype-nexus-community/nancy/releases/download/v$(NANCY_VERSION)/nancy-v$(NANCY_VERSION)-linux-amd64" -o nancy
endif
	chmod u+x nancy
	sudo mv nancy /usr/local/bin/nancy

.PHONY: containers
containers: calicoctlcli vpcctlcli
	cp /usr/local/bin/calicoctl cmd/ibm-cloud-controller-manager/calicoctl
	cp /usr/local/bin/vpcctl cmd/ibm-cloud-controller-manager/vpcctl
	docker -l debug build \
		--build-arg IMAGE_SOURCE=${IMAGE_SOURCE} \
		--build-arg this_build_id=${BUILD_ID} \
		--build-arg this_build_tag=${BUILD_TAG} \
		--build-arg this_build_sha=${BUILD_SHA} \
		--build-arg REPO_SOURCE_URL="${REPO_SOURCE_URL}" \
		--build-arg BUILD_URL="${BUILD_URL}" \
		-f cmd/ibm-cloud-controller-manager/Dockerfile \
		-t ${REGISTRY}/ibm-cloud-controller-manager:${BUILD_TAG} .
	docker images

.PHONY: calicoctlcli
calicoctlcli:
	scripts/verify_file_md5.sh /usr/local/bin/calicoctl ${CALICOCTL_CURL_URL} ${CALICOCTL_CHECKSUM} ${CALICOCTL_CURL_HEADERS}
	sudo chmod 755 /usr/local/bin/calicoctl
	sudo mkdir -p /etc/calico/ && sudo touch /etc/calico/calicoctl.cfg

.PHONY: vpcctlcli
vpcctlcli:
	scripts/verify_file_md5.sh /usr/local/bin/vpcctl ${VPCCTL_SOURCE} ${VPCCTL_CHECKSUM}
	sudo chmod 755 /usr/local/bin/vpcctl

.PHONY: kubectlcli
kubectlcli:
	sudo curl -Lo /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/$(TAG)/bin/linux/amd64/kubectl
	sudo chmod 755 /usr/local/bin/kubectl

.PHONY: ibmcloudcli
ibmcloudcli:
	curl -L 'https://clis.cloud.ibm.com/install/linux' | bash
	ibmcloud config --check-version false
	ibmcloud plugin uninstall kubernetes-service && ibmcloud plugin install kubernetes-service -r "IBM Cloud"

.PHONY: armadacli
armadacli: ibmcloudcli kubectlcli calicoctlcli
	ibmcloud plugin list && ibmcloud plugin repos
	ibmcloud --version
	kubectl version --client

.PHONY: runfvt
runfvt: kubectlcli vpcctlcli
	cd ./tests/fvt && LOCAL_IBM_ARMADA_LB_FVT_TEST=true ./ibm_loadbalancer --logtostderr=true -v=4 ${TEST_FVT_OPTIONS}

.PHONY: push-images
push-images:
	cd vagrant-kube-build/provisioning && ./push_image.sh ${ALT_REGISTRY} ${ALT_NAMESPACE} ibm-cloud-controller-manager

.PHONY: travis-deploy
travis-deploy:
	scripts/travisDeploy.sh ${REGISTRY}/ibm-cloud-controller-manager ${BUILD_TAG} ${TAG}

.PHONY: dev-deploy
dev-deploy:
	scripts/devDeploy.sh ${REGISTRY}/ibm-cloud-controller-manager ${BUILD_TAG}

.PHONY: clean
clean:
	rm -f cover.out cover.html
	rm -f cmd/ibm-cloud-controller-manager/calicoctl
	rm -f cmd/ibm-cloud-controller-manager/vpcctl
	rm -f ibm-cloud-controller-manager
	rm -f tests/fvt/ibm_loadbalancer
	rm -rf $(GOPATH)/src/k8s.io
	rm -rf Bluemix_CLI/
