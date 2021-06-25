#!/usr/bin/env bash
# ******************************************************************************
# IBM Cloud Kubernetes Service, 5737-D43
# (C) Copyright IBM Corp. 2020, 2021 All Rights Reserved.
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

git_branch=$(git branch | grep "\*" | cut -d' ' -f2)
count=1
current_tag="dev-${git_branch}-${count}"
username=$(git config user.name)

while true; do
	git tag -a "${current_tag}" -m "Tag to trigger dev deploy From: ${username}"
	return_code=$?
	if [[ ${return_code} -eq 0 ]]; then
		break
	fi

	count=$((count + 1))
	current_tag="dev-${git_branch}-${count}"
done

echo "TAG CREATED: ${current_tag}"

remote_branch=$(git remote -v | grep "push" | awk '{ print $1}')

git push "${remote_branch}" "${current_tag}"
return_code=$?

if [[ ${return_code} -eq 0 ]]; then
	echo "Successfully Triggered Dev Deploy"
else
	echo "Failed to Triggered Dev Deploy"
fi
