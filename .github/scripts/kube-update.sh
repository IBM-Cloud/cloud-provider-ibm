#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2025 All Rights Reserved.
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
set -e

K8S_CURRENT_VERSION=$(grep "^TAG " Makefile | awk '{ print $3 }')
if [[ -z "${K8S_CURRENT_VERSION}" ]]; then
    echo "FAIL: Unable to determine current Kubernetes version in Makefile."
    exit 1
fi

# Trim alpha/beta tag off of current release
K8S_SHORT_VERSION=${K8S_CURRENT_VERSION%-*}
MAJOR_MINOR=${K8S_SHORT_VERSION%.*}

# Verify TAG from Makefile matches git branch (first run on new release)
GIT_BRANCH=$(git branch --show-current)
BRANCH_MAJOR_MINOR="v${GIT_BRANCH#"release-"}"
if [[ "${MAJOR_MINOR}" != "${BRANCH_MAJOR_MINOR}" ]]; then
    MAJOR_MINOR=${BRANCH_MAJOR_MINOR}
fi

K8S_UPDATE_VERSION=$(curl https://api.github.com/repos/kubernetes/kubernetes/releases | jq -r ".[].name" | grep "$MAJOR_MINOR" | head -1 | sed 's/^Kubernetes //g')
MOD_VERSION=$(go mod download -json "k8s.io/api@kubernetes-${K8S_UPDATE_VERSION#v}" | jq -r .Version)
if [[ -z "${K8S_UPDATE_VERSION}" ]]; then
    echo "FAIL: Failed to retrieve the kubernetes release, attempt to retrieve the git tag"
    K8S_UPDATE_VERSION=$(curl https://api.github.com/repos/kubernetes/kubernetes/tags | jq -r ".[].name" | grep "$MAJOR_MINOR" | head -1)
    MOD_VERSION=$(go mod download -json "k8s.io/api@kubernetes-${K8S_UPDATE_VERSION#v}" | jq -r .Version)
fi
if [[ -z "${K8S_UPDATE_VERSION}" ]]; then
    echo "FAIL: Failed to retrieve latest kubernetes version."
    exit 1
fi
if [[ "${K8S_UPDATE_VERSION}" == "${K8S_CURRENT_VERSION}" ]]; then
    echo "INFO: No new version available, exiting gracefully"
    exit 0
fi
# Ensure the go modules have also been updated. i.e. k8s.io/api v0.20.3
if [[ "${MOD_VERSION}" != "v0.${K8S_UPDATE_VERSION#*.}" ]]; then
    echo "INFO: New go modules are not yet available, exiting gracefully"
    exit 0
fi

echo "INFO: Starting Kubernetes update from version ${K8S_CURRENT_VERSION} to ${K8S_UPDATE_VERSION} ..."

# Update files that contain the Kubernetes version
FILES_WITH_K8S_VERSION="ibm/ibm_version.go main.go Makefile README.md"
for FILE_TO_UPDATE in $FILES_WITH_K8S_VERSION; do
    sed -i -e "s/${K8S_CURRENT_VERSION}/${K8S_UPDATE_VERSION}/g" "${FILE_TO_UPDATE}"
    echo "INFO: Updated Kubernetes version in ${FILE_TO_UPDATE}"
done

# Determine the current and update golang version.
K8S_DIRECTORY="/tmp/kubernetes"
rm -rf "${K8S_DIRECTORY}"

git clone --filter=blob:none --depth=1 --sparse -b "${K8S_CURRENT_VERSION}" https://github.com/kubernetes/kubernetes.git ${K8S_DIRECTORY}
git -C ${K8S_DIRECTORY} sparse-checkout add build
GO_CURRENT_VERSION=$(grep -A 1 "name: \"golang: upstream version" "${K8S_DIRECTORY}/build/dependencies.yaml" | grep "version:" | awk '{ print $2 }')
echo "INFO: Current Go version: ${GO_CURRENT_VERSION}"
rm -rf "${K8S_DIRECTORY}"

git clone --filter=blob:none --depth=1 --sparse -b "${K8S_UPDATE_VERSION}" https://github.com/kubernetes/kubernetes.git ${K8S_DIRECTORY}
git -C ${K8S_DIRECTORY} sparse-checkout add build
GO_UPDATE_VERSION=$(grep -A 1 "name: \"golang: upstream version" "${K8S_DIRECTORY}/build/dependencies.yaml" | grep "version:" | awk '{ print $2 }')
echo "INFO: Updated Go version: ${GO_UPDATE_VERSION}"
rm -rf "${K8S_DIRECTORY}"

if [[ "${GO_CURRENT_VERSION}" != "${GO_UPDATE_VERSION}" ]]; then
    sed -i -e "s/go\s\+${GO_CURRENT_VERSION}/go ${GO_UPDATE_VERSION}/g" go.mod
    go mod tidy
    echo "INFO: Updated Go version in go.mod from ${GO_CURRENT_VERSION} to ${GO_UPDATE_VERSION}"
fi

echo "SUCCESS: Completed Kubernetes update from version ${K8S_CURRENT_VERSION} to ${K8S_UPDATE_VERSION}."
exit 0
