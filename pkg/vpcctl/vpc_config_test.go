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

package vpcctl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var mockCloud = CloudVpc{KubeClient: fake.NewSimpleClientset()}

// Node without InternalIP label but with status
var mockNode1 = &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "192.168.1.1",
	Labels: map[string]string{nodeLabelZone: "zoneA", nodeLabelDedicated: nodeLabelValueEdge}}, Status: v1.NodeStatus{Addresses: []v1.NodeAddress{{Address: "192.168.1.1", Type: v1.NodeInternalIP}}}}

// Node with InteralIP label but without status
var mockNode2 = &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "192.168.2.2",
	Labels: map[string]string{nodeLabelZone: "zoneB", nodeLabelInternalIP: "192.168.2.2"}}}

// Node without InternalIP label and status
var mockNode3 = &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "192.168.3.3",
	Labels: map[string]string{nodeLabelZone: "zoneB"}}}

// Node without InternalIP label with nil Addresses status
var mockNode4 = &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "192.168.1.1",
	Labels: map[string]string{nodeLabelZone: "zoneA", nodeLabelDedicated: nodeLabelValueEdge}}, Status: v1.NodeStatus{Addresses: nil}}

func TestConfigVpc_getIamEndpoint(t *testing.T) {
	config := &ConfigVpc{
		Region: "us-south",
	}
	// Check prod public VPC endpoint
	url := config.getIamEndpoint()
	assert.Equal(t, url, iamPublicTokenExchangeURL)

	// Check prod private VPC endpoint
	config.EnablePrivate = true
	url = config.getIamEndpoint()
	assert.Equal(t, url, iamPrivateTokenExchangeURL)

	// Check stage public VPC endpoint
	config.EnablePrivate = false
	config.Region = "us-south-stage01"
	url = config.getIamEndpoint()
	assert.Equal(t, url, iamStageTestPublicTokenExchangeURL)

	// Check stage private VPC endpoint
	config.EnablePrivate = true
	url = config.getIamEndpoint()
	assert.Equal(t, url, iamStagePrivateTokenExchangeURL)
}

func TestConfigVpc_getVpcEndpoint(t *testing.T) {
	config := &ConfigVpc{
		Region: "us-south",
	}
	// Check prod public VPC endpoint
	url := config.getVpcEndpoint()
	assert.Equal(t, url, "https://us-south.iaas.cloud.ibm.com")

	// Check prod private VPC endpoint
	config.EnablePrivate = true
	url = config.getVpcEndpoint()
	assert.Equal(t, url, "https://us-south.private.iaas.cloud.ibm.com")

	// Check stage public VPC endpoint
	config.EnablePrivate = false
	config.Region = "us-south-stage01"
	url = config.getVpcEndpoint()
	assert.Equal(t, url, "https://us-south-stage01.iaasdev.cloud.ibm.com")

	// Check stage private VPC endpoint
	config.EnablePrivate = true
	url = config.getVpcEndpoint()
	assert.Equal(t, url, "https://us-south-stage01.private.iaasdev.cloud.ibm.com")
}

func TestConfigVpc_initialize(t *testing.T) {
	config := &ConfigVpc{
		AccountID:         "accountID",
		APIKeySecret:      "apiKey",
		ClusterID:         "clusterID",
		EnablePrivate:     false,
		Region:            "us-south",
		ResourceGroupName: "Default",
		SubnetNames:       "subnet1,subnet2,subnet3",
		VpcName:           "vpc",
	}
	// ProviderType = "invalid".  Error is returned
	config.ProviderType = "invalid"
	err := config.initialize()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Invalid cloud configuration setting")

	// ProviderType = "fake".  Endpoints are not assigned
	config.ProviderType = VpcProviderTypeFake
	err = config.initialize()
	assert.Nil(t, err)
	assert.Equal(t, config.endpointURL, "")
	assert.Equal(t, config.tokenExchangeURL, "")

	// ProviderType = "g2".  Endpoints are set
	config.ProviderType = VpcProviderTypeGen2
	err = config.initialize()
	assert.Nil(t, err)
	assert.Equal(t, config.endpointURL, "https://us-south.iaas.cloud.ibm.com/v1")
	assert.Equal(t, config.tokenExchangeURL, "https://iam.cloud.ibm.com/identity/token")
}

func TestConfigVpc_validate(t *testing.T) {
	config := &ConfigVpc{
		AccountID:         "accountID",
		APIKeySecret:      "apiKey",
		ClusterID:         "clusterID",
		EnablePrivate:     false,
		ProviderType:      VpcProviderTypeGen2,
		Region:            "us-south",
		ResourceGroupName: "Default",
		SubnetNames:       "subnet1,subnet2,subnet3",
		WorkerAccountID:   "accountID",
		VpcName:           "vpc",
	}
	// Verify valid config returns no error
	err := config.validate()
	assert.Nil(t, err)

	// AccountID not set
	config.AccountID = ""
	err = config.validate()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing required cloud configuration setting")
	config.AccountID = "accountID"

	// APIKeySecret not set
	config.APIKeySecret = ""
	err = config.validate()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing required cloud configuration setting")
	config.APIKeySecret = "apiKey"

	// ClusterID not set
	config.ClusterID = ""
	err = config.validate()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing required cloud configuration setting")
	config.ClusterID = "clusterID"

	// ProviderType set to "fake"
	config.ProviderType = VpcProviderTypeFake
	err = config.validate()
	assert.Nil(t, err)

	// ProviderType not set
	config.ProviderType = ""
	err = config.validate()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Invalid cloud configuration setting")
	config.ProviderType = VpcProviderTypeGen2

	// Region not set
	config.Region = ""
	err = config.validate()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing required cloud configuration setting")
	config.Region = "us-south"

	// ResourceGroupName not set
	config.ResourceGroupName = ""
	err = config.validate()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing required cloud configuration setting")
	config.ResourceGroupName = "Default"

	// SubnetNames not set
	config.SubnetNames = ""
	err = config.validate()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing required cloud configuration setting")
	config.SubnetNames = "subnet1,subnet2,subnet3"

	// WorkerAccountID not set
	config.WorkerAccountID = ""
	err = config.validate()
	assert.Nil(t, err)
	config.WorkerAccountID = "accountID"

	// VpcName not set
	config.VpcName = ""
	err = config.validate()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing required cloud configuration setting")
	config.VpcName = "subnet1,subnet2,subnet3"
}

func TestNewCloudVpc(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	vpc, err := NewCloudVpc(kubeClient, nil, nil)
	assert.Nil(t, vpc)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing cloud configuration")

	// Verify empty ConfigVpc will generate an error
	config := &ConfigVpc{}
	vpc, err = NewCloudVpc(kubeClient, config, nil)
	assert.Nil(t, vpc)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Missing required cloud configuration setting")

	// Verify "fake" CloudVpc can be created
	config = &ConfigVpc{
		ClusterID:    "clusterID",
		ProviderType: VpcProviderTypeFake,
	}
	vpc, err = NewCloudVpc(kubeClient, config, nil)
	assert.NotNil(t, vpc)
	assert.Nil(t, err)
}

func TestCloudVpc_FilterNodesByEdgeLabel(t *testing.T) {
	// Pull out the 1 edge node from the list of 2 nodes
	inNodes := []*v1.Node{mockNode1, mockNode2}
	outNodes := mockCloud.filterNodesByEdgeLabel(inNodes)
	assert.Equal(t, len(outNodes), 1)
	assert.Equal(t, outNodes[0].Name, mockNode1.Name)

	// No edge nodes in the list
	inNodes = []*v1.Node{mockNode2}
	outNodes = mockCloud.filterNodesByEdgeLabel(inNodes)
	assert.Equal(t, len(outNodes), 1)
	assert.Equal(t, outNodes[0].Name, mockNode2.Name)
}

func TestCloudVpc_FilterNodesByServiceZone(t *testing.T) {
	// No annotation on the service, match both of the nodes
	mockService := &v1.Service{}
	inNodes := []*v1.Node{mockNode1, mockNode2}
	outNodes := mockCloud.filterNodesByServiceZone(inNodes, mockService)
	assert.Equal(t, len(outNodes), 2)

	// Add the zone annotation to the service, re-calc matching nodes
	mockService.Annotations = map[string]string{serviceAnnotationZone: "zoneA"}
	outNodes = mockCloud.filterNodesByServiceZone(inNodes, mockService)
	assert.Equal(t, len(outNodes), 1)
	assert.Equal(t, outNodes[0].Name, mockNode1.Name)
}

func TestCloudVpc_filterSubnetsByName(t *testing.T) {
	// No subnets matching the requested subnet name
	inSubnets := []*VpcSubnet{{Name: "subnet1"}, {Name: "subnet2"}, {Name: "subnet3"}}
	outSubnets := mockCloud.filterSubnetsByName(inSubnets, "subnetX")
	assert.Equal(t, len(outSubnets), 0)

	// Find the two subnets that match
	outSubnets = mockCloud.filterSubnetsByName(inSubnets, "subnet1,subnet3")
	assert.Equal(t, len(outSubnets), 2)
	assert.Equal(t, outSubnets[0].Name, "subnet1")
	assert.Equal(t, outSubnets[1].Name, "subnet3")
}

func TestCloudVpc_filterSubnetsByVpcName(t *testing.T) {
	// No subnets matching the requested subnet name
	inSubnets := []*VpcSubnet{
		{Name: "subnet1", Vpc: VpcObjectReference{Name: "vpc1"}},
		{Name: "subnet2", Vpc: VpcObjectReference{Name: "vpc2"}},
		{Name: "subnet3", Vpc: VpcObjectReference{Name: "vpc3"}},
	}
	outSubnets := mockCloud.filterSubnetsByVpcName(inSubnets, "vpcX")
	assert.Equal(t, len(outSubnets), 0)

	// Find the two subnets that match
	outSubnets = mockCloud.filterSubnetsByVpcName(inSubnets, "vpc2")
	assert.Equal(t, len(outSubnets), 1)
	assert.Equal(t, outSubnets[0].Name, "subnet2")
}

func TestCloudVpc_FindNodesMatchingLabelValue(t *testing.T) {
	// Pull out the 1 edge node from the list of 2 nodes
	inNodes := []*v1.Node{mockNode1, mockNode2}
	outNodes := mockCloud.findNodesMatchingLabelValue(inNodes, nodeLabelDedicated, nodeLabelValueEdge)
	assert.Equal(t, len(outNodes), 1)
	assert.Equal(t, outNodes[0].Name, mockNode1.Name)

	// No edge nodes in the list, return matches = 0
	inNodes = []*v1.Node{mockNode2}
	outNodes = mockCloud.findNodesMatchingLabelValue(inNodes, nodeLabelDedicated, nodeLabelValueEdge)
	assert.Equal(t, len(outNodes), 0)
}

func TestCloudVpc_GetNodeIDs(t *testing.T) {
	nodes := []*v1.Node{mockNode1, mockNode2, mockNode3}
	c := CloudVpc{}
	nodeIDs := c.getNodeIDs(nodes)
	assert.Equal(t, len(nodeIDs), 2)
	assert.Equal(t, nodeIDs[0], mockNode1.Name)
	assert.Equal(t, nodeIDs[1], mockNode2.Name)
}

func TestCloudVpc_GetNodeInteralIP(t *testing.T) {
	c := CloudVpc{}
	internalIP := c.getNodeInternalIP(mockNode1)
	assert.Equal(t, "192.168.1.1", internalIP)

	internalIP = c.getNodeInternalIP(mockNode2)
	assert.Equal(t, "192.168.2.2", internalIP)

	internalIP = c.getNodeInternalIP(mockNode3)
	assert.Equal(t, "", internalIP)

	internalIP = c.getNodeInternalIP(mockNode4)
	assert.Equal(t, "", internalIP)
}

func TestCloudVpc_GetPoolMemberTargets(t *testing.T) {
	members := []*VpcLoadBalancerPoolMember{{TargetIPAddress: "192.168.1.1", TargetInstanceID: "1234-56-7890"}}
	result := mockCloud.getPoolMemberTargets(members)
	assert.Equal(t, len(result), 1)
	assert.Equal(t, result[0], "192.168.1.1")
}

func TestCloudVpc_GetServiceNodeSelectorFilter(t *testing.T) {
	// No annotation on the service. Output should be ""
	mockService := &v1.Service{}
	filterLabel, filterValue := mockCloud.getServiceNodeSelectorFilter(mockService)
	assert.Equal(t, filterLabel, "")
	assert.Equal(t, filterValue, "")

	// Invalid annotation on the service. Output should be ""
	mockService.ObjectMeta.Annotations = map[string]string{serviceAnnotationNodeSelector: "invalid"}
	filterLabel, filterValue = mockCloud.getServiceNodeSelectorFilter(mockService)
	assert.Equal(t, filterLabel, "")
	assert.Equal(t, filterValue, "")

	// Invalid key in the annotation on the service.  Output should be ""
	mockService.ObjectMeta.Annotations = map[string]string{serviceAnnotationNodeSelector: "beta.kubernetes.io/os=linux"}
	filterLabel, filterValue = mockCloud.getServiceNodeSelectorFilter(mockService)
	assert.Equal(t, filterLabel, "")
	assert.Equal(t, filterValue, "")

	// Valid key in the annotation on the service.  Output should match the annotation value
	mockService.ObjectMeta.Annotations = map[string]string{serviceAnnotationNodeSelector: "node.kubernetes.io/instance-type=cx2.2x4"}
	filterLabel, filterValue = mockCloud.getServiceNodeSelectorFilter(mockService)
	assert.Equal(t, filterLabel, "node.kubernetes.io/instance-type")
	assert.Equal(t, filterValue, "cx2.2x4")
}

func TestCloudVpc_getServicePoolNames(t *testing.T) {
	c, _ := NewCloudVpc(fake.NewSimpleClientset(), &ConfigVpc{ClusterID: "clusterID", ProviderType: VpcProviderTypeFake}, nil)
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default",
			Annotations: map[string]string{}},
		Spec: v1.ServiceSpec{Ports: []v1.ServicePort{{Protocol: v1.ProtocolTCP, Port: 80, NodePort: 30123}}},
	}
	// getPoolNamesForService success
	poolNames, err := c.getServicePoolNames(service)
	assert.Nil(t, err)
	assert.Equal(t, len(poolNames), 1)
	assert.Equal(t, poolNames[0], "tcp-80-30123")
}

func TestCloudVpc_getSubnetIDs(t *testing.T) {
	subnets := []*VpcSubnet{{ID: "subnet1"}, {ID: "subnet2"}}
	result := mockCloud.getSubnetIDs(subnets)
	assert.Equal(t, len(result), 2)
	assert.Equal(t, result[0], "subnet1")
	assert.Equal(t, result[1], "subnet2")
}

func TestCloudVpc_IsServicePublic(t *testing.T) {
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default"}}
	result := mockCloud.isServicePublic(service)
	assert.Equal(t, result, true)

	service.ObjectMeta.Annotations = map[string]string{serviceAnnotationIPType: servicePrivateLB}
	result = mockCloud.isServicePublic(service)
	assert.Equal(t, result, false)
}

func TestCloudVpc_validateService(t *testing.T) {
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default",
			Annotations: map[string]string{serviceAnnotationEnableFeatures: ""}},
		Spec: v1.ServiceSpec{Ports: []v1.ServicePort{{Protocol: v1.ProtocolTCP, Port: 80}}},
	}
	// validateService, only TCP protocol is supported
	service.Spec.Ports[0].Protocol = v1.ProtocolUDP
	options, err := mockCloud.validateService(service)
	assert.Empty(t, options)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Only TCP is supported")

	// validateService, other options passed on through
	service.ObjectMeta.Annotations[serviceAnnotationEnableFeatures] = "generic-option"
	service.Spec.Ports[0].Protocol = v1.ProtocolTCP
	options, err = mockCloud.validateService(service)
	assert.Equal(t, options, "generic-option")
	assert.Nil(t, err)
}

func TestCloudVpc_ValidateServiceSubnets(t *testing.T) {
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default"}}
	vpcSubnets := []*VpcSubnet{{ID: "subnetID", Name: "subnetName", Ipv4CidrBlock: "10.240.0.0/24", Vpc: VpcObjectReference{ID: "vpcID"}}}

	// validateServiceSubnets, success
	subnetIDs, err := mockCloud.validateServiceSubnets(service, "subnetID", "vpcID", vpcSubnets)
	assert.Equal(t, len(subnetIDs), 1)
	assert.Nil(t, err)

	// validateServiceSubnets failed, invalid subnet in the service annotation
	subnetIDs, err = mockCloud.validateServiceSubnets(service, "invalid subnet", "vpcID", vpcSubnets)
	assert.Nil(t, subnetIDs)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid VPC subnet")

	// validateServiceSubnets failed, service subnet is in a different VPC
	subnetIDs, err = mockCloud.validateServiceSubnets(service, "subnetID", "vpc2", vpcSubnets)
	assert.Nil(t, subnetIDs)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "located in a different VPC")

	// validateServiceSubnets, success, subnetID, subnetName and CIDR all passed in for the same subnet
	subnetIDs, err = mockCloud.validateServiceSubnets(service, "subnetID,subnetName,10.240.0.0/24", "vpcID", vpcSubnets)
	assert.Equal(t, len(subnetIDs), 1)
	assert.Equal(t, subnetIDs[0], "subnetID")
	assert.Nil(t, err)
}

func TestCloudVpc_ValidateServiceSubnetsNotUpdated(t *testing.T) {
	lb := &VpcLoadBalancer{Subnets: []VpcObjectReference{{ID: "subnetID"}}}
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "echo-server", Namespace: "default",
		Annotations: map[string]string{}},
	}
	vpcSubnets := []*VpcSubnet{{ID: "subnetID"}, {ID: "subnetID2"}}

	// validateServiceSubnetsNotUpdated, success - annotation not set
	err := mockCloud.validateServiceSubnetsNotUpdated(service, lb, vpcSubnets)
	assert.Nil(t, err)

	// validateServiceSubnetsNotUpdated, success - no change in annotation
	service.ObjectMeta.Annotations[serviceAnnotationSubnets] = "subnetID"
	err = mockCloud.validateServiceSubnetsNotUpdated(service, lb, vpcSubnets)
	assert.Nil(t, err)

	// validateServiceSubnetsNotUpdated, Failed, diff subnet specified
	service.ObjectMeta.Annotations[serviceAnnotationSubnets] = "subnetID2"
	err = mockCloud.validateServiceSubnetsNotUpdated(service, lb, vpcSubnets)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "setting can not be changed")
}

func TestCloudVpc_ValidateServiceTypeNotUpdated(t *testing.T) {
	lb := &VpcLoadBalancer{IsPublic: true}
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "echo-server", Namespace: "default",
		Annotations: map[string]string{}},
	}

	// validateServiceTypeNotUpdated, success - annotation not set
	err := mockCloud.validateServiceTypeNotUpdated(service, lb)
	assert.Nil(t, err)

	// validateServiceTypeNotUpdated, success - lb public, service private
	service.ObjectMeta.Annotations[serviceAnnotationIPType] = servicePrivateLB
	err = mockCloud.validateServiceTypeNotUpdated(service, lb)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "setting can not be changed")

	// validateServiceTypeNotUpdated, success - lb private, service public
	lb.IsPublic = false
	service.ObjectMeta.Annotations[serviceAnnotationIPType] = servicePublicLB
	err = mockCloud.validateServiceTypeNotUpdated(service, lb)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "setting can not be changed")
	lb.IsPublic = true
}

func TestCloudVpc_ValidateServiceZone(t *testing.T) {
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default"}}
	vpcSubnets := []*VpcSubnet{{ID: "subnetID", Zone: "zoneA"}}

	// validateServiceZone, success
	subnetIDs, err := mockCloud.validateServiceZone(service, "zoneA", vpcSubnets)
	assert.Equal(t, len(subnetIDs), 1)
	assert.Nil(t, err)

	// validateServiceZone failed, no cluster subnets in that zone
	subnetIDs, err = mockCloud.validateServiceZone(service, "zoneX", vpcSubnets)
	assert.Nil(t, subnetIDs)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no cluster subnets in that zone")
}
