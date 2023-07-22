/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2021, 2023 All Rights Reserved.
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
	"runtime/debug"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	nodePanicCooldownPeriod = 10
)

func (c *Cloud) handleNodeWatchCrash() {
	if r := recover(); r != nil {
		klog.Errorf("Background Node Watch Process StackTrace: %v \nBackground Node Watch Process Panic Error: %v", string(debug.Stack()), r)
		// Cool down period before retrying.
		time.Sleep(time.Second * nodePanicCooldownPeriod)
		klog.Info("Recovered panic in background node watcher")
	}
}

// Main logic to handle node deletions
func (c *Cloud) handleNodeDelete(obj interface{}) {

	// Catch all panics that come from the node watch, sleep then close the channel to allow a restart
	defer c.handleNodeWatchCrash()

	node, isNode := obj.(*v1.Node)
	// We can get DeletedFinalStateUnknown instead of *v1.Node here
	// and we need to handle that correctly.
	if !isNode {
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Received unexpected object: %v", obj)
			return
		}
		node, ok = deletedState.Obj.(*v1.Node)
		if !ok {
			klog.Errorf("DeletedFinalStateUnknown contained non-Node object: %v", deletedState.Obj)
			return
		}
	}
	klog.Infof("Removing deleted node from metadata cache: %s", node.Name)
	c.Metadata.deleteCachedNode(node.Name)
}
