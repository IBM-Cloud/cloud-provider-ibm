#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2023 All Rights Reserved.
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
echo "Preparing artifactory build setup."
curl -s https://s3.us.cloud-object-storage.appdomain.cloud/armada-build-tools-prod-us-geo/build-tools/build-tools.tar.gz | tar -xvz
./build-tools/install.sh
# shellcheck disable=SC1091
source ./build-tools/JFrog/setJfrtToken.sh
export ARTIFACTORY_TOKEN_PATH="/tmp/.artifactory-token-path"
echo "${ARTIFACTORY_JFRT_TOKEN}" > ${ARTIFACTORY_TOKEN_PATH}
docker login wcp-alchemy-containers-team-access-redhat-docker-remote.artifactory.swg-devops.com --username "${ARTIFACTORY_USER_NAME}" --password-stdin <<< "${ARTIFACTORY_JFRT_TOKEN}"
docker login wcp-alchemy-containers-team-gcr-docker-remote.artifactory.swg-devops.com --username "${ARTIFACTORY_USER_NAME}" --password-stdin <<< "${ARTIFACTORY_JFRT_TOKEN}"
mkdir -p ~/.pip/
echo "[global]" > ~/.pip/pip.conf
echo "index-url = https://na.artifactory.swg-devops.com/artifactory/api/pypi/wcp-alchemy-containers-team-pypi-remote/simple" >> ~/.pip/pip.conf
echo "machine na.artifactory.swg-devops.com login ${ARTIFACTORY_USER_NAME} password " >> ~/.netrc
cat "${ARTIFACTORY_TOKEN_PATH}" >> ~/.netrc
printf "Authorization: Bearer " > "${ARTIFACTORY_AUTH_HEADER_FILE}"
cat "${ARTIFACTORY_TOKEN_PATH}" >> "${ARTIFACTORY_AUTH_HEADER_FILE}"
