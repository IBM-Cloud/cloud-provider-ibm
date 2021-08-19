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
# ==============================================================================
# This script will verify an existing file via md5 checksum and download a
# new binary or copy over from specified location if verification fails.
# ==============================================================================
# Arguments
# ------------------------------------------------------------------------------
# Positional:
#     - file:        file to verify against specified md5 hash, if the file
#                    exists; otherwise use new_source
#     - new_source:  source to use if specified file does not match provided md5
#                    hash
#     - md5_hash:    md5 checksum hash to verify specified file, if it exists,
#                    matches
# ==============================================================================
SCRIPT_DIR=$(dirname "${BASH_SOURCE[0]}")
# import shared common bash functions
# shellcheck source=scripts/common_functions.sh
. "${SCRIPT_DIR}/common_functions.sh"

set -ex

VERIFY_FILE="${1}"
NEW_SOURCE="${2}"
MD5_HASH="${3}"
CURL_HEADER="${4}"

if [[ -z ${VERIFY_FILE} || -z ${NEW_SOURCE} || -z ${MD5_HASH} ]]; then
    echo "Usage: ${0} <file> <new_source> <md5_hash> <curl_modifiers>"
    echo "        file        file to verify against specified md5 has, if the file exists; otherwise use new_source"
    echo "        new_source  source to use if specified file does not match provided md5 hash"
    echo "        md5_hash    md5 checksum hash to verify specified file, if it exists, matches"
    echo "        curl_modifiers additional parameters to pass to curl (optional)"
    exit 1
fi


# if the file already exists, verify it matches the specified md5 hash
if [[ -f ${VERIFY_FILE} ]]; then
    echo "Found existing file, verifying expected version via md5 hash..."
    set +e
    verify_checksum "${MD5_HASH}" "${VERIFY_FILE}"
    rc=$?
    set -e

    if [[ ${rc} -eq 0 ]]; then
        echo "Current file verified. Exiting."
        exit 0
    fi
    echo "Current file failed md5 checksum, will attempt to collect new copy via ${NEW_SOURCE}"
fi

# NOTE(cjschaef): if NEW_SOURCE refers to a local file, copy that file over; otherwise curl down the binary
if [[ -f "${NEW_SOURCE}" ]]; then
    echo "Copying file..."
    sudo cp "${NEW_SOURCE}" "${VERIFY_FILE}"
else
    echo "Downloading file..."
    curl_error_dump=$(mktemp /tmp/file_download_error-XXXX.txt)
    set +e
    if [[ -n "${CURL_HEADER}" ]]; then
        # shellcheck disable=SC2024,SC2086
        sudo curl -Lo "${VERIFY_FILE}" ${CURL_HEADER} "${NEW_SOURCE}" > "${curl_error_dump}" 2>&1
    else
        # shellcheck disable=SC2024
        sudo curl -Lo "${VERIFY_FILE}" "${NEW_SOURCE}" > "${curl_error_dump}" 2>&1
    fi
    rc=$?
    set -e

    if [[ ${rc} -ne 0 ]]; then
        echo "Failure attempting to download file, check curl results:  ${curl_error_dump}"
        exit 1
    fi
fi

# NOTE(cjschaef): verify new file matches expected checksum
set +e
verify_checksum "${MD5_HASH}" "${VERIFY_FILE}"
rc=$?
set -e

if [[ ${rc} -ne 0 ]]; then
    echo -e "Failure verifying collected file against provided m5d hash.\nVerify you have the correct file and md5 hash specified."
    exit 1
fi

exit 0
