#!/bin/bash
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
cat "${TRAVIS_BUILD_DIR}/nancy.log"
echo "Exit code from calling nancy = [$1]"
status=""
if [ "$1" = "0" ]; then
    echo "Nancy dependency check was successful"
    status="ok"
else
    echo "Nancy dependency check failed"
    status="failed"
fi
set -e
if [ "${TRAVIS_ALLOW_FAILURE}" = "false" ] && [ "${TRAVIS_PULL_REQUEST_BRANCH}" = "" ] && [ "${TRAVIS_EVENT_TYPE}" = "cron" ]; then
    # If build is without artifactory, then exit
    if [ "${TRAVIS_JOB_NAME}" = "No Artifactory" ]; then
        exit 0
    fi

    # Install hub if needed
    which hub || make hub-install
    export GITHUB_TOKEN=${GHE_TOKEN}

    # Clone the armada-network repo
    git clone --depth=1 --single-branch "https://${GHE_USER}:${GHE_TOKEN}@github.ibm.com/alchemy-containers/armada-network.git"
    cd armada-network

    # Updating existing issue
    {
        echo "Travis build ${TRAVIS_BUILD_NUMBER}: ${TRAVIS_BUILD_WEB_URL}"
        echo
        # shellcheck disable=SC2016
        echo '```'
        tail -7 "${TRAVIS_BUILD_DIR}/nancy.log"
        echo
        grep "pkg:golang" "${TRAVIS_BUILD_DIR}/nancy.log" || true
        # shellcheck disable=SC2016
        echo '```'
        echo
    } >"${TRAVIS_BUILD_DIR}"/update-issue.txt
    body=$(cat "${TRAVIS_BUILD_DIR}/update-issue.txt")

    # Creating new issue
    {
        echo "Travis build of armada-lb failed depcheck (${TRAVIS_BRANCH})"
        echo
        cat "${TRAVIS_BUILD_DIR}"/update-issue.txt
        echo
    } >"${TRAVIS_BUILD_DIR}"/create-issue.txt

    # Grab list of all depcheck issues
    hub issue -l depcheck >issues.txt

    # Create new issue or add comment/close existing issue
    if ! grep -q "${TRAVIS_BRANCH}" issues.txt; then
        if [ "${status}" = "failed" ]; then
            echo "Create new issue"
            hub issue create --file "${TRAVIS_BUILD_DIR}"/create-issue.txt --labels "ccm,depcheck,security,${TRAVIS_BRANCH}"
        fi
    else
        echo "Update existing issue"
        num=$(grep "${TRAVIS_BRANCH}" issues.txt | awk '{print $1 }' | tr -d '#')
        hub api "repos/alchemy-containers/armada-network/issues/${num}/comments" --raw-field body="${body}"
        if [ "${status}" = "ok" ]; then
            echo "Close existing issue"
            hub issue update "${num}" -s closed
        fi
    fi

    # Remove the armada-network clone
    cd "${TRAVIS_BUILD_DIR}"
    rm -rf armada-network
fi
