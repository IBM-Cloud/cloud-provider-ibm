#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2017, 2024 All Rights Reserved.
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
SCRIPT_DIR=$(dirname "${BASH_SOURCE[0]}")
# import shared common bash functions
# shellcheck source=scripts/common_functions.sh
. "${SCRIPT_DIR}/scripts/common_functions.sh"

K8S_CURRENT_VERSION=$(grep "^TAG " Makefile | awk '{ print $3 }')
if [[ -z "${K8S_CURRENT_VERSION}" ]]; then
    echo "FAIL: Unable to determine current Kubernetes version in Makefile."
    exit 1
fi

# $1: Kubernetes update version
K8S_UPDATE_VERSION="${1}"
if [[ -z "${K8S_UPDATE_VERSION}" ]]; then
    if [[ $TRAVIS_EVENT_TYPE == "cron" ]]; then
        # Trim alpha/beta tag off of current release
        K8S_SHORT_VERSION=${K8S_CURRENT_VERSION%-*}
        # Looking up update version manually for updater cron job
        MAJOR_MINOR=${K8S_SHORT_VERSION%.*}
        # Verify TAG from Makefile matches git branch (first cron run on new release)
        BRANCH_MAJOR_MINOR="v${TRAVIS_BRANCH#"release-"}"
        if [[ "${MAJOR_MINOR}" != "${BRANCH_MAJOR_MINOR}" ]]; then
            MAJOR_MINOR=${BRANCH_MAJOR_MINOR}
        fi
        K8S_UPDATE_VERSION=$(curl https://api.github.com/repos/kubernetes/kubernetes/releases | jq -r ".[].name" | grep "$MAJOR_MINOR" | head -1 | sed 's/^Kubernetes //g')
        MOD_VERSION=$(go mod download -json "k8s.io/api@kubernetes-${K8S_UPDATE_VERSION#v}" | jq -r .Version)
        if [[ -z "${K8S_UPDATE_VERSION}" ]]; then
            echo "FAIL: Failed to retrieve the kubernetes release, attempt to retrive the git tag"
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
    else
        echo "FAIL: Kubernetes update version not set."
        exit 1
    fi
fi

echo "INFO: Starting Kubernetes update from version ${K8S_CURRENT_VERSION} to ${K8S_UPDATE_VERSION} ..."
make clean

K8S_DIRECTORY="/tmp/kubernetes"
if [[ ! -e "${K8S_DIRECTORY}" ]]; then
    git clone --depth=1 --no-single-branch https://github.com/kubernetes/kubernetes.git ${K8S_DIRECTORY}
fi
git -C ${K8S_DIRECTORY} checkout master && git -C ${K8S_DIRECTORY} remote update && git -C ${K8S_DIRECTORY} pull --ff-only origin master

# Determine the current and update golang version.
git -C "${K8S_DIRECTORY}" checkout "${K8S_CURRENT_VERSION}"
K8S_GOLANG_CURRENT_VERSION=$(grep -A 1 "name: \"golang: upstream version" "${K8S_DIRECTORY}/build/dependencies.yaml" | grep "version:" | awk '{ print $2 }')
git -C "${K8S_DIRECTORY}" checkout "${K8S_UPDATE_VERSION}"
K8S_GOLANG_UPDATE_VERSION=$(grep -A 1 "name: \"golang: upstream version" "${K8S_DIRECTORY}/build/dependencies.yaml" | grep "version:" | awk '{ print $2 }')

# Update files based on Kubernetes and IBM release versions.
ALL_FILES=$(find . \( -path ./.git -o -path ./kube-update.sh -o -path './go.*' \) -prune -o \( -type f -print \))
# shellcheck disable=SC2086
FILES_TO_UPDATE_FOR_K8S_VERSION=$(grep -l -F "${K8S_CURRENT_VERSION}" $ALL_FILES)
for FILE_TO_UPDATE_FOR_K8S_VERSION in $FILES_TO_UPDATE_FOR_K8S_VERSION; do
    sed -i -e "s/${K8S_CURRENT_VERSION}/${K8S_UPDATE_VERSION}/g" "${FILE_TO_UPDATE_FOR_K8S_VERSION}"
    git add "${FILE_TO_UPDATE_FOR_K8S_VERSION}"
    echo "INFO: Updated Kubernetes version in ${FILE_TO_UPDATE_FOR_K8S_VERSION}"
done

if [[ "${K8S_GOLANG_CURRENT_VERSION}" != "${K8S_GOLANG_UPDATE_VERSION}" ]]; then
    echo "INFO: Current golang version: ${K8S_GOLANG_CURRENT_VERSION}"
    echo "INFO: Updated golang version: ${K8S_GOLANG_UPDATE_VERSION}"

    sed -i -e "s/go\s\+${K8S_GOLANG_CURRENT_VERSION}/go ${K8S_GOLANG_UPDATE_VERSION}/g" go.mod
    go mod tidy
    git add go.mod
    git add go.sum
    echo "INFO: Updated golang version in go.mod / go.sun"

    sed -i -e "s/^  - ${K8S_GOLANG_CURRENT_VERSION}/  - ${K8S_GOLANG_UPDATE_VERSION}/g" .travis.yml
    sed -i -e "s/go:\s\+${K8S_GOLANG_CURRENT_VERSION}/go: ${K8S_GOLANG_UPDATE_VERSION}/g" .travis.yml
    git add .travis.yml
    echo "INFO: Updated golang version in .travis.yml"

    sed -i -e "s/go${K8S_GOLANG_CURRENT_VERSION}/go${K8S_GOLANG_UPDATE_VERSION}/g" vagrant-kube-build/Vagrantfile
    git add vagrant-kube-build/Vagrantfile
    echo "INFO: Updated golang version in vagrant-kube-build/Vagrantfile"
fi

COMMIT_MESSAGE="Update repo from ${K8S_CURRENT_VERSION} to ${K8S_UPDATE_VERSION} ($TRAVIS_BRANCH)"
git checkout -b "${K8S_UPDATE_VERSION}-initial"
git commit --no-verify -m "${COMMIT_MESSAGE}"
if [[ $TRAVIS_EVENT_TYPE == "cron" ]]; then
    make hub-install
    export GITHUB_TOKEN=${GHE_TOKEN}
    hub pull-request -b "${TRAVIS_BRANCH}" -m "${COMMIT_MESSAGE}" --push
else
    # Otherwise push up branch for manual runs
    git push origin "${K8S_UPDATE_VERSION}-initial"
fi

echo "SUCCESS: Completed Kubernetes update from version ${K8S_CURRENT_VERSION} to ${K8S_UPDATE_VERSION}."
exit 0
