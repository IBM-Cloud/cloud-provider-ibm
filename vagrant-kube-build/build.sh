#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2017, 2021 All Rights Reserved.
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

key_file=~/.ssh/id_rsa
if ! ssh-add -L | grep -q $key_file; then
     ssh-add $key_file
fi

if vagrant status --machine-readable | grep state,running; then
    echo "INFO: Vagrant is running, running provision ..."
    CLOUD_PROVIDER_IBM_BUILD_STEPS="${CLOUD_PROVIDER_IBM_BUILD_STEPS[*]}" vagrant provision
else
    echo "INFO: Bringing up and provisioning vm ..."
    CLOUD_PROVIDER_IBM_BUILD_STEPS="${CLOUD_PROVIDER_IBM_BUILD_STEPS[*]}" vagrant up --provision
fi
exit 0
