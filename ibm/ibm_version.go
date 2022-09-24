/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2021, 2022 All Rights Reserved.
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
	"fmt"
	"os"

	flag "github.com/spf13/pflag"
)

var (
	versionFlag bool
)

// Version is overwritten during builds.
var Version = "v1.25.2"

func PrintVersionAndExitIfRequested() {
	if versionFlag {
		fmt.Printf("%s\n", Version)
		os.Exit(0)
	}
}

// AddFlags registers this package's flags on arbitrary FlagSets, such that they point to the
// same value as the global flags.
func AddVersionFlag(fs *flag.FlagSet) {
	fs.BoolVar(&versionFlag, "version", false, "Print version and exit")
}
