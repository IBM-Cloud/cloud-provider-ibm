/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2022 All Rights Reserved.
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
	"reflect"
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	cloudprovider "k8s.io/cloud-provider"
)

func getInstancesInterfaceWithProvider(provider *Provider) cloudprovider.Instances {
	var config *CloudConfig
	if nil != provider {
		config = &CloudConfig{Prov: *provider}
	} else {
		config = &CloudConfig{}
	}
	c := &Cloud{Config: config}
	i, _ := c.Instances()
	return i
}

func getInstancesInterfaceWithCCMProvider(provider *Provider, metadataSvc *MetadataService) cloudprovider.Instances {
	var config *CloudConfig
	if nil != provider {
		config = &CloudConfig{Prov: *provider}
	} else {
		config = &CloudConfig{}
	}
	c := &Cloud{Config: config, Metadata: metadataSvc}
	i, _ := c.Instances()
	return i
}

func getInstancesInterface() cloudprovider.Instances {
	return getInstancesInterfaceWithProvider(nil)
}

func TestInstances(t *testing.T) {
	c := &Cloud{}
	cloud, ok := c.Instances()
	if ok {
		t.Fatalf("Instances implementation should be disabled")
	}
	if c != cloud {
		t.Fatalf("Cloud not returned")
	}
}

func TestNodeAddresses(t *testing.T) {
	internalIP := "10.190.31.186"
	externalIP := "169.61.102.244"
	anotherIP := "10.190.31.187"
	nodeName := types.NodeName(internalIP)
	anotherNodeName := types.NodeName(anotherIP)
	defaultNodeAddresses := []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: internalIP},
		{Type: v1.NodeExternalIP, Address: internalIP},
	}
	anotherNodeAddresses := []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: anotherIP},
		{Type: v1.NodeExternalIP, Address: anotherIP},
	}
	providerNodeAddresses := []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: internalIP},
		{Type: v1.NodeExternalIP, Address: externalIP},
	}

	// Default node addresses expected with no IPs from provider.
	i := getInstancesInterface()
	nodeAddresses, err := i.NodeAddresses(context.Background(), nodeName)
	if nil != err {
		t.Fatalf("Failed to get node addresses: %v", err)
	}
	if !reflect.DeepEqual(defaultNodeAddresses, nodeAddresses) {
		t.Fatalf("Unexpected default node addresses: %v", nodeAddresses)
	}

	// Default node addresses expected with ExternalIP from provider.
	i = getInstancesInterfaceWithProvider(&Provider{ExternalIP: externalIP})
	nodeAddresses, err = i.NodeAddresses(context.Background(), nodeName)
	if nil != err {
		t.Fatalf("Failed to get node addresses: %v", err)
	}
	if !reflect.DeepEqual(defaultNodeAddresses, nodeAddresses) {
		t.Fatalf("Unexpected default node addresses: %v", nodeAddresses)
	}

	// Default node addresses expected with InternalIP from provider.
	i = getInstancesInterfaceWithProvider(&Provider{InternalIP: internalIP})
	nodeAddresses, err = i.NodeAddresses(context.Background(), nodeName)
	if nil != err {
		t.Fatalf("Failed to get node addresses: %v", err)
	}
	if !reflect.DeepEqual(defaultNodeAddresses, nodeAddresses) {
		t.Fatalf("Unexpected default node addresses: %v", nodeAddresses)
	}

	i = getInstancesInterfaceWithProvider(&Provider{InternalIP: internalIP, ExternalIP: externalIP})

	// Another node address expected on IP mis-match from provider.
	nodeAddresses, err = i.NodeAddresses(context.Background(), anotherNodeName)
	if nil != err {
		t.Fatalf("Failed to get node addresses: %v", err)
	}
	if !reflect.DeepEqual(anotherNodeAddresses, nodeAddresses) {
		t.Fatalf("Unexpected another node addresses: %v", nodeAddresses)
	}

	// Provider node addresses expected on IP match from provider.
	nodeAddresses, err = i.NodeAddresses(context.Background(), nodeName)
	if nil != err {
		t.Fatalf("Failed to get node addresses: %v", err)
	}
	if !reflect.DeepEqual(providerNodeAddresses, nodeAddresses) {
		t.Fatalf("Unexpected provider node addresses: %v", nodeAddresses)
	}
}

func TestNodeAddressesCCM(t *testing.T) {
	expectedAccountID := "testaccount"
	expectedClusterID := "testcluster"
	fakeclient := k8sfake.NewSimpleClientset()
	metadataSvc := NewMetadataService(nil, fakeclient)
	var metadata NodeMetadata
	var expectedNodeAddresses []v1.NodeAddress
	var labels map[string]string

	i := getInstancesInterfaceWithCCMProvider(&Provider{AccountID: expectedAccountID, ClusterID: expectedClusterID}, metadataSvc)

	// test getting undefined node
	_, err := i.NodeAddresses(context.Background(), "testnode")
	if nil == err {
		t.Fatalf("NodeAddresses did not return error getting undefined node")
	}

	// testing getting valid node
	metadata = NodeMetadata{
		InternalIP:    "10.190.31.186",
		ExternalIP:    "169.61.102.244",
		WorkerID:      "testworkerid",
		InstanceType:  "testmachinetype",
		FailureDomain: "testfailuredomain",
		Region:        "testregion",
	}
	expectedNodeAddresses = []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: metadata.InternalIP},
		{Type: v1.NodeExternalIP, Address: metadata.ExternalIP},
	}
	labels = map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  metadata.InternalIP,
		"ibm-cloud.kubernetes.io/external-ip":  metadata.ExternalIP,
		"ibm-cloud.kubernetes.io/zone":         metadata.FailureDomain,
		"ibm-cloud.kubernetes.io/region":       metadata.Region,
		"ibm-cloud.kubernetes.io/worker-id":    metadata.WorkerID,
		"ibm-cloud.kubernetes.io/machine-type": metadata.InstanceType,
	}
	k8snode1 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "testnode1",
			Labels: labels},
	}
	_, err = fakeclient.CoreV1().Nodes().Create(context.TODO(), &k8snode1, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node testnode1: %v", err)
	}
	nodeAddresses, err := i.NodeAddresses(context.Background(), "testnode1")
	if nil != err {
		t.Fatalf("Failed to get node addresses")
	}
	if !reflect.DeepEqual(expectedNodeAddresses, nodeAddresses) {
		t.Fatalf("Unexpected provider node addresses: %v", nodeAddresses)
	}

	// testing getting valid node with only internal IP
	metadata = NodeMetadata{
		InternalIP:    "10.190.31.186",
		WorkerID:      "testworkerid",
		InstanceType:  "testmachinetype",
		FailureDomain: "testfailuredomain",
		Region:        "testregion",
	}
	expectedNodeAddresses = []v1.NodeAddress{
		{Type: v1.NodeInternalIP, Address: metadata.InternalIP},
		{Type: v1.NodeExternalIP, Address: metadata.InternalIP},
	}
	labels = map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  metadata.InternalIP,
		"ibm-cloud.kubernetes.io/zone":         metadata.FailureDomain,
		"ibm-cloud.kubernetes.io/region":       metadata.Region,
		"ibm-cloud.kubernetes.io/worker-id":    metadata.WorkerID,
		"ibm-cloud.kubernetes.io/machine-type": metadata.InstanceType,
	}
	k8snode2 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "testnode2",
			Labels: labels},
	}
	_, err = fakeclient.CoreV1().Nodes().Create(context.TODO(), &k8snode2, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node testnode2: %v", err)
	}
	nodeAddresses, err = i.NodeAddresses(context.Background(), "testnode2")
	if nil != err {
		t.Fatalf("Failed to get node addresses")
	}
	if !reflect.DeepEqual(expectedNodeAddresses, nodeAddresses) {
		t.Fatalf("Unexpected provider node addresses: %v", nodeAddresses)
	}

	// testing getting node with neither internal or external IP set
	metadata = NodeMetadata{
		WorkerID:      "testworkerid",
		InstanceType:  "testmachinetype",
		FailureDomain: "testfailuredomain",
		Region:        "testregion",
	}
	expectedNodeAddresses = []v1.NodeAddress{}
	labels = map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  metadata.InternalIP,
		"ibm-cloud.kubernetes.io/zone":         metadata.FailureDomain,
		"ibm-cloud.kubernetes.io/region":       metadata.Region,
		"ibm-cloud.kubernetes.io/worker-id":    metadata.WorkerID,
		"ibm-cloud.kubernetes.io/machine-type": metadata.InstanceType,
	}
	k8snode3 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "testnode3",
			Labels: labels},
	}
	_, err = fakeclient.CoreV1().Nodes().Create(context.TODO(), &k8snode3, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node testnode2: %v", err)
	}
	nodeAddresses, err = i.NodeAddresses(context.Background(), "testnode3")
	if nil != err {
		t.Fatalf("Failed to get node addresses")
	}
	if !reflect.DeepEqual(expectedNodeAddresses, nodeAddresses) {
		t.Fatalf("Unexpected provider node addresses: %v", nodeAddresses)
	}
}

func TestNodeAddressesByProviderID(t *testing.T) {
	i := getInstancesInterface()
	_, err := i.NodeAddressesByProviderID(context.Background(), "rtheis")
	if nil == err {
		t.Fatalf("Unexpected node addresses by provider ID support for instances")
	}
}

func TestInstanceID(t *testing.T) {
	i := getInstancesInterfaceWithProvider(&Provider{ProviderID: "testaccount/testorg/testspace/testclusterID/testworkerID"})
	id, err := i.InstanceID(context.Background(), types.NodeName("192.168.10.2"))
	if nil != err {
		t.Fatalf("Failed to get instance ID: %v", err)
	}
	if strings.Compare(id, "testaccount/testorg/testspace/testclusterID/testworkerID") != 0 {
		t.Fatalf("Unexpected instance ID: %v", id)
	}
}

func TestInstanceIDCCM(t *testing.T) {
	expectedAccountID := "testaccount"
	expectedClusterID := "testcluster"
	expectedInstanceID := "testaccount///testcluster/testworkerid"
	fakeclient := k8sfake.NewSimpleClientset()
	metadataSvc := NewMetadataService(nil, fakeclient)

	// Define fake node
	expectedMetadata := NodeMetadata{
		InternalIP:    "10.190.31.186",
		ExternalIP:    "169.61.102.244",
		WorkerID:      "testworkerid",
		InstanceType:  "testmachinetype",
		FailureDomain: "testfailuredomain",
		Region:        "testregion",
	}
	labels := map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  expectedMetadata.InternalIP,
		"ibm-cloud.kubernetes.io/external-ip":  expectedMetadata.ExternalIP,
		"ibm-cloud.kubernetes.io/zone":         expectedMetadata.FailureDomain,
		"ibm-cloud.kubernetes.io/region":       expectedMetadata.Region,
		"ibm-cloud.kubernetes.io/worker-id":    expectedMetadata.WorkerID,
		"ibm-cloud.kubernetes.io/machine-type": expectedMetadata.InstanceType,
	}
	k8snode := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "testnode",
			Labels: labels},
	}

	i := getInstancesInterfaceWithCCMProvider(&Provider{AccountID: expectedAccountID, ClusterID: expectedClusterID}, metadataSvc)

	// test getting undefined node
	_, err := i.InstanceID(context.Background(), "testnode")
	if nil == err {
		t.Fatalf("InstanceID did not return error getting undefined node")
	}

	// testing getting valid node
	_, err = fakeclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node testnode: %v", err)
	}
	instanceID, err := i.InstanceID(context.Background(), "testnode")
	if nil != err {
		t.Fatalf("Failed to get InstanceID")
	}
	if instanceID != expectedInstanceID {
		t.Fatalf("Unexpected provider instanceID: %v", instanceID)
	}
}

func TestInstanceIDEmptyProvider(t *testing.T) {
	i := getInstancesInterfaceWithProvider(&Provider{ProviderID: "////"})
	id, err := i.InstanceID(context.Background(), types.NodeName("192.168.10.2"))
	if nil != err {
		t.Fatalf("Failed to get instance ID: %v", err)
	}
	if strings.Compare(id, "192.168.10.2") != 0 {
		t.Fatalf("Unexpected instance ID: %v", id)
	}
}

func TestInstanceType(t *testing.T) {
	i := getInstancesInterface()
	iType, err := i.InstanceType(context.Background(), types.NodeName("192.168.10.2"))
	if nil != err {
		t.Fatalf("Failed to get instance type: %v", err)
	}
	if len(iType) != 0 {
		t.Fatalf("Unexpected instance type: %v", iType)
	}
	expectedInstanceType := "testInstanceType"
	i = getInstancesInterfaceWithProvider(&Provider{InstanceType: expectedInstanceType})
	iType, err = i.InstanceType(context.Background(), types.NodeName("192.168.10.2"))
	if nil != err {
		t.Fatalf("Failed to get instance type: %v", err)
	}
	if expectedInstanceType != iType {
		t.Fatalf("Unexpected instance type: %v", iType)
	}
}

func TestInstanceTypeCCM(t *testing.T) {
	expectedAccountID := "testaccount"
	expectedClusterID := "testcluster"
	expectedInstanceType := "testmachinetype"
	fakeclient := k8sfake.NewSimpleClientset()
	metadataSvc := NewMetadataService(nil, fakeclient)

	// Define fake node
	expectedMetadata := NodeMetadata{
		InternalIP:    "10.190.31.186",
		ExternalIP:    "169.61.102.244",
		WorkerID:      "testworkerid",
		InstanceType:  expectedInstanceType,
		FailureDomain: "testfailuredomain",
		Region:        "testregion",
	}
	labels := map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  expectedMetadata.InternalIP,
		"ibm-cloud.kubernetes.io/external-ip":  expectedMetadata.ExternalIP,
		"ibm-cloud.kubernetes.io/zone":         expectedMetadata.FailureDomain,
		"ibm-cloud.kubernetes.io/region":       expectedMetadata.Region,
		"ibm-cloud.kubernetes.io/worker-id":    expectedMetadata.WorkerID,
		"ibm-cloud.kubernetes.io/machine-type": expectedMetadata.InstanceType,
	}
	k8snode := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "testnode",
			Labels: labels},
	}

	i := getInstancesInterfaceWithCCMProvider(&Provider{AccountID: expectedAccountID, ClusterID: expectedClusterID}, metadataSvc)

	// test getting undefined node
	_, err := i.InstanceType(context.Background(), "testnode")
	if nil == err {
		t.Fatalf("InstanceType did not return error getting undefined node")
	}

	// testing getting valid node
	_, err = fakeclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node testnode: %v", err)
	}
	instanceType, err := i.InstanceType(context.Background(), "testnode")
	if nil != err {
		t.Fatalf("Failed to get InstanceType")
	}
	if instanceType != expectedInstanceType {
		t.Fatalf("Unexpected provider instanceType: %v", instanceType)
	}
}

func TestInstanceTypeByProviderID(t *testing.T) {
	i := getInstancesInterface()
	_, err := i.InstanceTypeByProviderID(context.Background(), "rtheis")
	if nil == err {
		t.Fatalf("Unexpected instance type by provider ID support for instances")
	}
}

func TestAddSSHKeyToAllInstances(t *testing.T) {
	i := getInstancesInterface()
	err := i.AddSSHKeyToAllInstances(context.Background(), "rtheis", []byte{})
	if nil == err {
		t.Fatalf("Unexpected SSH support for instances")
	}
}

func TestCurrentNodeName(t *testing.T) {
	i := getInstancesInterface()
	nodeName, err := i.CurrentNodeName(context.Background(), "192.168.10.2")
	if nil != err {
		t.Fatalf("Failed to get current node name: %v", err)
	}
	if strings.Compare("192.168.10.2", string(nodeName)) != 0 {
		t.Fatalf("Unexpected current node name: %v", nodeName)
	}
}

func TestInstanceExistsByProviderID(t *testing.T) {
	i := getInstancesInterface()
	exists, err := i.InstanceExistsByProviderID(context.Background(), "ibm")
	if !exists || nil != err {
		t.Fatalf("Unexpected instance not exists by provider ID")
	}
}

func TestInstanceShutdownByProviderID(t *testing.T) {
	i := getInstancesInterface()
	exists, err := i.InstanceShutdownByProviderID(context.Background(), "ibm")
	if exists || nil == err {
		t.Fatalf("Unexpected instance shutdown by provider ID support for instances")
	}
}

func getInstancesV2InterfaceWithProvider(provider *Provider) cloudprovider.InstancesV2 {
	var config *CloudConfig
	if nil != provider {
		config = &CloudConfig{Prov: *provider}
	} else {
		config = &CloudConfig{}
	}
	c := &Cloud{Config: config}
	i, _ := c.InstancesV2()
	return i
}

func getInstancesV2InterfaceWithCCMProvider(provider *Provider, metadataSvc *MetadataService) cloudprovider.InstancesV2 {
	var config *CloudConfig
	if nil != provider {
		config = &CloudConfig{Prov: *provider}
	} else {
		config = &CloudConfig{}
	}
	c := &Cloud{Config: config, Metadata: metadataSvc}
	i, _ := c.InstancesV2()
	return i
}

func getInstancesV2Interface() cloudprovider.InstancesV2 {
	return getInstancesV2InterfaceWithProvider(nil)
}

func TestInstancesV2(t *testing.T) {
	c := &Cloud{}
	cloud, ok := c.InstancesV2()
	if !ok {
		t.Fatalf("InstancesV2 implementation missing")
	}
	if c != cloud {
		t.Fatalf("Cloud not returned")
	}
}

func TestInstanceExists(t *testing.T) {
	i := getInstancesV2Interface()

	exists, err := i.InstanceExists(context.Background(), &v1.Node{Spec: v1.NodeSpec{ProviderID: "ibm"}})
	if err != nil {
		t.Fatalf("InstanceExists should return no error")
	}
	if !exists {
		t.Fatal("Node with provider should exist")
	}
}

func TestInstanceShutdown(t *testing.T) {
	i := getInstancesV2Interface()

	shutdown, err := i.InstanceShutdown(context.Background(), &v1.Node{})
	if err != nil {
		t.Fatalf("InstanceShutdown should not return an error")
	}
	if shutdown {
		t.Fatal("InstanceShutdown should always return false")
	}
}

func TestInstanceMetadata(t *testing.T) {
	expectedAccountID := "testaccount"
	expectedClusterID := "testcluster"
	expectedInstanceType := "testmachinetype"
	fakeclient := k8sfake.NewSimpleClientset()
	metadataSvc := NewMetadataService(nil, fakeclient)

	expectedMetadata := cloudprovider.InstanceMetadata{
		ProviderID:   "ibm://testaccount///testcluster/testworkerid",
		InstanceType: expectedInstanceType,
		NodeAddresses: []v1.NodeAddress{
			{Type: v1.NodeInternalIP, Address: "10.190.31.186"},
			{Type: v1.NodeExternalIP, Address: "169.61.102.244"},
		},
		Zone:   "testfailuredomain",
		Region: "testregion",
	}

	labels := map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  "10.190.31.186",
		"ibm-cloud.kubernetes.io/external-ip":  "169.61.102.244",
		"ibm-cloud.kubernetes.io/zone":         "testfailuredomain",
		"ibm-cloud.kubernetes.io/region":       "testregion",
		"ibm-cloud.kubernetes.io/worker-id":    "testworkerid",
		"ibm-cloud.kubernetes.io/machine-type": expectedInstanceType,
	}
	k8snode := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "testnode",
			Labels: labels},
	}

	i := getInstancesV2InterfaceWithCCMProvider(&Provider{AccountID: expectedAccountID, ClusterID: expectedClusterID}, metadataSvc)

	// test getting undefined node
	_, err := i.InstanceMetadata(context.Background(), &k8snode)
	if nil == err {
		t.Fatalf("InstanceID did not return error getting undefined node")
	}

	// testing getting valid node
	_, err = fakeclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node testnode: %v", err)
	}
	metadata, err := i.InstanceMetadata(context.Background(), &k8snode)
	if nil != err {
		t.Fatalf("Failed to get InstanceID")
	}
	if metadata.ProviderID != expectedMetadata.ProviderID {
		t.Fatalf("ProviderID set to incorrect value of %s", metadata.ProviderID)
	}
	if metadata.InstanceType != expectedMetadata.InstanceType {
		t.Fatalf("InstanceType set to incorrect value of %s", metadata.InstanceType)
	}
	for i, nodeAddress := range metadata.NodeAddresses {
		if nodeAddress != expectedMetadata.NodeAddresses[i] {
			t.Fatalf("NodeAddress set to incorrect value of %s", nodeAddress)
		}
	}
	if metadata.Zone != expectedMetadata.Zone {
		t.Fatalf("Zone set to incorrect value of %s", metadata.Zone)
	}
	if metadata.Region != expectedMetadata.Region {
		t.Fatalf("Region set to incorrect value of %s", metadata.Region)
	}

	labels["ibm-cloud.kubernetes.io/internal-ip"] = ""
	labels["ibm-cloud.kubernetes.io/external-ip"] = ""
	k8snode2 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "testnode2",
			Labels: labels},
	}

	// testing getting valid node
	_, err = fakeclient.CoreV1().Nodes().Create(context.TODO(), &k8snode2, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node testnode: %v", err)
	}
	metadata, err = i.InstanceMetadata(context.Background(), &k8snode2)
	if nil != err {
		t.Fatalf("Failed to get InstanceID")
	}
	if len(metadata.NodeAddresses) != 0 {
		t.Fatalf("NodeAddress set to incorrect value of %x", metadata.NodeAddresses)
	}
}
