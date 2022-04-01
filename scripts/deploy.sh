#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2019, 2022 All Rights Reserved.
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

DOCKER_IMAGE_NAME="${1}"
DOCKER_IMAGE_TAG="${2}"

# Push the docker image.
./build-tools/docker/pushDockerImage.sh "${DOCKER_IMAGE_NAME}" "${DOCKER_IMAGE_TAG}"

# Initialize image, kube release, and tag information
bom_image="k8s_cloud_controller_manager_image"
current_release=$(grep "^TAG ?=" Makefile | awk '{ print $3 }')
kube_major=$(echo "${current_release}" | cut -d'.' -f1 | tr -d 'v')
kube_minor=$(echo "${current_release}" | cut -d'.' -f2)
image_name=$(echo "${DOCKER_IMAGE_NAME}" | cut -d'/' -f2)
new_image_tag=${DOCKER_IMAGE_TAG}

# Clone the armada-ansible repo
git clone --depth=1 --no-single-branch "https://${GHE_USER}:${GHE_TOKEN}@github.ibm.com/alchemy-containers/armada-ansible.git"

cd armada-ansible/common/bom/next
bom_file_list=$(grep "^${bom_image}:" ./* | grep ":v${kube_major}.${kube_minor}." | cut -d':' -f1)

for file in $bom_file_list; do
    echo "Updating BOM file ${file} image ${bom_image} with new tag ${new_image_tag} ..."

    # Pull out the line that starts with the bom_image variable.
    line=$(grep "^${bom_image}:" "${file}")

    # Find the current image tag
    prev_image_tag=$(echo "${line##*:}" | tr -d "'")

    # Update the file
    sed -i "s,${image_name}:${prev_image_tag},${image_name}:${new_image_tag}," "${file}"

    # Display updated file
    grep "^${bom_image}:" "${file}"

    # Add file to the PR
    git add "$file"
done

echo "Create new branch ..."
git checkout -b "armdada-lb-${new_image_tag}"

echo "Disaply changes for this PR..."
git status

# Determine the contents of the pull request message
cd "${TRAVIS_BUILD_DIR}"
if [[ "${DOCKER_IMAGE_TAG}" = dev-* ]]; then
    dev_branch=${new_image_tag#"dev-"}
    dev_branch=${dev_branch%-*}
    pr_option="--draft"
    echo "${DOCKER_IMAGE_TAG} is a dev image"
    {
        echo "DNM: Test BOM for ${image_name} - ${new_image_tag}"
        echo
        echo "### Do not merge. Test only."
        echo
        echo "To create TEST BOM, use Jenkins job: [armada-test-bom-publish](https://alchemy-containers-jenkins.swg-devops.com/job/Containers-Runtime/view/Armada-BOM/job/armada-test-bom-publish/build?delay=0sec)"
        echo "with the following settings:"
        echo
        echo "- ARMADA_ANSIBLE_BRANCH: *armada-lb-${new_image_tag}*"
        echo "- BOM_FILES: *select the BOM files changed in this PR*"
        echo "- BOM_TYPE: *next*"
        echo "- CARRIERS: *prestage-mon01-carrier1*"
        echo
        echo "### Commits to [${dev_branch}](https://github.ibm.com/alchemy-containers/armada-lb/commits/${dev_branch}) since ${prev_image_tag}"
        echo
        git log --no-patch --abbrev-commit --no-color --oneline "${prev_image_tag}"..."${new_image_tag}"
        echo
    } >"${TRAVIS_BUILD_DIR}"/message.txt
else
    kube_branch="release-${kube_major}.${kube_minor}"
    pr_option=""
    {
        echo "Update ${image_name} to ${new_image_tag}"
        echo
        echo "### Commits to [armada-lb](https://github.ibm.com/alchemy-containers/armada-lb/commits/${kube_branch}) since ${prev_image_tag}"
        echo
        git log --no-patch --abbrev-commit --no-color "${prev_image_tag}"..."${new_image_tag}"
        echo
    } >"${TRAVIS_BUILD_DIR}"/message.txt
fi
cat "${TRAVIS_BUILD_DIR}"/armada-ansible/.github/pull_request_template.md >>"${TRAVIS_BUILD_DIR}"/message.txt
cd ./armada-ansible

echo "Comitting changes..."
git commit --file "${TRAVIS_BUILD_DIR}"/message.txt

echo "Creating pull request..."
export GITHUB_TOKEN=${GHE_TOKEN}
hub pull-request --file "${TRAVIS_BUILD_DIR}"/message.txt --push "${pr_option}"
