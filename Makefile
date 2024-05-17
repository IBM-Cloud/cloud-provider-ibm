# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2021, 2023 All Rights Reserved.
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
GOCOVERDIR := .

CALICOCTL_VERSION=$(shell cat addons/calicoctl.yml | awk '/^version:/{print $$2}')
CALICOCTL_CHECKSUM=$(shell cat addons/calicoctl.yml | awk '/^checksum:/{print $$2}')

# When ARTIFACTORY_AUTH_HEADER_FILE is set then artifactory builds are enabled and the
# following environment variables will also be set:
# - ARTIFACTORY_USER_NAME
# - ARTIFACTORY_TOKEN_PATH
# - ARTIFACTORY_AUTH_HEADER_FILE
ifdef ARTIFACTORY_AUTH_HEADER_FILE
IMAGE_SOURCE := docker-na-private.artifactory.swg-devops.com/wcp-alchemy-containers-team-gcr-docker-remote
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
YAML_FILES=$(shell find . -type f -name '*.y*ml' -not -path "./build-tools/*" -not -path "./travis-secret-metadata/*" -print | sort)
INI_FILES=$(shell find . -type f -name '*.ini' -not -path "./build-tools/*")
OSS_FILES := go.mod

GOLANGCI_LINT_VERSION := 1.54.2
GOLANGCI_LINT_EXISTS := $(shell golangci-lint --version 2>/dev/null)

HUB_RLS ?= 2.14.2
REGISTRY ?= armada-master
TAG ?= v1.29.5

NANCY_VERSION := 1.0.45

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
ifdef ARTIFACTORY_AUTH_HEADER_FILE
	export ARTIFACTORY_TOKEN_PATH="/tmp/.artifactory-token-path"
	scripts/setup-artifactory-build.sh
else
	echo "Skipping artifactory build setup."
endif

.PHONY: setup-build
setup-build: setup-artifactory-build
	sudo apt-get install shellcheck -y || sudo snap install shellcheck
	pip install PyYAML yamllint

.PHONY: install-golangci-lint
install-golangci-lint:
ifdef ARTIFACTORY_AUTH_HEADER_FILE
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
	golangci-lint run --disable-all --no-config --enable=gofmt --timeout 5m
else
	@echo "golangci-lint is not installed"
	exit 1
endif

.PHONY: lint
lint:
ifdef GOLANGCI_LINT_EXISTS
	# NOTE(cjschaef): golangci-lint can take a while to run, bump deadline
	echo "Running gosec"
	golangci-lint run -v --timeout 5m
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
	go list -json -deps | nancy sleuth --no-color > nancy.log 2>&1; scripts/process_nancy_log.sh $$?

.PHONY: install-nancy-dep-scanner
install-nancy-dep-scanner:
ifdef ARTIFACTORY_AUTH_HEADER_FILE
	curl -L -H @${ARTIFACTORY_AUTH_HEADER_FILE} "https://na.artifactory.swg-devops.com/artifactory/wcp-alchemy-containers-team-github-generic-remote/sonatype-nexus-community/nancy/releases/download/v$(NANCY_VERSION)/nancy-v$(NANCY_VERSION)-linux-amd64" -o nancy
else
	curl -L "https://github.com/sonatype-nexus-community/nancy/releases/download/v$(NANCY_VERSION)/nancy-v$(NANCY_VERSION)-linux-amd64" -o nancy
endif
	chmod u+x nancy
	sudo mv nancy /usr/local/bin/nancy

.PHONY: containers
containers: calicoctlcli
	cp /usr/local/bin/calicoctl cmd/ibm-cloud-controller-manager/calicoctl
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

.PHONY: classic
classic:
ifdef ARTIFACTORY_AUTH_HEADER_FILE
	@echo "Update pkg/classic to use alternate classic library"
	./scripts/updatePackage.sh addons/classic.yml
else
	@echo "Use the existing pkg/classic logic"
endif

.PHONY: vpcctl
vpcctl:
ifdef ARTIFACTORY_AUTH_HEADER_FILE
	@echo "Update pkg/vpcctl to use alternate vpcctl library"
	./scripts/updatePackage.sh addons/vpcctl.yml
else
	@echo "Use the existing pkg/vpcctl logic"
endif

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

.PHONY: hub-install
hub-install:
ifdef ARTIFACTORY_AUTH_HEADER_FILE
	@echo "installing hub"
	@curl -H @${ARTIFACTORY_AUTH_HEADER_FILE} -OL "https://na.artifactory.swg-devops.com/artifactory/wcp-alchemy-containers-team-github-generic-remote/github/hub/releases/download/v$(HUB_RLS)/hub-linux-amd64-$(HUB_RLS).tgz" ; \
	tar -xzvf hub-linux-amd64-$(HUB_RLS).tgz ; \
	rm -f hub-linux-amd64-$(HUB_RLS).tgz ; \
	cd hub-linux-amd64-$(HUB_RLS) ; \
	sudo ./install ; \
	cd ..; rm -rf hub-linux-amd64-$(HUB_RLS) ; \
	git config --global --add hub.host github.ibm.com ; \
	git config --global user.email "iksroch1@us.ibm.com" ; \
	git config --global user.name "iksroch1"
else
	@echo "hub was not installed"
endif

.PHONY: deploy
deploy: hub-install
	scripts/deploy.sh ${REGISTRY}/ibm-cloud-controller-manager ${BUILD_TAG}

.PHONY: clean
clean:
	rm -f cover.out cover.html
	rm -f cmd/ibm-cloud-controller-manager/calicoctl
	rm -f ibm-cloud-controller-manager
	rm -f tests/fvt/ibm_loadbalancer
	rm -rf $(GOPATH)/src/k8s.io
	rm -rf Bluemix_CLI/
