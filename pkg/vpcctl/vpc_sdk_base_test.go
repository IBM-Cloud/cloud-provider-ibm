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

	"github.com/go-openapi/strfmt"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

func TestExtractPortsFromPoolName(t *testing.T) {
	// Invalid pool name
	protocol, port, nodePort, err := extractProtocolPortsFromPoolName("poolName")
	assert.Equal(t, protocol, "")
	assert.Equal(t, port, -1)
	assert.Equal(t, nodePort, -1)
	assert.NotNil(t, err)

	// Invalid protocol
	protocol, port, nodePort, err = extractProtocolPortsFromPoolName("sctp-80-31234")
	assert.Equal(t, protocol, "")
	assert.Equal(t, port, -1)
	assert.Equal(t, nodePort, -1)
	assert.NotNil(t, err)

	// Invalid port
	protocol, port, nodePort, err = extractProtocolPortsFromPoolName("tcp-abc-31234")
	assert.Equal(t, protocol, "")
	assert.Equal(t, port, -1)
	assert.Equal(t, nodePort, -1)
	assert.NotNil(t, err)

	// Invalid nodePort
	protocol, port, nodePort, err = extractProtocolPortsFromPoolName("tcp-80-xyz")
	assert.Equal(t, protocol, "")
	assert.Equal(t, port, -1)
	assert.Equal(t, nodePort, -1)
	assert.NotNil(t, err)

	// Success
	protocol, port, nodePort, err = extractProtocolPortsFromPoolName("tcp-80-31234")
	assert.Equal(t, protocol, "tcp")
	assert.Equal(t, port, 80)
	assert.Equal(t, nodePort, 31234)
	assert.Nil(t, err)
}

func TestGenLoadBalancerPoolName(t *testing.T) {
	kubePort := v1.ServicePort{Protocol: "TCP", Port: 80, NodePort: 31234}
	result := genLoadBalancerPoolName(kubePort)
	assert.Equal(t, result, "tcp-80-31234")
}

func TestIsVpcOptionEnabled(t *testing.T) {
	result := isVpcOptionEnabled("", "item")
	assert.False(t, result)
	result = isVpcOptionEnabled("itemA,itemB,itemC", "item")
	assert.False(t, result)
	result = isVpcOptionEnabled("itemA,itemB,itemC", "itemC")
	assert.True(t, result)
}

func TestSafePointerBool(t *testing.T) {
	var ptr *bool
	result := SafePointerBool(ptr)
	assert.Equal(t, result, false)

	val := true
	ptr = &val
	result = SafePointerBool(ptr)
	assert.Equal(t, result, true)
}

func TestSafePointerDate(t *testing.T) {
	var ptr *strfmt.DateTime
	result := SafePointerDate(ptr)
	assert.Equal(t, result, "nil")

	val := strfmt.NewDateTime()
	ptr = &val
	result = SafePointerDate(ptr)
	assert.Equal(t, result, "1970-01-01T00:00:00.000Z")
}

func TestSafePointerInt64(t *testing.T) {
	var ptr *int64
	result := SafePointerInt64(ptr)
	assert.Equal(t, result, int64(0))

	val := int64(1234)
	ptr = &val
	result = SafePointerInt64(ptr)
	assert.Equal(t, result, int64(1234))
}

func TestSafePointerString(t *testing.T) {
	var ptr *string
	result := SafePointerString(ptr)
	assert.Equal(t, result, "nil")

	val := "apple"
	ptr = &val
	result = SafePointerString(ptr)
	assert.Equal(t, result, "apple")
}

func TestVpcLoadBalancer_GetStatus(t *testing.T) {
	lb := &VpcLoadBalancer{
		ProvisioningStatus: LoadBalancerProvisioningStatusActive,
		OperatingStatus:    LoadBalancerOperatingStatusOnline,
	}
	result := lb.GetStatus()
	assert.Equal(t, result, "online/active")
}

func TestVpcLoadBalancer_getSubnetIDs(t *testing.T) {
	lb := &VpcLoadBalancer{
		Subnets: []VpcObjectReference{{ID: "subnet-1"}, {ID: "subnet-2"}},
	}
	result := lb.getSubnetIDs()
	assert.Equal(t, len(result), 2)
	assert.Equal(t, result[0], "subnet-1")
	assert.Equal(t, result[1], "subnet-2")
}

func TestVpcLoadBalancer_GetSummary(t *testing.T) {
	lb := &VpcLoadBalancer{
		Name:               "LoadBalancer",
		ID:                 "1234",
		Hostname:           "lb.ibm.com",
		Pools:              []VpcObjectReference{{Name: "tcp-80-30303"}},
		PrivateIps:         []string{"10.0.0.1", "10.0.0.2"},
		PublicIps:          []string{"192.168.0.1", "192.168.0.2"},
		ProvisioningStatus: LoadBalancerProvisioningStatusActive,
		OperatingStatus:    LoadBalancerOperatingStatusOnline,
	}
	result := lb.GetSummary()
	assert.Equal(t, result, "Name:LoadBalancer ID:1234 Status:online/active Hostname:lb.ibm.com Pools:tcp-80-30303 Private:10.0.0.1,10.0.0.2 Public:192.168.0.1,192.168.0.2")
}

func TestVpcLoadBalancer_getVpcID(t *testing.T) {
	lb := &VpcLoadBalancer{
		Subnets: []VpcObjectReference{{ID: "subnetID"}},
	}
	vpcSubnets := []*VpcSubnet{{ID: "subnetID", Vpc: VpcObjectReference{ID: "vpcID"}}}
	// Found VPC ID
	result := lb.getVpcID(vpcSubnets)
	assert.Equal(t, result, "vpcID")

	// No matching subnet ID, vpcID is returned as""
	lb.Subnets[0].ID = "invalid-subnetID"
	result = lb.getVpcID(vpcSubnets)
	assert.Equal(t, result, "")
}

func TestVpcLoadBalancer_getZones(t *testing.T) {
	lb := &VpcLoadBalancer{
		Subnets: []VpcObjectReference{{ID: "subnetA"}, {ID: "subnetB"}, {ID: "subnetC"}},
	}
	vpcSubnets := []*VpcSubnet{
		{ID: "subnetA", Zone: "us-south-1"},
		{ID: "subnetB", Zone: "us-south-2"},
		{ID: "subnetC", Zone: "us-south-1"},
	}
	// Retrieve zones for the LB
	result := lb.getZones(vpcSubnets)
	assert.Equal(t, len(result), 2)
	assert.Equal(t, result[0], "us-south-1")
	assert.Equal(t, result[1], "us-south-2")
}

func TestVpcLoadBalancer_IsReady(t *testing.T) {
	// Status is "online/active"
	lb := &VpcLoadBalancer{
		ProvisioningStatus: LoadBalancerProvisioningStatusActive,
		OperatingStatus:    LoadBalancerOperatingStatusOnline,
	}
	ready := lb.IsReady()
	assert.Equal(t, ready, true)

	// Status is "offline/create_pending"
	lb = &VpcLoadBalancer{
		ProvisioningStatus: LoadBalancerProvisioningStatusCreatePending,
		OperatingStatus:    LoadBalancerOperatingStatusOffline,
	}
	ready = lb.IsReady()
	assert.Equal(t, ready, false)
}
