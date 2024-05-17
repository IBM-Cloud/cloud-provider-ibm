#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2019, 2024 All Rights Reserved.
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
set -xe

# Parse input options.
FORCE_TAG=false
while getopts "f" opt; do
    case ${opt} in
        f)
          FORCE_TAG=true
          ;;
        \?)
          echo "Usage: publishTag.sh [-f]"
          exit 1
          ;;
    esac
done
shift $((OPTIND-1))

# Determine if the current commit has a tag. If so, then the caller must
# force a another tag on the commit.
EXISTING_TAG=$(git tag --list --points-at HEAD)
if [[ -n "${EXISTING_TAG}" && "${FORCE_TAG}" != "true" ]]; then
    echo "INFO: Current commit already tagged as ${EXISTING_TAG}. Use -f to force another tag."
    exit 1
fi

# Set default tag.
BASE_TAG="v1.29.5"

# Determine the next build tag.
BASE_TAGS=$(git tag --list "${BASE_TAG}-[0-9]*" --sort=v:refname)
LAST_BUILD_TAG=$(echo "${BASE_TAGS}" | awk -F- '{ print $NF }' | tail -1)
if [[ -n "${LAST_BUILD_TAG}" ]]; then
    NEXT_BUILD_TAG=$((LAST_BUILD_TAG+1))
else
    NEXT_BUILD_TAG=1
fi

# Publish the tag.
TAG="${BASE_TAG}-${NEXT_BUILD_TAG}"
git tag ${TAG}
git push origin ${TAG}

exit 0
