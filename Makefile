# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2021, 2025 All Rights Reserved.
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

export
GOPACKAGES=$(shell go list ./...)
SHFILES=$(shell find . -type f -name '*.sh')

GOLANGCI_LINT_VERSION := 1.64.8
GOLANGCI_LINT_EXISTS := $(shell golangci-lint --version 2>/dev/null)

TAG ?= v1.34.0-beta.0

.PHONY: all
all: fmt lint lint-sh vet test ccm

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
	echo "Running gosec"
	golangci-lint run -v --timeout 5m
else
	@echo "golangci-lint is not installed"
	exit 1
endif

.PHONY: lint-sh
lint-sh:
	shellcheck -x -V
	shellcheck ${SHFILES}

.PHONY: vet
vet:
	go vet ${GOPACKAGES}

.PHONY: test
test:
	go test -v -race -covermode=atomic -coverprofile=cover.out ${GOPACKAGES}

.PHONY: ccm
ccm:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ibm-cloud-controller-manager -ldflags '-w -X cloud.ibm.com/cloud-provider-ibm/ibm.Version=${TAG}' .

.PHONY: clean
clean:
	rm -f cover.out
	rm -f ibm-cloud-controller-manager
