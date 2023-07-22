#!/usr/bin/env bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2017, 2023 All Rights Reserved.
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

function print_fmt_msg {
    echo "============== $(date -u +'%D %T %Z') =================="
    echo -e "  $1"
    echo "======================================================="
}

function lint_file() {
    file="$1"
    if git status -s | grep -q "${file}"; then
        file_was_changed_this_year="true"
    else
        file_was_changed_this_year=$(git log --format="%aD" "${file}" | grep "${CURRENT_YEAR}")
    fi

    # Skip copyright check for files changes in the previous year.
    # We aren't fixing old copyrights until the files change.
    if [[ -z "${file_was_changed_this_year}" ]]; then
        return
    fi

    # Skip copyright checks for select test files.
    if [[ "${file}" == "test-fixtures/kdd-calico-config.yaml" ]]; then
        return
    fi
    if [[ "${file}" == "test-fixtures/kdd-calico-config_for_pkg_classic.yaml" ]]; then
        return
    fi

    file_name_was_displayed=0

    # Ensure files changed this year have their copyright updated accordingly.
    if ! head -10 "${file}" | grep "${COPYRIGHT_KEY}" | grep -q "${CURRENT_YEAR}"; then
        echo "ERROR FILE: ${file}"
        echo "    - YEAR STR EXPECTED TO BE IN THE COPYRIGHT: ${CURRENT_YEAR}"
        file_name_was_displayed=1
        LINT_FAILURE=true
    fi

    # Check if copyright is correct.
    file_missing_copyright_data=false
    file_head=$(head -10 "${file}")
    grep -q "${expected_service_name}" <<< "${file_head}" || file_missing_copyright_data=true
    grep -q "${expected_spdx_license_identifier}" <<< "${file_head}" || file_missing_copyright_data=true
    if [[ ${file_missing_copyright_data} == true ]]; then
        if [[ ${file_name_was_displayed} -ne 1 ]]; then
            LINT_FAILURE=true
            echo "ERROR FILE: ${file}"
            echo "    - MISSING DATA: ${expected_service_name} or ${expected_spdx_license_identifier}"
        fi
    fi
}

expected_service_name="IBM Cloud Kubernetes Service"
expected_spdx_license_identifier="SPDX-License-Identifier: Apache2.0"
CURRENT_YEAR=$(date +%Y)
COPYRIGHT_KEY="Copyright IBM Corp."
LINT_FAILURE=false

while (( "$#" )); do
    file=$(echo "$1" | cut -c 3-)
    lint_file "$file"
    shift
done
# Print out bad files if the don't have the copyright
if [[ ${LINT_FAILURE} == true ]]; then
    print_fmt_msg "Fail Copyright Test"
    exit 1
else
    print_fmt_msg "Success :) Passed Copyright Test"
fi
