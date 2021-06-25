/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2019, 2021 All Rights Reserved.
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

package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestCommandVersion(t *testing.T) {
	// if TEST_COMMAND_VERSION is set we are inside the process created below
	if os.Getenv("TEST_COMMAND_VERSION") == "1" {
		os.Args = []string{"ibm-cloud-controller-manager", "--version"}
		main()
		return
	}

	// need to run in separate process because --version flag perform os.Exit(0);
	// which causes normal testing to fail
	cmdName := os.Args[0]
	cmd := exec.Command(cmdName, "-test.run=TestCommandVersion")
	cmd.Env = append(os.Environ(), "TEST_COMMAND_VERSION=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		t.Fatalf("TestCommandVersion process exited with err: %v", err)
	}
}

func TestCommandHelp(t *testing.T) {
	os.Args = []string{"ibm-cloud-controller-manager", "--help"}
	main()
}
