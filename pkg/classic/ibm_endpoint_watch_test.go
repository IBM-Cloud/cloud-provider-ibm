/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2018, 2023 All Rights Reserved.
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

package classic

import (
	"context"
	"errors"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

func strPtr(value string) *string {
	return &value
}

func resetDeleteServiceLBPod() {
	deletePodViaName = deletePod
}

func resetGetPodsListViaLabel() {
	listPodsViaLabel = listPods
}

func resetGetServiceViaEndpoint() {
	getServiceViaEndpoint = getService
}

func assertPodWasNotDeleted(t *testing.T, deletePodCalled bool, expectedDeletedPodName string, deletedPodName string, lbDeploymentNamespace string, deletedPodNamspace string) {
	if deletePodCalled {
		t.Fatalf("Expected delete pod not to be called")
	}
	if expectedDeletedPodName == deletedPodName {
		t.Fatalf("Expected no pod to be delete: %s, Actual: %s", expectedDeletedPodName, deletedPodName)
	}
	if lbDeploymentNamespace == deletedPodNamspace {
		t.Fatalf("Incorrect Deleted pod Namespace Expected: %s, Actual: %s", lbDeploymentNamespace, deletedPodNamspace)
	}
}

func assertPodWasDeleted(t *testing.T, deletePodCalled bool, expectedDeletedPodName string, deletedPodName string, lbDeploymentNamespace string, deletedPodNamspace string) {
	if !deletePodCalled {
		t.Fatalf("Expected delete pod to be called")
	}
	if expectedDeletedPodName != deletedPodName {
		t.Fatalf("Incorrect Deleted pod Name Expected: %s, Actual: %s", expectedDeletedPodName, deletedPodName)
	}
	if lbDeploymentNamespace != deletedPodNamspace {
		t.Fatalf("Incorrect Deleted pod Namespace Expected: %s, Actual: %s", lbDeploymentNamespace, deletedPodNamspace)
	}
}

func getMockPodList(podName string, podHostIP string, podPhase v1.PodPhase) *v1.PodList {
	return &v1.PodList{
		Items: []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: podName,
				},
				Status: v1.PodStatus{
					Phase:  podPhase,
					HostIP: podHostIP,
				},
			},
		},
	}
}

func getMockEnpoints(serviceName string) *v1.Endpoints {
	return &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: testNamespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						NodeName: strPtr("192.168.10.11"),
						IP:       "192.168.10.11",
					},
				},
			},
		},
	}
}

func getBadMockEnpoints(serviceName string) *v1.Endpoints {
	return &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: lbDeploymentNamespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						NodeName: nil,
						IP:       "192.168.10.11",
					},
				},
			},
		},
	}
}

func TestEnsureLoadBalancerSourceIpEvictionLocalPolicy(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	expectedDeletedPodHostIP := "192.168.10.12"
	expectedDeletedPodName := "Some Random Pod Name"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := getMockEnpoints(lbServiceName)

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionLocalPolicyIPVS(t *testing.T) {
	lbServiceName := "testIPVSDeleteCMCreate"
	c, _, _ := getTestCloud()

	expectedDeletedPodHostIP := "192.168.10.12"
	expectedDeletedPodName := "Some Random Pod Name"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := getMockEnpoints(lbServiceName)

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionDeletePodViaNameError(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	expectedDeletedPodHostIP := "192.168.10.12"
	expectedDeletedPodName := "Some Random Pod Name"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return errors.New("ERROR: Testing when error is returned")
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := getMockEnpoints(lbServiceName)

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionListPodsViaLabelError(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	expectedDeletedPodHostIP := "192.168.10.12"
	expectedDeletedPodName := "Some Random Pod Name"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), errors.New("ERROR: Testing when error is returned")
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := getMockEnpoints(lbServiceName)

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionGetServiceViaEndpointError(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	expectedDeletedPodHostIP := "192.168.10.12"
	expectedDeletedPodName := "Some Random Pod Name"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	getServiceViaEndpoint = func(_ string, _ string, _ clientset.Interface) (*v1.Service, error) {
		return nil, errors.New("ERROR: Testing when error is returned")
	}
	defer resetGetServiceViaEndpoint()

	var oldObj struct{}

	newObj := getMockEnpoints(lbServiceName)

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionLocalPolicyNoSubsets(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	expectedDeletedPodName := "Some Random Pod Name"
	expectedDeletedPodHostIP := "192.168.10.12"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbServiceName,
			Namespace: lbDeploymentNamespace,
		},
		Subsets: []v1.EndpointSubset{},
	}

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionLocalPolicyNoAddresses(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	expectedDeletedPodName := "Some Random Pod Name"
	expectedDeletedPodHostIP := "192.168.10.12"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbServiceName,
			Namespace: testNamespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{},
			},
		},
	}

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionLocalPolicyCorrectNode(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	expectedDeletedPodName := "Some Random Pod Name"
	// We hard code this namespace that we are delete pods in since that is the only namespace
	// the keepalived pods are deployed
	expectedDeletedPodHostIP := "192.168.10.11"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := getMockEnpoints(lbServiceName)

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionLocalPolicyPending(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	expectedDeletedPodName := "Some Random Pod Name"
	// Pending pods will not have a host IP
	expectedDeletedPodHostIP := ""
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodPending), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := getMockEnpoints(lbServiceName)

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionClusterPolicy(t *testing.T) {
	lbServiceName := "test"
	c, _, _ := getTestCloud()

	expectedDeletedPodName := "Some Random Pod Name"
	expectedDeletedPodHostIP := "192.168.10.12"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := getMockEnpoints(lbServiceName)

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestEnsureLoadBalancerSourceIpEvictionNoEndpoints(t *testing.T) {
	c, _, _ := getTestCloud()

	expectedDeletedPodName := "Some Random Pod Name"
	expectedDeletedPodHostIP := "192.168.10.12"
	deletePodCalled := false
	deletedPodName := ""
	deletedPodNamspace := ""

	listPodsViaLabel = func(string, string, clientset.Interface) (*v1.PodList, error) {
		return getMockPodList(expectedDeletedPodName, expectedDeletedPodHostIP, v1.PodRunning), nil
	}
	defer resetGetPodsListViaLabel()

	deletePodViaName = func(namespace string, podName string, _ clientset.Interface) error {
		deletePodCalled = true
		deletedPodName = podName
		deletedPodNamspace = namespace
		return nil
	}
	defer resetDeleteServiceLBPod()

	var oldObj struct{}

	newObj := &v1.Endpoints{}

	c.handleEndpointUpdate(oldObj, newObj)

	assertPodWasNotDeleted(t, deletePodCalled, expectedDeletedPodName, deletedPodName, lbDeploymentNamespace, deletedPodNamspace)
}

func TestLoadBalancerSourceIpEvictionExitHandler(t *testing.T) {
	// Test exit handler for handleEndpointUpdate to catch a nil pointer exception,
	// we then verify that the exit handler closes the channel to allow for the
	// Endpoint watch service to be restarted
	c, _, _ := getTestCloud()

	c.handleEndpointUpdate(nil, nil)
}

func TestLoadBalancerSourceIpEvictionExitHandlerWithService(t *testing.T) {
	c, _, _ := getTestCloud()

	defer c.handleEndpointWatchCrash()

	panic(errors.New("TESTING ENDPOINT WATCH CRASH HANDLER. EXPECTING A STACK TRACE AND THIS TEST TO PASS"))
}

func TestLoadBalancerSourceIpEvictionGetPodsListViaLabel(t *testing.T) {
	lbServiceName := "testSourceIP"
	c, _, _ := getTestCloud()

	newObj := getBadMockEnpoints(lbServiceName)

	_, err := c.KubeClient.CoreV1().Services(newObj.Namespace).Get(context.TODO(), newObj.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("ERROR: Failed to get service: %v", err)
		return
	}

	podList, errGetPodList := listPodsViaLabel("RandomNamespace", "RandomLabel", c.KubeClient)
	if errGetPodList != nil {
		t.Fatalf("ERROR: Failed to get pod list via a Label: %v", errGetPodList)
	}
	if len(podList.Items) != 0 {
		t.Fatalf("ERROR: Expected Pod List Slice to be 0: %v", podList)
	}
}

func TestLoadBalancerSourceIpEvictionDeleteServiceLBPod(t *testing.T) {

	c, _, _ := getTestCloud()

	deletePodViaName("RandomNamespace", "RandomLabel", c.KubeClient)
}
