/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2023 All Rights Reserved.
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

package classic

import (
	"reflect"
	"runtime"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
)

// CloudTaskFunc is the cloud task function signature.
type CloudTaskFunc func(c *Cloud, data map[string]string)

// CloudTask is the cloud task data.
type CloudTask struct {
	// Name of the cloud task built from the task function
	Name string
	// Interval between each run of the cloud task
	Interval time.Duration
	// Ticker to run the cloud task on the specified interval
	Ticker *time.Ticker
	// Stopper to stop the cloud task
	Stopper chan time.Time
	// Function to run for the cloud task
	TaskFunc CloudTaskFunc
	// Persistent data for the cloud task function
	FuncData map[string]string
}

// getCloudTaskName returns the cloud task name given the cloud task function.
func getCloudTaskName(taskFunc CloudTaskFunc) string {
	return runtime.FuncForPC(reflect.ValueOf(taskFunc).Pointer()).Name()
}

// StartTask creates and runs a new cloud task as a go routine at the
// specified interval.
func (c *Cloud) StartTask(taskFunc CloudTaskFunc, interval time.Duration) {
	taskName := getCloudTaskName(taskFunc)
	if _, found := c.CloudTasks[taskName]; !found {
		ct := CloudTask{
			Name:     taskName,
			Interval: interval,
			Ticker:   time.NewTicker(interval),
			Stopper:  make(chan time.Time),
			TaskFunc: taskFunc,
			FuncData: map[string]string{},
		}
		c.CloudTasks[taskName] = &ct
		klog.Infof("Starting cloud task: %v", ct.Name)
		go ct.run(c)
	} else {
		klog.Infof("Cloud task already started: %v", taskName)
	}
}

// StopTask stops an existing cloud task
func (c *Cloud) StopTask(taskFunc CloudTaskFunc) {
	taskName := getCloudTaskName(taskFunc)
	if ct, found := c.CloudTasks[taskName]; found {
		klog.Infof("Stopping cloud task: %v", ct.Name)
		ct.stop()
		delete(c.CloudTasks, taskName)
	} else {
		klog.Infof("No cloud task to stop: %v", taskName)
	}
}

// run the cloud task to monitor for ticks and a stopper
func (ct *CloudTask) run(c *Cloud) {
	klog.Infof("Running cloud task: %v", ct.Name)
	defer utilruntime.HandleCrash()
	var taskStartTime time.Time
	var taskRunDuration time.Duration
	maxTaskRunDuration := time.Duration(ct.Interval.Nanoseconds() / 2)
	for {
		select {
		case <-ct.Stopper:
			klog.Infof("Stopper on cloud task: %v", ct.Name)
			return
		case <-ct.Ticker.C:
			taskStartTime = time.Now()
			ct.TaskFunc(c, ct.FuncData)
			// Ensure that the cloud task isn't constantly running
			// by enforcing a sleep.
			taskRunDuration = time.Since(taskStartTime)
			if taskRunDuration > maxTaskRunDuration {
				klog.Warningf("Cloud task exceed maximum expected run duration: %v, %v", ct.Name, taskRunDuration)
				time.Sleep(ct.Interval)
			}
		}
	}
}

// stop the cloud task
func (ct *CloudTask) stop() {
	ct.Ticker.Stop()
	close(ct.Stopper)
	<-ct.Stopper
}
