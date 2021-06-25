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


# Verify file matches checksum hash
function verify_checksum {
    # $1: expected md5 hash
    # $2: file
    echo "Verifying checksum of $1 and $2"
    if [[ $OSTYPE == "darwin"* ]]; then
        # MacOS does not have md5sum routine.  Have to use md5 instead
        md5hash=$(md5 -q "${2}")
        if [[ ${1} != "$md5hash" ]] ; then
            echo "Unexpected md5 hash on patched file ${2}."
            return 1
        fi
        return 0
    fi
    if ! echo "${1}  ${2}" | md5sum --check; then
        echo "Unexpected md5 hash on patched file ${2}."
        return 1
    fi
}
