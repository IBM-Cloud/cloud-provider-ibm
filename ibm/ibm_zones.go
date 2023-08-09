/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2021, 2023 All Rights Reserved.
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
	"context"

	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
)

/*
Zones cloud provider interface must be implemented in order to support the
LoadBalancer interface.
*/
func (c *Cloud) Zones() (cloudprovider.Zones, bool) {
	return c, true
}

// GetZone returns the Zone containing the current failure zone and locality region that the program is running in
// In most cases, this method is called from the kubelet querying a local metadata service to acquire its zone.
// For the case of external cloud providers, use GetZoneByProviderID or GetZoneByNodeName since GetZone
// can no longer be called from the kubelets.
func (c *Cloud) GetZone(ctx context.Context) (cloudprovider.Zone, error) {
	return cloudprovider.Zone{
		FailureDomain: c.Config.Prov.Zone,
		Region:        c.Config.Prov.Region,
	}, nil
}

// GetZoneByProviderID returns the Zone containing the current zone and locality region of the node specified by providerID
// This method is particularly used in the context of external cloud providers where node initialization must be done
// outside the kubelets.
func (c *Cloud) GetZoneByProviderID(ctx context.Context, providerID string) (cloudprovider.Zone, error) {
	// 1) in kubelet: okay as-is or fail - its not used
	// 2) in controller-manager: must fail for caller to try GetZoneByNodeName
	var zone cloudprovider.Zone
	return zone, cloudprovider.NotImplemented
}

// GetZoneByNodeName returns the Zone containing the current zone and locality region of the node specified by node name
// This method is particularly used in the context of external cloud providers where node initialization must be done
// outside the kubelets.
func (c *Cloud) GetZoneByNodeName(ctx context.Context, nodeName types.NodeName) (cloudprovider.Zone, error) {
	// 1) in kubelet: okay as-is or fail - its not used
	// 2) in controller-manager: get from node labels
	var zone cloudprovider.Zone
	if c.Metadata == nil {
		return zone, nil
	}
	nodeMd, err := c.Metadata.GetNodeMetadata(string(nodeName), false)
	if nil == err {
		zone = cloudprovider.Zone{
			FailureDomain: nodeMd.FailureDomain,
			Region:        nodeMd.Region,
		}
	}
	return zone, err
}
