#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2022, 2023 All Rights Reserved.
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
ADDON_FILE=${1}
SOURCE_REPO=$(awk '/^source:/{print $2}' "${ADDON_FILE}")
RELEASE=$(awk '/^release:/{print $2}' "${ADDON_FILE}")
SOURCE_DIR=$(awk '/^source_dir:/{print $2}' "${ADDON_FILE}")
TARGET_DIR=$(awk '/^target_dir:/{print $2}' "${ADDON_FILE}")
GREP_STRING=$(awk '/^grep_string:/{print $2}' "${ADDON_FILE}")
SED_COMMAND_1=$(grep '^sed_command_1:' "${ADDON_FILE}" | cut -d':' -f2)
SED_COMMAND_2=$(grep '^sed_command_2:' "${ADDON_FILE}" | cut -d':' -f2)
SED_COMMAND_3=$(grep '^sed_command_3:' "${ADDON_FILE}" | cut -d':' -f2)
UPDATE_GO_MOD=$(awk '/^update_go_mod:/{print $2}' "${ADDON_FILE}")
GO_GET_UPDATES=$(awk '/^go_get_updates:/{print $2}' "${ADDON_FILE}")
CREATE_PR=$(awk '/^create_pr:/{print $2}' "${ADDON_FILE}")

# Set target dir (if it was not specified)
if [ -z "${TARGET_DIR}" ]; then
    TARGET_DIR=${SOURCE_DIR}
fi
REPO_BASE=$(basename "${SOURCE_REPO}")

echo "Allow access to github.ibm.com"
export GOPRIVATE=github.ibm.com
export GONOPROXY=github.ibm.com
git config --global url."git@github.ibm.com:".insteadOf "https://github.ibm.com/"

# Clone the source repo
echo "Clone the source repo: ${SOURCE_REPO} ..."
git clone --depth=1 --single-branch --branch "${RELEASE}" "https://${GHE_USER}:${GHE_TOKEN}@${SOURCE_REPO}.git"

# Determine commit of the source repo
SOURCE_COMMIT=$(cd "${REPO_BASE}"; git rev-parse --short HEAD)

# Copy over the source files
echo "Copy over the package files ..."
rm -f "${TARGET_DIR}"/*.go
cp "${REPO_BASE}/${SOURCE_DIR}"/*.go "${TARGET_DIR}"
ls -al "${TARGET_DIR}"
echo

# If a grep string was specified, grep before and after the sed updates
if [ -n "${GREP_STRING}" ]; then
    echo "Before sed commands are done:"
    grep "${GREP_STRING}" "${TARGET_DIR}"/*.go
    echo
fi
# Update the GO source code using sed commands
for file in "${TARGET_DIR}"/*.go; do
    if [ -n "${SED_COMMAND_1}" ]; then
        sed -i "${SED_COMMAND_1}" "$file"
    fi
    if [ -n "${SED_COMMAND_2}" ]; then
        sed -i "${SED_COMMAND_2}" "$file"
    fi
    if [ -n "${SED_COMMAND_3}" ]; then
        sed -i "${SED_COMMAND_3}" "$file"
    fi
done
if [ -n "${GREP_STRING}" ]; then
    echo "After sed commands are done:"
    grep "${GREP_STRING}" "${TARGET_DIR}"/*.go
    echo
fi

# Do we need to update go.mod
if [ "${UPDATE_GO_MOD}" == "true" ]; then
    # Save a copy of the go.mod from the source repo
    cp "${REPO_BASE}"/go.mod go.mod.pkg

    # Delete the source repo, no longer needed
    rm -rf "${REPO_BASE}"

    # Determine how go.mod should be updated
    if [ "${GO_GET_UPDATES}" == "auto-detect" ]; then
        # Adjust the go.mod based on what is in source repo go.mod file
        echo "Determine if any go modules from ${REPO_BASE}/go.mod need to be retrieved ..."
        list=$(grep -v "indirect" go.mod.pkg | grep -v "^module " | grep '/' | awk '{print $1 ":" $2 }')
        for update in $list; do
            module=$(echo "$update" | cut -f1 -d':')
            version=$(echo "$update" | cut -f2 -d':')
            echo "... module: $module   version: $version"
            if ! grep -q "$module" go.mod; then
                echo "go get $module $version"
                go get "${module}@${version}"
            fi
        done
    else
        # Adjust the go.mod based on what was in the addon file
        echo "Update go.mod based on the ${ADDON_FILE} setting"
        for update in $GO_GET_UPDATES; do
            module=$(echo "$update" | cut -f1 -d':')
            version=$(echo "$update" | cut -f2 -d':')
            echo "... module: $module   version: $version"
            if ! grep -q "$module" go.mod; then
                echo "go get $module $version"
                go get "${module}@${version}"
            fi
        done
    fi
    echo

    # Delete the go.mod from the source repo, no longer needed
    rm go.mod.pkg

    # Refresh go.sum
    echo "Refresh go dependencies for new package logic"
    go mod tidy
    echo

    # Display the updates that were done to the go.mod
    echo "Changes to go.mod for new package logic"
    git diff go.mod
    echo
else
    # Delete the source repo, no longer needed
    rm -rf "${REPO_BASE}"
fi

# Do we need to create a PR for this update
if [ "${CREATE_PR}" != "false" ]; then
    make hub-install

    # Example branch name: "update-armada-lb-release-1.24-80f1016"
    echo "Create new branch ..."
    git checkout -b "update-${REPO_BASE}-${RELEASE}-${SOURCE_COMMIT}"
    echo

    # Add files to the commit
    for file in "${TARGET_DIR}"/*.go; do
        git add "$file"
    done
    if [ "${UPDATE_GO_MOD}" == "true" ]; then
        git add go.mod
        git add go.sum
    fi

    # Check to see if PR is needed
    git status
    if ! git status | grep -q "${TARGET_DIR}"; then
        echo "None of the ${TARGET_DIR} GO files need to get updated"
        exit 0
    fi

    # Example commit message: "Update pkg/vpcctl with armada-lb:release-1.24 commit 80f1016"
    {
        echo "Update ${TARGET_DIR} with ${REPO_BASE}:${RELEASE} commit ${SOURCE_COMMIT}"
        echo
    } >"${TRAVIS_BUILD_DIR}"/message.txt

    echo "Committing changes..."
    git commit --file "${TRAVIS_BUILD_DIR}"/message.txt

    echo "Creating pull request..."
    export GITHUB_TOKEN=${GHE_TOKEN}
    hub pull-request --file "${TRAVIS_BUILD_DIR}"/message.txt --push -b "${CREATE_PR}"
    echo
    echo "Pull request successfully created"
    echo
else
    echo "The files in ${TARGET_DIR} have been successfully updated"
    echo
fi
