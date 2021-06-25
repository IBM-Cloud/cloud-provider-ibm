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

function exit_build {
    echo "ERROR: push_image.sh <registry> <namespace> <name>"
    exit 1
}

ALT_REGISTRY="${1}"
ALT_NAMESPACE="${2}"
ALT_NAME="${3}"
if [[ -z "${ALT_REGISTRY}" || -z "${ALT_NAMESPACE}" || -z "${ALT_NAME}" ]]; then
    echo "ERROR: Registry, namespace or name not provided."
    exit_build
fi

LOCAL_REGISTRY="192.168.10.10:5000"
LOCAL_NAMESPACE="armada-master"
LOCAL_NAME="${ALT_NAME}"
LOCAL_IMAGE_NO_TAG="${LOCAL_REGISTRY}/${LOCAL_NAMESPACE}/${LOCAL_NAME}"
echo "{ \"insecure-registries\": [\"${LOCAL_REGISTRY}\", \"${ALT_REGISTRY}\"] }" > /tmp/docker-daemon.json
sudo cp /tmp/docker-daemon.json /etc/docker/daemon.json
sudo service docker restart
sleep 5

LOCAL_IMAGE=$(sudo docker image ls | grep "${LOCAL_IMAGE_NO_TAG}" | grep -v "<none>" | awk '{ print $1":"$2 }')
if [[ -z "${LOCAL_IMAGE}" ]]; then
    echo "ERROR: Local image '${LOCAL_IMAGE_NO_TAG}' not found."
    exit_build
fi

ALT_IMAGE=$(echo "${LOCAL_IMAGE}" | sed "s/${LOCAL_REGISTRY}/${ALT_REGISTRY}/" | sed "s/${LOCAL_NAMESPACE}/${ALT_NAMESPACE}/")
if [[ -z "${ALT_IMAGE}" ]]; then
    echo "ERROR: Failed to generate image name."
    exit_build
fi

if ! sudo docker tag "${LOCAL_IMAGE}" "${ALT_IMAGE}"; then
    echo "ERROR: Failed to tag image '${LOCAL_IMAGE}' as '${ALT_IMAGE}'."
    exit_build
fi

if ! sudo docker push "${ALT_IMAGE}"; then
    echo "ERROR: Failed to push image '${ALT_IMAGE}'."
    exit_build
fi

echo "SUCCESS: Pushed image '${ALT_IMAGE}'."
exit 0
