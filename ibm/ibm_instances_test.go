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
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	cloudprovider "k8s.io/cloud-provider"
)

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
