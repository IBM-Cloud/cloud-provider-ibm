/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2025 All Rights Reserved.
*
* SPDX-License-Identifier: Apache2.0
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*    http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*******************************************************************************/

package ibm

import (
	"strconv"
	"testing"
	"time"
)

const runCountKey = "runCount"

func cloudFunc(c *Cloud, data map[string]string) {
	runCount := data[runCountKey]
	if len(runCount) == 0 {
		data[runCountKey] = "1"
	} else {
		runCountI, _ := strconv.Atoi(runCount)
		data[runCountKey] = strconv.Itoa(runCountI + 1)
	}
	time.Sleep(time.Second)
}

func TestTask(t *testing.T) {
	c := &Cloud{Name: "ibm", CloudTasks: map[string]*CloudTask{}}

	// Start a new cloud task.
	c.StartTask(cloudFunc, time.Second*2)
	ctName := getCloudTaskName(cloudFunc)
	ct := c.CloudTasks[ctName]
	if nil == ct {
		t.Fatalf("No cloud task created: %v", c.CloudTasks)
	}
	if len(c.CloudTasks) != 1 {
		t.Fatalf("Unexpected number of cloud tasks created: %v", c.CloudTasks)
	}
	if ct.Name != ctName {
		t.Fatalf("Unexpected cloud task name")
	}

	// Verify another cloud task isn't started for the same function.
	c.StartTask(cloudFunc, time.Second)
	if len(c.CloudTasks) != 1 {
		t.Fatalf("Unexpected number of cloud tasks created: %v", c.CloudTasks)
	}

	// Stop and verify cloud task was removed.
	time.Sleep(time.Second * 5)
	c.StopTask(cloudFunc)
	time.Sleep(time.Second * 5)
	if len(c.CloudTasks) != 0 {
		t.Fatalf("Unexpected number of cloud tasks exist: %v", c.CloudTasks)
	}

	// Stop cloud task that does not exist.
	c.StopTask(cloudFunc)
	if len(c.CloudTasks) != 0 {
		t.Fatalf("Unexpected number of cloud tasks exist: %v", c.CloudTasks)
	}
}
