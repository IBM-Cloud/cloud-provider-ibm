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
	"fmt"
	"log"
	"reflect"
	"testing"

	"github.com/IBM-Cloud/power-go-client/power/models"
	rc "github.com/IBM/platform-services-go-sdk/resourcecontrollerv2"
	"github.com/stretchr/testify/assert"

	"k8s.io/utils/ptr"
)

func TestGetPowerVSNetwork(t *testing.T) {
	instanceName := "test_vm"
	testCases := []struct {
		testcase      string
		pvmInstance   *models.PVMInstance
		expectedError error
	}{
		{
			testcase:      "with no network attached to instance",
			pvmInstance:   &models.PVMInstance{ServerName: ptr.To(instanceName)},
			expectedError: fmt.Errorf("expecting only one network to be attached to vm test_vm but got 0"),
		},
		{
			testcase: "with more than 1 network attached to instance",
			pvmInstance: &models.PVMInstance{
				ServerName: ptr.To(instanceName),
				Networks: []*models.PVMInstanceNetwork{
					{
						NetworkName: "test_network",
					},
					{
						NetworkName: "test_network_2",
					},
				},
			},
			expectedError: fmt.Errorf("expecting only one network to be attached to vm %s but got 2", instanceName),
		},
		{
			testcase: "with one network attached to instance",
			pvmInstance: &models.PVMInstance{
				ServerName: ptr.To(instanceName),
				Networks: []*models.PVMInstanceNetwork{
					{
						Type:        "dynamic",
						NetworkName: "test_network",
						MacAddress:  "ff:11:33:dd:00:22",
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.testcase, func(t *testing.T) {
			network, err := getPowerVSNetwork(tc.pvmInstance)
			if tc.expectedError != nil {
				if err == nil {
					t.Fatal("Expecting error but got nil")
				}
				if !reflect.DeepEqual(tc.expectedError.Error(), err.Error()) {
					t.Errorf("expected %v, got: %v", tc.expectedError.Error(), err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error %v", err)
				}
				assert.Equal(t, "test_network", network.NetworkName, "Unexpected NetworkName")
				assert.Equal(t, "dynamic", network.Type, "Unexpected Network Type")
				assert.Equal(t, "ff:11:33:dd:00:22", network.MacAddress, "Unexpected MacAddress")
			}
		})
	}
}

func TestPopulatePowerVSNodeMetadata(t *testing.T) {
	powerVSClient := ibmPowerVSClient{
		provider:        Provider{PowerVSRegion: "lon", PowerVSZone: "lon4"},
		sdk:             &powerVSTestClient{},
		cloudInstanceID: "testCloudInstanceId",
	}
	newNode := NodeMetadata{}
	err := powerVSClient.populateNodeMetadata("testNode", &newNode)
	if err != nil {
		log.Fatal(err)
	}
	assert.Equal(t, "192.168.1.1", newNode.InternalIP, "Unexpected InternalIP")
	assert.Equal(t, "740aabcb-5096-4024-4324-12327e8d0def", newNode.WorkerID, "Unexpected WorkerID")
	assert.Equal(t, "s922", newNode.InstanceType, "Unexpected InstanceType")
	assert.Equal(t, "lon4", newNode.FailureDomain, "Unexpected FailureDomain")
	assert.Equal(t, "lon", newNode.Region, "Unexpected Region")
}

type powerVSTestClient struct {
}

func (p *powerVSTestClient) GetInstanceByName(name string) (*models.PVMInstance, error) {
	pvmInstanceID := "740aabcb-5096-4024-4324-12327e8d0def"
	return &models.PVMInstance{
		PvmInstanceID: &pvmInstanceID,
		SysType:       "s922",
		Networks: []*models.PVMInstanceNetwork{
			{IPAddress: "192.168.1.1"},
		},
	}, nil

}

func (p *powerVSTestClient) GetInstances() (*models.PVMInstances, error) {
	pvmInstanceID := "740aabcb-5096-4024-4324-12327e8d0def"
	serverName := "TestMachine"
	return &models.PVMInstances{PvmInstances: []*models.PVMInstanceReference{
		{
			Networks: []*models.PVMInstanceNetwork{
				{IPAddress: "192.168.1.1"},
			},
			PvmInstanceID: &pvmInstanceID,
			ServerName:    &serverName,
			SysType:       "s922",
		},
	}}, nil
}

func (p *powerVSTestClient) GetDHCPServers() (models.DHCPServers, error) {
	return nil, nil
}

func (p *powerVSTestClient) GetDHCPServerByID(string) (*models.DHCPServerDetail, error) {
	return nil, nil
}

func (p *powerVSTestClient) GetCloudServiceInstanceByName(string) (*rc.ResourceInstance, error) {
	return nil, nil
}
