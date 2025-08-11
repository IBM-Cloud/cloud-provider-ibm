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
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
)

/*
Instances cloud provider interface must be implemented.
*/
func (c *Cloud) Instances() (cloudprovider.Instances, bool) {
	return nil, false
}

// NodeAddresses returns the addresses of the specified instance.
func (c *Cloud) NodeAddresses(ctx context.Context, name types.NodeName) ([]v1.NodeAddress, error) {
	return []v1.NodeAddress{}, cloudprovider.NotImplemented
}

// NodeAddressesByProviderID returns the addresses of the specified instance.
// The instance is specified using the providerID of the node. The
// ProviderID is a unique identifier of the node. This will not be called
// from the node whose nodeaddresses are being queried. i.e. local metadata
// services cannot be used in this method to obtain nodeaddresses
func (c *Cloud) NodeAddressesByProviderID(ctx context.Context, providerID string) ([]v1.NodeAddress, error) {
	return []v1.NodeAddress{}, cloudprovider.NotImplemented
}

// InstanceID returns the cloud provider ID of the node with the specified NodeName.
// Note that if the instance does not exist, we must return ("", cloudprovider.InstanceNotFound)
// cloudprovider.InstanceNotFound should NOT be returned for instances that exist but are stopped/sleeping
func (c *Cloud) InstanceID(ctx context.Context, nodeName types.NodeName) (string, error) {
	return "", cloudprovider.NotImplemented
}

// InstanceType returns the type of the specified instance.
func (c *Cloud) InstanceType(ctx context.Context, name types.NodeName) (string, error) {
	return "", cloudprovider.NotImplemented
}

// InstanceTypeByProviderID returns the type of the specified instance.
func (c *Cloud) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	return "", cloudprovider.NotImplemented
}

// AddSSHKeyToAllInstances adds an SSH public key as a legal identity for all instances
// expected format for the key is standard ssh-keygen format: <protocol> <blob>
func (c *Cloud) AddSSHKeyToAllInstances(ctx context.Context, user string, keyData []byte) error {
	return cloudprovider.NotImplemented
}

// CurrentNodeName returns the name of the node we are currently running on
// On most clouds (e.g. GCE) this is the hostname, so we provide the hostname
func (c *Cloud) CurrentNodeName(ctx context.Context, hostname string) (types.NodeName, error) {
	return "", cloudprovider.NotImplemented
}

// InstanceExistsByProviderID returns true if the instance for the given provider exists.
// If false is returned with no error, the instance will be immediately deleted by the cloud controller manager.
// This method should still return true for instances that exist but are stopped/sleeping.
func (c *Cloud) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	return false, cloudprovider.NotImplemented
}

// InstanceShutdownByProviderID returns true if the instance is shutdown in cloudprovider.
func (c *Cloud) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	return false, cloudprovider.NotImplemented
}

/*
InstancesV2 cloud provider interface must be implemented.
*/

func (c *Cloud) InstancesV2() (cloudprovider.InstancesV2, bool) {
	return c, true
}

// InstanceExists returns true if the instance for the given node exists according to the cloud provider.
// Use the node.name or node.spec.providerID field to find the node in the cloud provider.
func (c *Cloud) InstanceExists(ctx context.Context, node *v1.Node) (bool, error) {
	// NOTE(rtheis): Returning an error causes Kubernetes to add unnecessary
	// error messages to the logs. To avoid this noise, we'll continue assuming
	// the instance exists, but no longer return cloudprovider.NotImplemented
	// error.
	return true, nil
}

// InstanceShutdown returns true if the instance is shutdown according to the cloud provider.
// Use the node.name or node.spec.providerID field to find the node in the cloud provider.
func (c *Cloud) InstanceShutdown(ctx context.Context, node *v1.Node) (bool, error) {
	return false, nil
}

// InstanceMetadata returns the instance's metadata. The values returned in InstanceMetadata are
// translated into specific fields and labels in the Node object on registration.
// Implementations should always check node.spec.providerID first when trying to discover the instance
// for a given node. In cases where node.spec.providerID is empty, implementations can use other
// properties of the node like its name, labels and annotations.
func (c *Cloud) InstanceMetadata(ctx context.Context, node *v1.Node) (*cloudprovider.InstanceMetadata, error) {
	nodeMD, err := c.Metadata.GetNodeMetadata(node.Name, c.Config.Kubernetes.SetNetworkUnavailable, c.Config.Kubernetes.CniProvider)
	if err != nil {
		return nil, err
	}

	providerID := c.providerIDV2(nodeMD)
	instanceType := c.instanceTypeV2(nodeMD)
	nodeAddresses := c.nodeAddressesV2(nodeMD)

	instanceMetadata := cloudprovider.InstanceMetadata{
		ProviderID:    providerID,
		InstanceType:  instanceType,
		NodeAddresses: nodeAddresses,
		Zone:          nodeMD.FailureDomain,
		Region:        nodeMD.Region,
	}

	return &instanceMetadata, nil
}

// get provider id from node labels
func (c *Cloud) providerIDV2(nodeMD NodeMetadata) string {
	if nodeMD.ProviderID != "" {
		return nodeMD.ProviderID
	}
	// construct provider id from config and node metadata
	return fmt.Sprintf("ibm://%s///%s/%s", c.Config.Prov.AccountID, c.Config.Prov.ClusterID, nodeMD.WorkerID)
}

// Get instance type from node labels
func (c *Cloud) instanceTypeV2(nodeMD NodeMetadata) string {
	return nodeMD.InstanceType
}

// Get node addresses from node labels
func (c *Cloud) nodeAddressesV2(nodeMD NodeMetadata) []v1.NodeAddress {
	// ExternalIP may not be provided by metadata for private-only nodes, but
	// we will return one in case external consumers depend on it.
	externalIP := nodeMD.ExternalIP
	if len(externalIP) == 0 {
		externalIP = nodeMD.InternalIP
	}
	// Build and return node nodeaddresses - if they are non-empty
	nodeAddress := []v1.NodeAddress{}
	if len(nodeMD.InternalIP) > 0 {
		nodeAddress = append(nodeAddress, v1.NodeAddress{Type: v1.NodeInternalIP, Address: nodeMD.InternalIP})
	}
	if len(externalIP) > 0 {
		nodeAddress = append(nodeAddress, v1.NodeAddress{Type: v1.NodeExternalIP, Address: externalIP})
	}
	return nodeAddress
}
