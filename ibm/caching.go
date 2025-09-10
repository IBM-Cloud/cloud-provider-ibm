/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2025 All Rights Reserved.
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
	"time"

	"k8s.io/client-go/tools/cache"
)

// nodeMetadataCacheTTL is duration of time to store the vm ip in cache
// Currently the default sync period is 5 minutes that means every 5 minutes
// there will be a reconciliation, So setting cache timeout to 15 minutes so the cache updates will happen
// once in 3 reconciliations.
const nodeMetadataCacheTTL = time.Duration(15) * time.Minute

// nodeMetadataCache holds the node name and corresponding node's metadata.
type nodeMetadataCache struct {
	Name     string
	Metadata *NodeMetadata
}

// cacheKeyFunc defines the key function required in TTLStore.
func cacheKeyFunc(obj interface{}) (string, error) {
	return obj.(nodeMetadataCache).Name, nil
}

// initialiseDHCPCacheStore returns a new cache store.
func initialiseDHCPCacheStore() cache.Store {
	return cache.NewTTLStore(cacheKeyFunc, nodeMetadataCacheTTL)
}
