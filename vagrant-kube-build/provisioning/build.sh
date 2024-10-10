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

CLOUD_PROVIDER_IBM_BUILD_STEPS=("$@")

# The default includes all build steps.
DEFAULT_CLOUD_PROVIDER_IBM_BUILD_STEPS='setup source containers registry'
if [[ -z "${CLOUD_PROVIDER_IBM_BUILD_STEPS[0]}" ]]; then
    CLOUD_PROVIDER_IBM_BUILD_STEPS=("$DEFAULT_CLOUD_PROVIDER_IBM_BUILD_STEPS")
fi

echo "INFO: Building ${CLOUD_PROVIDER_IBM_BUILD_STEPS[*]} ..."

# Other steps are optional.
build_source=0; build_containers=0; build_registry=0; build_make=0;

# Specific make will override build steps.
if [[ ${CLOUD_PROVIDER_IBM_BUILD_STEPS[0]} == "make" ]]; then
    build_make=1
else
    # shellcheck disable=SC2048
    for BUILD_STEP in ${CLOUD_PROVIDER_IBM_BUILD_STEPS[*]}; do
        case $BUILD_STEP in
            'setup') ;;
            'source') build_source=1;;
            'containers') build_containers=1;;
            'registry') build_registry=1;;
            'make')
                echo "ERROR: Build step 'make' cannot be done with other build steps."
                exit 1;;
            *)
                echo "ERROR: Build step '${BUILD_STEP}' is not valid. Valid build steps: ${DEFAULT_CLOUD_PROVIDER_IBM_BUILD_STEPS} make"
                exit 1
        esac
    done
fi

function exit_build {
    echo "ERROR: Build failed"
    exit 1
}

BUILD_PWD=$PWD
echo "Build directory is $BUILD_PWD."

export GOPATH="${BUILD_PWD}/go"
export GO_CLOUD_PROVIDER_IBM_MODULE="cloud.ibm.com/cloud-provider-ibm"
export GO_CLOUD_PROVIDER_IBM_DIR="${GOPATH}/src/${GO_CLOUD_PROVIDER_IBM_MODULE}"
GO_CLOUD_PROVIDER_IBM_DIR_BASENAME=$(basename "${GO_CLOUD_PROVIDER_IBM_DIR}" || exit_build)
export GO_CLOUD_PROVIDER_IBM_DIR_BASENAME
if ! grep -q "GOPATH" ~/.bashrc; then
    echo "export GOPATH=${GOPATH}" >> ~/.bashrc
    echo "cd ${GO_CLOUD_PROVIDER_IBM_DIR}" >> ~/.bashrc
fi

# Copy all hidden files as well using dotglob shell option.
shopt -s dotglob
echo "Copying ${BUILD_PWD}/${GO_CLOUD_PROVIDER_IBM_DIR_BASENAME} to $GO_CLOUD_PROVIDER_IBM_DIR ..."
if ! rm -rf "$GO_CLOUD_PROVIDER_IBM_DIR"; then exit_build ; fi
if ! mkdir -p "$GO_CLOUD_PROVIDER_IBM_DIR"; then exit_build ; fi
# shellcheck disable=SC2086
if ! cp -r ${BUILD_PWD}/${GO_CLOUD_PROVIDER_IBM_DIR_BASENAME}/* "$GO_CLOUD_PROVIDER_IBM_DIR"; then exit_build ; fi
shopt -u dotglob

echo "Adding symbolic links to $GO_CLOUD_PROVIDER_IBM_DIR build scripts ..."
for BUILD_SCRIPT in build.sh build_docker_registry.sh; do
    if [[ ! -e $BUILD_SCRIPT ]]; then
        ln -s "${GO_CLOUD_PROVIDER_IBM_DIR}/vagrant-kube-build/provisioning/${BUILD_SCRIPT}" "$BUILD_SCRIPT"
    fi
done

echo "Working directory is $GO_CLOUD_PROVIDER_IBM_DIR."

cd "$GO_CLOUD_PROVIDER_IBM_DIR" || exit_build

# NOTE(rtheis): Applicable build steps should align with .travis.yml.

if [[ $build_source -eq 1 || $build_containers -eq 1 ]]; then
    if ! make clean; then exit_build ; fi
fi

if [[ $build_source -eq 1 ]]; then
    if ! make oss; then exit_build ; fi

    if ! make fmt; then exit_build ; fi

    if ! make lint; then exit_build ; fi

    if ! make lint-sh; then exit_build ; fi

    if ! make lint-copyright; then exit_build ; fi

    if ! make yamllint; then exit_build ; fi

    if ! make vet; then exit_build ; fi

    if ! make test; then exit_build ; fi

    if ! make coverage; then exit_build ; fi

    if ! ./scripts/calculateCoverage.sh; then exit_build ; fi

    if ! make commands; then exit_build ; fi

    make runanalyzedeps
fi

if [[ $build_containers -eq 1 ]]; then
    if ! make commands; then exit_build ; fi

    # Vagrant user update to put it in the docker group won't take effect in this shell until
    # vagrant completes this run, so need to get a new shell and run commands there.
    if ! sudo su - vagrant -c "cd $GO_CLOUD_PROVIDER_IBM_DIR; make containers"; then exit_build ; fi
fi

if [[ $build_registry -eq 1 ]]; then
    if ! cd "${BUILD_PWD}" && ./build_docker_registry.sh; then exit_build ; fi
fi

if [[ $build_make -eq 1 ]]; then
    if ! eval "${CLOUD_PROVIDER_IBM_BUILD_STEPS[*]}"; then exit_build ; fi
fi

exit 0
