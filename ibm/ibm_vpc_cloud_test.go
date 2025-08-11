/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2021, 2025 All Rights Reserved.
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
	"os"
	"testing"

	"cloud.ibm.com/cloud-provider-ibm/pkg/vpcctl"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	cluster     = "clusterID"
	clusterName = "clusterName"
)

func TestShouldPrivateEndpointBeEnabled(t *testing.T) {
	result := shouldPrivateEndpointBeEnabled()
	assert.True(t, result)
	os.Setenv(envVarPublicEndPoint, "true")
	defer os.Unsetenv(envVarPublicEndPoint)
	result = shouldPrivateEndpointBeEnabled()
	assert.False(t, result)
}

func TestCloud_InitCloudVpc(t *testing.T) {
	c := Cloud{Config: &CloudConfig{Prov: Provider{ClusterID: cluster}}, KubeClient: fake.NewSimpleClientset()}
	v, err := c.InitCloudVpc(shouldPrivateEndpointBeEnabled())
	assert.Nil(t, v)
	assert.NotNil(t, err)
}

func TestCloud_isProviderVpc(t *testing.T) {
	c := Cloud{Config: &CloudConfig{Prov: Provider{ClusterID: cluster}}}
	result := c.isProviderVpc()
	assert.False(t, result)
	c.Config.Prov.ProviderType = vpcctl.VpcProviderTypeGen2
	result = c.isProviderVpc()
	assert.True(t, result)
}

func TestCloud_NewConfigVpc(t *testing.T) {
	// Test for the case of cloud config not initialized
	c := Cloud{}
	config, err := c.NewConfigVpc(true)
	assert.Nil(t, config)
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "cloud config not initialized")

	// Test failure to read credentials from file
	c.Config = &CloudConfig{Prov: Provider{
		Region:                   "us-south",
		AccountID:                "accountID",
		ClusterID:                "clusterID",
		IamEndpointOverride:      "iam-override",
		ProviderType:             "g2",
		RmEndpointOverride:       "rm-override",
		G2Credentials:            "../../test-fixtures/missing-file.txt",
		G2ResourceGroupName:      "Default",
		G2VpcSubnetNames:         "subnet1,subnet2,subnet3",
		G2WorkerServiceAccountID: "accountID",
		G2VpcName:                "vpc",
		G2EndpointOverride:       "vpc-override",
	}}
	config, err = c.NewConfigVpc(true)
	assert.Nil(t, config)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "failed to read credentials")

	// Successfully return ConfigVpc
	c.Config.Prov.G2Credentials = ""
	config, err = c.NewConfigVpc(true)
	assert.NotNil(t, config)
	assert.Nil(t, err)
	assert.Equal(t, config.AccountID, "accountID")
	assert.Equal(t, config.APIKeySecret, "")
	assert.Equal(t, config.ClusterID, "clusterID")
	assert.Equal(t, config.EnablePrivate, true)
	assert.Equal(t, config.IamEndpointOverride, "iam-override")
	assert.Equal(t, config.ProviderType, "g2")
	assert.Equal(t, config.Region, "us-south")
	assert.Equal(t, config.ResourceGroupName, "Default")
	assert.Equal(t, config.RmEndpointOverride, "rm-override")
	assert.Equal(t, config.SubnetNames, "subnet1,subnet2,subnet3")
	assert.Equal(t, config.WorkerAccountID, "accountID")
	assert.Equal(t, config.VpcName, "vpc")
	assert.Equal(t, config.VpcEndpointOverride, "vpc-override")
}

func TestCloud_VpcEnsureLoadBalancer(t *testing.T) {
	cloud := Cloud{
		Config:     &CloudConfig{Prov: Provider{ClusterID: "clusterID", ProviderType: vpcctl.VpcProviderTypeGen2}},
		KubeClient: fake.NewSimpleClientset(),
		Recorder:   NewCloudEventRecorderV1("ibm", fake.NewSimpleClientset().CoreV1().Events("")),
	}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "192.168.0.1", Labels: map[string]string{}}}

	// VpcEnsureLoadBalancer failed, no available nodes, sync chanel has been closed
	close(CreateUpdateChan)
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotFound"}}
	status, err := cloud.VpcEnsureLoadBalancer(context.Background(), clusterName, service, []*v1.Node{})
	assert.Nil(t, status)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "There are no available nodes for LoadBalancer")

	// VpcEnsureLoadBalancer failed, no available nodes
	CreateUpdateChan = nil
	status, err = cloud.VpcEnsureLoadBalancer(context.Background(), clusterName, service, []*v1.Node{})
	assert.Nil(t, status)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "There are no available nodes for LoadBalancer")

	// VpcEnsureLoadBalancer failed, failed to initialize VPC env
	vpcctl.ResetCloudVpc()
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	status, err = cloud.VpcEnsureLoadBalancer(context.Background(), clusterName, service, []*v1.Node{node})
	assert.Nil(t, status)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "failed initializing VPC")
}

func TestCloud_VpcEnsureLoadBalancerDeleted(t *testing.T) {
	cloud := Cloud{
		Config:     &CloudConfig{Prov: Provider{ClusterID: "clusterID", ProviderType: vpcctl.VpcProviderTypeGen2}},
		KubeClient: fake.NewSimpleClientset(),
		Recorder:   NewCloudEventRecorderV1("ibm", fake.NewSimpleClientset().CoreV1().Events("")),
	}
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}

	// VpcEnsureLoadBalancerDeleted failed, failed to initialize VPC env
	vpcctl.ResetCloudVpc()
	err := cloud.VpcEnsureLoadBalancerDeleted(context.Background(), clusterName, service)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "failed initializing VPC")

	// VpcEnsureLoadBalancerDeleted successful, existing LB was deleted
	cloud.Config.Prov.ProviderType = vpcctl.VpcProviderTypeFake
	err = cloud.VpcEnsureLoadBalancerDeleted(context.Background(), clusterName, service)
	assert.Nil(t, err)

	// VpcEnsureLoadBalancerDeleted failed, failed to delete the LB
	c := cloud.GetCloudVpc()
	c.SetFakeSdkError("DeleteLoadBalancer")
	err = cloud.VpcEnsureLoadBalancerDeleted(context.Background(), clusterName, service)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed deleting LoadBalancer")
	c.ClearFakeSdkError("DeleteLoadBalancer")
}

func TestCloud_VpcGetLoadBalancer(t *testing.T) {
	cloud := Cloud{
		Config:     &CloudConfig{Prov: Provider{ClusterID: "clusterID", ProviderType: vpcctl.VpcProviderTypeGen2}},
		KubeClient: fake.NewSimpleClientset(),
		Recorder:   NewCloudEventRecorderV1("ibm", fake.NewSimpleClientset().CoreV1().Events("")),
	}
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}

	// VpcGetLoadBalancer failed, failed to initialize VPC env
	vpcctl.ResetCloudVpc()
	status, exist, err := cloud.VpcGetLoadBalancer(context.Background(), clusterName, service)
	assert.Nil(t, status)
	assert.False(t, exist)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "failed initializing VPC")

	// VpcGetLoadBalancer successful, LB is ready
	cloud.Config.Prov.ProviderType = vpcctl.VpcProviderTypeFake
	status, exist, err = cloud.VpcGetLoadBalancer(context.Background(), clusterName, service)
	assert.NotNil(t, status)
	assert.Equal(t, status.Ingress[0].Hostname, "lb.ibm.com")
	assert.True(t, exist)
	assert.Nil(t, err)

	// VpcGetLoadBalancer failed, failed to get find the LB
	c := cloud.GetCloudVpc()
	c.SetFakeSdkError("FindLoadBalancer")
	c.SetFakeSdkError("ListLoadBalancers")
	status, exist, err = cloud.VpcGetLoadBalancer(context.Background(), clusterName, service)
	assert.Nil(t, status)
	assert.False(t, exist)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed getting LoadBalancer")
	c.ClearFakeSdkError("FindLoadBalancer")
	c.ClearFakeSdkError("ListLoadBalancers")
}

func TestCloud_VpcGetLoadBalancerName(t *testing.T) {
	clusterID := "12345678901234567890"
	c := Cloud{Config: &CloudConfig{Prov: Provider{ClusterID: clusterID}}}
	kubeService := &v1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "echo-server", Namespace: "default", UID: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"}}
	lbName := "kube-" + clusterID + "-" + string(kubeService.UID)
	lbName = lbName[:63]
	result := c.vpcGetLoadBalancerName(kubeService)
	assert.Equal(t, result, lbName)
}

func TestCloud_VpcMonitorLoadBalancers(t *testing.T) {
	cloud := Cloud{
		Config:     &CloudConfig{Prov: Provider{ClusterID: "clusterID", ProviderType: vpcctl.VpcProviderTypeGen2}},
		KubeClient: fake.NewSimpleClientset(),
		Recorder:   NewCloudEventRecorderV1("ibm", fake.NewSimpleClientset().CoreV1().Events("")),
	}
	serviceNodePort := v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "nodePort", Namespace: "default", UID: "NodePort"},
		Spec: v1.ServiceSpec{Type: v1.ServiceTypeNodePort}}
	serviceList := &v1.ServiceList{Items: []v1.Service{serviceNodePort}}
	dataMap := map[string]string{}

	// VpcUpdateLoadBalancer failed, service list was not passed in
	cloud.VpcMonitorLoadBalancers(nil, dataMap)

	// VpcUpdateLoadBalancer failed, service list was not passed in
	cloud.VpcMonitorLoadBalancers(&v1.ServiceList{Items: []v1.Service{}}, dataMap)

	// VpcUpdateLoadBalancer failed, failed to initialize VPC env
	vpcctl.ResetCloudVpc()
	cloud.VpcMonitorLoadBalancers(serviceList, dataMap)

	// VpcUpdateLoadBalancer failed, initialize VPC successfully
	cloud.Config.Prov.ProviderType = vpcctl.VpcProviderTypeFake
	cloud.VpcMonitorLoadBalancers(serviceList, dataMap)
}

func TestCloud_VpcUpdateLoadBalancer(t *testing.T) {
	cloud := Cloud{
		Config:     &CloudConfig{Prov: Provider{ClusterID: "clusterID", ProviderType: vpcctl.VpcProviderTypeGen2}},
		KubeClient: fake.NewSimpleClientset(),
		Recorder:   NewCloudEventRecorderV1("ibm", fake.NewSimpleClientset().CoreV1().Events("")),
	}
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "192.168.0.1", Labels: map[string]string{}}}
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}

	// VpcUpdateLoadBalancer failed, node list is empty
	err := cloud.VpcUpdateLoadBalancer(context.Background(), clusterName, service, []*v1.Node{})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no available nodes for LoadBalancer")

	// VpcUpdateLoadBalancer failed, failed to initialize VPC env
	vpcctl.ResetCloudVpc()
	err = cloud.VpcUpdateLoadBalancer(context.Background(), clusterName, service, []*v1.Node{node})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "failed initializing VPC")
}

func TestCloud_WatchCloudCredential(t *testing.T) {
	cloud := Cloud{
		Config: &CloudConfig{Prov: Provider{ClusterID: "clusterID", ProviderType: vpcctl.VpcProviderTypeGen2}},
	}

	// WatchCloudCredential failed, no cloud credential file
	err := cloud.WatchCloudCredential()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no cloud credential file to watch")

	// WatchCloudCredential failed, cloud credential file does not exist
	cloud.Config.Prov.G2Credentials = "/tmp/file_does_not_exist"
	err = cloud.WatchCloudCredential()
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "no such file")
}
