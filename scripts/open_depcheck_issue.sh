#!/bin/bash
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
set -e
if [ "${TRAVIS_ALLOW_FAILURE}" = "false" ] && [ "${TRAVIS_PULL_REQUEST_BRANCH}" = "" ] && [ "${TRAVIS_EVENT_TYPE}" = "cron" ]; then
    # If build is without artifactory, then exit
    if [ "${BUILD_JOB_NAME}" = "No Artifactory" ]; then
        exit 0
    fi

    # Install hub if needed
    if ! command -v "hub" >/dev/null; then
        make hub-install
    fi
    export GITHUB_TOKEN=${GHE_TOKEN}

    # Clone the armada-network repo
    git clone --depth=1 --single-branch "https://${GHE_USER}:${GHE_TOKEN}@github.ibm.com/alchemy-containers/armada-network.git"
    cd armada-network

    # Create body for new issue
    {
        echo "Travis build of armada-lb failed depcheck (${TRAVIS_BRANCH})"
        echo
        echo "Travis build ${TRAVIS_BUILD_NUMBER}: ${TRAVIS_BUILD_WEB_URL}"
        echo
    } >"${TRAVIS_BUILD_DIR}"/message.txt

    # Grab list of all depcheck issues
    hub issue -l depcheck >issues.txt

    # Create new issue or add comment to existing issue
    if ! grep -q "${TRAVIS_BRANCH}" issues.txt; then
        echo "Create new issue"
        hub issue create --file "${TRAVIS_BUILD_DIR}"/message.txt --labels "ccm,depcheck,security,${TRAVIS_BRANCH}"
    else
        echo "Update existing issue"
        num=$(grep "${TRAVIS_BRANCH}" issues.txt | awk '{print $1 }' | tr -d '#')
        hub api "repos/alchemy-containers/armada-network/issues/${num}/comments" --raw-field "body=Travis build ${TRAVIS_BUILD_NUMBER}: ${TRAVIS_BUILD_WEB_URL}"
    fi

    # Remove the armada-network clone
    cd ..
    rm -rf armada-network
fi
