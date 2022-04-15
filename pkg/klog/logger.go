/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2022 All Rights Reserved.
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

// Package klog provides standard formatting
package klog

import (
	"fmt"
	"time"

	"k8s.io/klog/v2"
)

// Variable to determine which type of logging should be done
var logStdout = false

func SetOutputToStdout() {
	logStdout = true
}

// Errorf ...
func Errorf(format string, v ...interface{}) {
	if logStdout {
		fmt.Printf("ERROR: "+format+"\n", v...)
	} else {
		s := fmt.Sprintf(format, v...)
		klog.ErrorDepth(1, s)
	}
}

// Infof ...
func Infof(format string, v ...interface{}) {
	if logStdout {
		timestamp := time.Now().Format("15:04:05.0000")
		fmt.Printf("INFO: ["+timestamp+"] "+format+"\n", v...)
	} else {
		s := fmt.Sprintf(format, v...)
		klog.InfoDepth(1, s)
	}
}

// Warningf ...
func Warningf(format string, v ...interface{}) {
	if logStdout {
		fmt.Printf("WARNING: "+format+"\n", v...)
	} else {
		s := fmt.Sprintf(format, v...)
		klog.WarningDepth(1, s)
	}
}
