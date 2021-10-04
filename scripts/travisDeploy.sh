#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2019, 2021 All Rights Reserved.
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
BUILD_PIPELINE_TAG="${3}"

# Push the docker image.
./build-tools/docker/pushDockerImage.sh "${DOCKER_IMAGE_NAME}" "${DOCKER_IMAGE_TAG}"

# Update the pipeline to use the docker image.
git clone --depth=1 --no-single-branch "https://${GHE_USER}:${GHE_TOKEN}@github.ibm.com/alchemy-containers/armada-ansible.git"
BOM_IMAGE="k8s_cloud_controller_manager_image"
BOM_IMAGE_TAG="${DOCKER_IMAGE_TAG}"
BOM_FILE_NAME=$(echo "${BUILD_PIPELINE_TAG}" | awk -F'[v.]' '{ print "armada-ansible-bom-"$2"."$3".yml" }')
echo "Updating BOM ${BOM_FILE_NAME} image ${BOM_IMAGE} with new tag ${BOM_IMAGE_TAG} ..."
export BOM_FILE_NAME
armada-ansible/common/bom/tools/update-bom-image-tags.sh "${BOM_IMAGE}" "${BOM_IMAGE_TAG}"

# OpenShift 4.9 uses the 1.22 version of the IBM CCM.
export BOM_FILE_NAME="openshift-target-bom-4.9.yml"

echo "Updating BOM ${BOM_FILE_NAME} image ${BOM_IMAGE} with new tag ${BOM_IMAGE_TAG} ..."
armada-ansible/common/bom/tools/update-bom-image-tags.sh "${BOM_IMAGE}" "${BOM_IMAGE_TAG}"
