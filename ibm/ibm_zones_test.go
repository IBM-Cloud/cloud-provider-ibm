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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	cloudprovider "k8s.io/cloud-provider"
)

func getZonesInterfaceWithProvider(provider *Provider) cloudprovider.Zones {
	var config *CloudConfig
	if nil != provider {
		config = &CloudConfig{Prov: *provider}
	} else {
		config = &CloudConfig{}
	}
	c := &Cloud{Config: config}
	i, _ := c.Zones()
	return i
}

func getZonesInterfaceWithCCMProvider(provider *Provider, metadataSvc *MetadataService) cloudprovider.Zones {
	var config *CloudConfig
	if nil != provider {
		config = &CloudConfig{Prov: *provider}
	} else {
		config = &CloudConfig{}
	}
	c := &Cloud{Config: config, Metadata: metadataSvc}
	i, _ := c.Zones()
	return i
}

func getZonesInterface() cloudprovider.Zones {
	return getZonesInterfaceWithProvider(nil)
}

func TestZones(t *testing.T) {
	c := &Cloud{}
	cloud, ok := c.Zones()
	if !ok {
		t.Fatalf("Zones implementation missing")
	}
	if c != cloud {
		t.Fatalf("Cloud not returned")
	}
}

func TestGetZone(t *testing.T) {
	z := getZonesInterface()
	zone, err := z.GetZone(context.Background())
	if nil != err {
		t.Fatalf("GetZone failed: %s", err)
	}
	if zone.Region != "" {
		t.Fatalf("Zone Region is not empty")
	}
	if zone.FailureDomain != "" {
		t.Fatalf("Zone FailureDomain is not empty")
	}

	expectedRegion := "testregion"
	expectedFailureDomain := "testdomain"

	z = getZonesInterfaceWithProvider(&Provider{Region: expectedRegion, Zone: expectedFailureDomain})
	zone, err = z.GetZone(context.Background())
	if nil != err {
		t.Fatalf("GetZone failed: %s", err)
	}
	if expectedRegion != zone.Region {
		t.Fatalf("Unexpected Region: %v", zone.Region)
	}
	if expectedFailureDomain != zone.FailureDomain {
		t.Fatalf("Unexpected FailureDomain: %v", zone.FailureDomain)
	}
}

func TestGetZoneByProviderID(t *testing.T) {
	c := &Cloud{}
	_, err := c.GetZoneByProviderID(context.Background(), "ibm")
	if nil == err {
		t.Fatalf("GetZoneByProviderID did not return an error")
	}
}

func TestGetZoneByNodeName(t *testing.T) {
	c := &Cloud{}
	zone, err := c.GetZoneByNodeName(context.Background(), types.NodeName("192.168.10.5"))
	if nil != err {
		t.Fatalf("GetZoneByNodeName failed: %s", err)
	}
	if zone.Region != "" {
		t.Fatalf("Zone Region is not empty")
	}
	if zone.FailureDomain != "" {
		t.Fatalf("Zone FailureDomain is not empty")
	}
}

func TestGetZoneByNodeNameCCM(t *testing.T) {
	expectedAccountID := "testaccount"
	expectedClusterID := "testcluster"
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
	k8snode := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "testnode",
			Labels: labels},
	}

	z := getZonesInterfaceWithCCMProvider(&Provider{AccountID: expectedAccountID, ClusterID: expectedClusterID}, metadataSvc)

	// testing getting undefined node
	zone, err := z.GetZoneByNodeName(context.Background(), types.NodeName("testnode"))
	if nil == err {
		t.Fatalf("GetZoneByNodeName did not return error getting undefined node")
	}

	// testing getting valid node
	_, err = fakeclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node testnode: %v", err)
	}
	zone, err = z.GetZoneByNodeName(context.Background(), types.NodeName("testnode"))
	if nil != err {
		t.Fatalf("GetZoneByNodeName failed: %s", err)
	}
	if expectedMetadata.Region != zone.Region {
		t.Fatalf("Zone Region had unexpected value: %v", zone.Region)
	}
	if expectedMetadata.FailureDomain != zone.FailureDomain {
		t.Fatalf("Zone FailureDomain had unexpected value: %v", zone.FailureDomain)
	}
}
