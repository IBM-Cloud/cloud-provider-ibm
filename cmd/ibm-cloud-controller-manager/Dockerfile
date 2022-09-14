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

# The gcr.io/distroless/static image doesn't have semantic version tags.
# Therefore, the latest fully-qualified image digest can be found by pointing
# your browser to "gcr.io/distroless/static:nonroot". We avoid using "nonroot"
# tag in order to have a deterministic build. For more information on
# distroless images, see https://github.com/GoogleContainerTools/distroless.
# The Kubernetes version update process will keep this image up-to-date.
ARG IMAGE_SOURCE=gcr.io
FROM $IMAGE_SOURCE/distroless/static@sha256:2a9e2b4fa771d31fe3346a873be845bfc2159695b9f90ca08e950497006ccc2e

# Add Labels to image to show build details
ARG this_build_id=unknown
ARG this_build_tag=unknown
ARG this_build_sha=unknown
ARG REPO_SOURCE_URL=blank
ARG BUILD_URL=blank
LABEL "ibm-cloud-controller-manager.build"="$this_build_id"
LABEL "ibm-cloud-controller-manager.build.tag"="$this_build_tag"
LABEL "ibm-cloud-controller-manager.build.sha"="$this_build_sha"
LABEL compliance.owner="ibm"
LABEL razee.io.source-url="${REPO_SOURCE_URL}"
LABEL razee.io.build-url="${BUILD_URL}"
ENV BUILD_ID=${this_build_id}
ENV BUILD_TAG=${this_build_tag}
ENV BUILD_SHA=${this_build_sha}

WORKDIR /bin/
ADD cmd/ibm-cloud-controller-manager/calicoctl /bin/
ADD ibm-cloud-controller-manager /bin/
ENTRYPOINT ["/bin/ibm-cloud-controller-manager"]
