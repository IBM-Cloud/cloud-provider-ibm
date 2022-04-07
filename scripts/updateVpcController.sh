#!/bin/bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2022 All Rights Reserved.
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

ORIGINAL_VPCCTL_LIBRARY="github.com/IBM-Cloud/cloud-provider-vpc-controller"
set -e

# Verify that opensource library is still listed in the go.mod
if ! grep -q "$ORIGINAL_VPCCTL_LIBRARY" go.mod; then
    echo "go.mod does not contain: $ORIGINAL_VPCCTL_LIBRARY"
    exit 0
fi

# Determine current line in the go.mod, the new vpc controller library, and the new release
CURRENT_VPCCTL_LINE=$(grep "$ORIGINAL_VPCCTL_LIBRARY" go.mod | awk '{print $2 " " $3}')
NEW_VPCCTL_LIBRARY=$(awk '/^source:/{print $2}' "addons/vpcctl.yml")
NEW_VPCCTL_RELEASE=$(awk '/^release:/{print $2}' "addons/vpcctl.yml")

# Update go.mod to use new vpcctl library
cp go.mod go.mod.bak
grep -v "$ORIGINAL_VPCCTL_LIBRARY" go.mod.bak >go.mod
rm go.mod.bak
echo "replace $CURRENT_VPCCTL_LINE => $NEW_VPCCTL_LIBRARY $NEW_VPCCTL_RELEASE " >>go.mod

# Update GO/git so that alternate vpc controller library can be used
if echo "$NEW_VPCCTL_LIBRARY" | grep -q "github.ibm.com"; then
    echo "Allow access to github.ibm.com"
    export GOPRIVATE=github.ibm.com
    export GONOPROXY=github.ibm.com
    git config --global url."git@github.ibm.com:".insteadOf "https://github.ibm.com/"
fi

# Update go.sum based on the new vpc controller library
echo "Refresh go dependencies for new vpc controller library"
go mod tidy
