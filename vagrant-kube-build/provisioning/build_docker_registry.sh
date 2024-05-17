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

function exit_build {
    echo "ERROR: build_docker_registry.sh failed"
    exit 1
}

BUILD_VERSION="v1.29.5"
BUILD_REGISTRY_NAME="armada-master"
BUILD_REGISTRY_IP_ADDRESS="192.168.10.10"
BUILD_REGISTRY_PORT="5000"
BUILD_REGISTRY_URL="${BUILD_REGISTRY_IP_ADDRESS}:${BUILD_REGISTRY_PORT}"
BUILD_IBM_CCM_IMAGE="${BUILD_REGISTRY_NAME}/ibm-cloud-controller-manager:${BUILD_VERSION}-0"

echo "Setting up docker registry ..."

# Configure access to the local docker registry.
if ! sudo ls /etc/docker/daemon.json >/dev/null 2>&1; then
    echo "{ \"insecure-registries\": [\"${BUILD_REGISTRY_URL}\"] }" > /tmp/docker-daemon.json
    sudo cp /tmp/docker-daemon.json /etc/docker/daemon.json
    sudo service docker restart
    sleep 5
fi

# Start the docker registry container.
if ! sudo docker inspect registry >/dev/null 2>&1; then
    if ! sudo docker run -d -p "${BUILD_REGISTRY_PORT}:${BUILD_REGISTRY_PORT}" --name registry registry:2; then exit_build ; fi
else
    if ! sudo docker start registry; then exit_build ; fi
fi

# Wait (maximum of 30 seconds) for the docker registry container to start.
echo "Waiting for docker registry container to start ..."
for _ in $(seq 1 10); do
    sleep 3
    if curl ${BUILD_REGISTRY_URL} >/dev/null 2>&1; then
        echo "Docker registry container has started."
        break
    fi
done

# Add container images to the local docker registry.
for BUILD_IMAGE in ${BUILD_IBM_CCM_IMAGE}; do
    echo "Adding $BUILD_IMAGE to the docker registry ..."
    if ! sudo docker tag "$BUILD_IMAGE" ${BUILD_REGISTRY_URL}/"${BUILD_IMAGE}"; then exit_build ; fi
    if ! sudo docker push ${BUILD_REGISTRY_URL}/"${BUILD_IMAGE}"; then exit_build ; fi
    if ! sudo docker image ls ${BUILD_REGISTRY_URL}/"${BUILD_IMAGE}"; then exit_build ; fi
done

echo "Completed docker registry setup."
exit 0
