/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2021, 2024 All Rights Reserved.
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

func TestCloudVpc_EnsureLoadBalancer(t *testing.T) {
	c, _ := NewCloudVpc(fake.NewSimpleClientset(), &ConfigVpc{ClusterID: "clusterID", ProviderType: VpcProviderTypeFake}, nil)
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "192.168.0.1", Labels: map[string]string{}}}

	// EnsureLoadBalancer failed, required argument is missing
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	status, err := c.EnsureLoadBalancer("", service, []*v1.Node{node})
	assert.Nil(t, status)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Required argument is missing")

	// EnsureLoadBalancer failed, failed to get find the LB
	c.SetFakeSdkError("FindLoadBalancer")
	c.SetFakeSdkError("ListLoadBalancers")
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	status, err = c.EnsureLoadBalancer("kube-clusterID-Ready", service, []*v1.Node{node})
	assert.Nil(t, status)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed getting LoadBalancer")
	c.ClearFakeSdkError("FindLoadBalancer")
	c.ClearFakeSdkError("ListLoadBalancers")

	// EnsureLoadBalancer failed, failed to get create LB, no available nodes
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotFound"}}
	status, err = c.EnsureLoadBalancer("kube-clusterID-NotFound", service, []*v1.Node{})
	assert.Nil(t, status)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed ensuring LoadBalancer")

	// EnsureLoadBalancer failed, existing LB is busy
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotReady"}}
	status, err = c.EnsureLoadBalancer("kube-clusterID-NotReady", service, []*v1.Node{})
	assert.Nil(t, status)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "LoadBalancer is busy")

	// EnsureLoadBalancer failed, failed to update LB, no available nodes
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	status, err = c.EnsureLoadBalancer("kube-clusterID-Ready", service, []*v1.Node{})
	assert.Nil(t, status)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed ensuring LoadBalancer")

	// EnsureLoadBalancer successful, existing LB was updated
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	status, err = c.EnsureLoadBalancer("kube-clusterID-Ready", service, []*v1.Node{node})
	assert.NotNil(t, status)
	assert.Nil(t, err)
	assert.Equal(t, status.Ingress[0].Hostname, "lb.ibm.com")
}

func TestCloudVpc_EnsureLoadBalancerDeleted(t *testing.T) {
	c, _ := NewCloudVpc(fake.NewSimpleClientset(), &ConfigVpc{ClusterID: "clusterID", ProviderType: VpcProviderTypeFake}, nil)

	// EnsureLoadBalancerDeleted failed, failed to get find the LB
	c.SetFakeSdkError("FindLoadBalancer")
	c.SetFakeSdkError("ListLoadBalancers")
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	err := c.EnsureLoadBalancerDeleted("kube-clusterID-Ready", service)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed getting LoadBalancer")
	c.ClearFakeSdkError("FindLoadBalancer")
	c.ClearFakeSdkError("ListLoadBalancers")

	// EnsureLoadBalancerDeleted success, existing LB does not exist
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotFound"}}
	err = c.EnsureLoadBalancerDeleted("kube-clusterID-NotFound", service)
	assert.Nil(t, err)

	// EnsureLoadBalancerDeleted failed, failed to delete the LB
	c.SetFakeSdkError("DeleteLoadBalancer")
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	err = c.EnsureLoadBalancerDeleted("kube-clusterID-Ready", service)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed deleting LoadBalancer")
	c.ClearFakeSdkError("DeleteLoadBalancer")

	// EnsureLoadBalancerDeleted successful, existing LB was deleted
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	err = c.EnsureLoadBalancerDeleted("kube-clusterID-Ready", service)
	assert.Nil(t, err)
}

func TestCloud_EnsureLoadBalancerUpdated(t *testing.T) {
	c, _ := NewCloudVpc(fake.NewSimpleClientset(), &ConfigVpc{ClusterID: "clusterID", ProviderType: VpcProviderTypeFake}, nil)
	node := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "192.168.0.1", Labels: map[string]string{}}}

	// EnsureLoadBalancerUpdated failed, failed to get find the LB
	c.SetFakeSdkError("FindLoadBalancer")
	c.SetFakeSdkError("ListLoadBalancers")
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	err := c.EnsureLoadBalancerUpdated("kube-clusterID-Ready", service, []*v1.Node{node})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed getting LoadBalancer")
	c.ClearFakeSdkError("FindLoadBalancer")
	c.ClearFakeSdkError("ListLoadBalancers")

	// EnsureLoadBalancerUpdated failed, existing LB does not exist
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotFound"}}
	err = c.EnsureLoadBalancerUpdated("kube-clusterID-NotFound", service, []*v1.Node{node})
	assert.Nil(t, err)

	// EnsureLoadBalancerUpdated failed, existing LB is busy
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotReady"}}
	err = c.EnsureLoadBalancerUpdated("kube-clusterID-NotReady", service, []*v1.Node{node})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "LoadBalancer is busy")

	// EnsureLoadBalancerUpdated failed, failed to update LB, node list is empty
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	err = c.EnsureLoadBalancerUpdated("kube-clusterID-Ready", service, []*v1.Node{})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed updating LoadBalancer")

	// EnsureLoadBalancerUpdated successful, existing LB was updated
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	err = c.EnsureLoadBalancerUpdated("kube-clusterID-Ready", service, []*v1.Node{node})
	assert.Nil(t, err)
}

func TestCloudVpc_GatherLoadBalancers(t *testing.T) {
	c, _ := NewCloudVpc(fake.NewSimpleClientset(), &ConfigVpc{ClusterID: "clusterID", ProviderType: VpcProviderTypeFake}, nil)
	serviceNodePort := v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "nodePort", Namespace: "default", UID: "NodePort"},
		Spec:       v1.ServiceSpec{Type: v1.ServiceTypeNodePort}}
	serviceNotFound := v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "notFound", Namespace: "default", UID: "NotFound"},
		Spec:       v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer}}
	serviceNotReady := v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "notReady", Namespace: "default", UID: "NotReady"},
		Spec:       v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer}}
	serviceList := &v1.ServiceList{Items: []v1.Service{serviceNodePort, serviceNotFound, serviceNotReady}}

	// GatherLoadBalancers failed, Kube services not specified
	lbMap, vpcMap, err := c.GatherLoadBalancers(nil)
	assert.Nil(t, lbMap)
	assert.Nil(t, vpcMap)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Required argument is missing")

	// GatherLoadBalancers failed, SDK List LB failed
	c.SetFakeSdkError("ListLoadBalancers")
	lbMap, vpcMap, err = c.GatherLoadBalancers(serviceList)
	assert.Nil(t, lbMap)
	assert.Nil(t, vpcMap)
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "ListLoadBalancers failed")
	c.ClearFakeSdkError("ListLoadBalancers")

	// GatherLoadBalancers success
	lbMap, vpcMap, err = c.GatherLoadBalancers(serviceList)
	assert.NotNil(t, lbMap)
	assert.NotNil(t, vpcMap)
	assert.Nil(t, err)
	assert.Equal(t, len(lbMap), 2)
	assert.Equal(t, len(vpcMap), 2)
}

func TestCloudVpc_GenerateLoadBalancerName(t *testing.T) {
	clusterID := "12345678901234567890"
	c, _ := NewCloudVpc(fake.NewSimpleClientset(), &ConfigVpc{ClusterID: clusterID, ProviderType: VpcProviderTypeFake}, nil)
	kubeService := &v1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "echo-server", Namespace: "default", UID: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"}}
	lbName := "kube-" + clusterID + "-" + string(kubeService.UID)
	lbName = lbName[:63]
	result := c.GenerateLoadBalancerName(kubeService)
	assert.Equal(t, result, lbName)

	// Verify annotation works to specify VPC LB name
	kubeService.Annotations = map[string]string{serviceAnnotationLbName: "my-lb-name"}
	result = c.GenerateLoadBalancerName(kubeService)
	assert.Equal(t, result, "my-lb-name")
}

func TestCloudVpc_GetLoadBalancer(t *testing.T) {
	c, _ := NewCloudVpc(fake.NewSimpleClientset(), &ConfigVpc{ClusterID: "clusterID", ProviderType: VpcProviderTypeFake}, nil)

	// GetLoadBalancer failed, failed to get find the LB
	c.SetFakeSdkError("FindLoadBalancer")
	c.SetFakeSdkError("ListLoadBalancers")
	service := &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	status, exist, err := c.GetLoadBalancer("kube-clusterID-Ready", service)
	assert.Nil(t, status)
	assert.False(t, exist)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Failed getting LoadBalancer")
	c.ClearFakeSdkError("FindLoadBalancer")
	c.ClearFakeSdkError("ListLoadBalancers")

	// GetLoadBalancer success, existing LB does not found
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotFound"}}
	status, exist, err = c.GetLoadBalancer("kube-clusterID-NotFound", service)
	assert.Nil(t, status)
	assert.False(t, exist)
	assert.Nil(t, err)

	// GetLoadBalancer successful, LB is not ready, service does not have a hostname
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotReady"}}
	status, exist, err = c.GetLoadBalancer("kube-clusterID-NotReady", service)
	assert.NotNil(t, status)
	assert.Equal(t, len(status.Ingress), 0)
	assert.True(t, exist)
	assert.Nil(t, err)

	// GetLoadBalancer successful, LB is not ready, return the host name associated with the VPC LB
	service = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "NotReady"},
		Status:     v1.ServiceStatus{LoadBalancer: v1.LoadBalancerStatus{Ingress: []v1.LoadBalancerIngress{{Hostname: "service.lb.ibm.com"}}}},
	}
	status, exist, err = c.GetLoadBalancer("kube-clusterID-NotReady", service)
	assert.NotNil(t, status)
	assert.Equal(t, status.Ingress[0].Hostname, "notready.lb.ibm.com")
	assert.True(t, exist)
	assert.Nil(t, err)

	// GetLoadBalancer successful, LB is ready
	service = &v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "echo-server", Namespace: "default", UID: "Ready"}}
	status, exist, err = c.GetLoadBalancer("kube-clusterID-Ready", service)
	assert.NotNil(t, status)
	assert.Equal(t, status.Ingress[0].Hostname, "lb.ibm.com")
	assert.True(t, exist)
	assert.Nil(t, err)
}

func TestCloudVpc_MonitorLoadBalancers(t *testing.T) {
	c, _ := NewCloudVpc(fake.NewSimpleClientset(), &ConfigVpc{ClusterID: "clusterID", ProviderType: VpcProviderTypeFake}, nil)
	serviceNodePort := v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "nodePort", Namespace: "default", UID: "NodePort"},
		Spec: v1.ServiceSpec{Type: v1.ServiceTypeNodePort}}
	serviceNotFound := v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "notFound", Namespace: "default", UID: "NotFound"},
		Spec: v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer}}
	serviceNotReady := v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "notReady", Namespace: "default", UID: "NotReady"},
		Spec: v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer}}
	serviceReady := v1.Service{ObjectMeta: metav1.ObjectMeta{Name: "Ready", Namespace: "default", UID: "Ready"},
		Spec: v1.ServiceSpec{Type: v1.ServiceTypeLoadBalancer}}
	dataMap := map[string]string{}

	// MonitorLoadBalancers failed, service list was not passed in
	c.MonitorLoadBalancers(nil, dataMap)

	// MonitorLoadBalancers success, no existing status. Verify current status is returned
	serviceList := &v1.ServiceList{Items: []v1.Service{serviceNodePort, serviceNotFound, serviceNotReady}}
	c.MonitorLoadBalancers(serviceList, dataMap)
	assert.Equal(t, len(dataMap), 2)
	assert.Equal(t, dataMap["NotFound"], vpcLbStatusOfflineNotFound)
	assert.Equal(t, dataMap["NotReady"], vpcLbStatusOfflineCreatePending)

	// MonitorLoadBalancers success, data updated based on current state of LB
	serviceList = &v1.ServiceList{Items: []v1.Service{serviceReady}}
	dataMap = map[string]string{"Ready": vpcLbStatusOfflineCreatePending}
	c.MonitorLoadBalancers(serviceList, dataMap)
	assert.Equal(t, len(dataMap), 1)
	assert.Equal(t, dataMap["Ready"], vpcLbStatusOnlineActive)

	// MonitorLoadBalancers success, no change is status
	serviceList = &v1.ServiceList{Items: []v1.Service{serviceNotReady, serviceNotFound}}
	dataMap = map[string]string{"NotReady": vpcLbStatusOfflineCreatePending, "NotFound": vpcLbStatusOfflineNotFound}
	c.MonitorLoadBalancers(serviceList, dataMap)
	assert.Equal(t, len(dataMap), 2)
	assert.Equal(t, dataMap["NotReady"], vpcLbStatusOfflineCreatePending)
	assert.Equal(t, dataMap["NotFound"], vpcLbStatusOfflineNotFound)
}
