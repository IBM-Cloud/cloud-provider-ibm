/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2019, 2021 All Rights Reserved.
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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

/*
	VPC Load Balancer Testing

	Unit tests verifying expected functionality of ibm_vpc_loadbalancer.go
*/

func TestGetVPCLoadBalancerName(t *testing.T) {
	service := getLoadBalancerService("c90bcf60-5d0e-4fc4-9a72-086e2cb15000")
	cloud, _, _ := getTestCloud()
	cloud.Config.Prov.ClusterID = "bkielqu20bvnkn9nr400" // Required as it's included in the name

	expectedLBName := "kube-bkielqu20bvnkn9nr400-c90bcf605d0e4fc49a72086e2cb15000"
	lbName := cloud.getVpcLoadBalancerName(service)
	if lbName != expectedLBName {
		t.Errorf("\nUnexpected VPC Load Balancer Name: %s, expected: %s", lbName, expectedLBName)
	}
}

/*
	NOTE: getVPCLoadbalancerName() does not work if the ClusterID is not atleast 4 characters in length, and there is currently no check for that.
*/

func TestGetVPCLoadBalancerStatus(t *testing.T) {
	service := getLoadBalancerService("c90bcf60-5d0e-4fc4-9a72-086e2cb15000")
	lbStatus := getVpcLoadBalancerStatus(service, "Test-Hostname")
	if lbStatus.Ingress[0].Hostname != "Test-Hostname" {
		t.Errorf("\nUnexpected VPC Load Balancer Status/HostName: %v", lbStatus)
	}

	service = getLoadBalancerService("c90bcf60-5d0e-4fc4-9a72-086e2cb15000b")
	lbStatus = getVpcLoadBalancerStatus(service, "10.10.10.0,10.10.10.1,10.10.10.2")
	if lbStatus.Ingress[0].IP != "10.10.10.0" || lbStatus.Ingress[1].IP != "10.10.10.1" || lbStatus.Ingress[2].IP != "10.10.10.2" {
		t.Errorf("\nUnexpected VPC Service Load Balancer Status/IP: %v", lbStatus)
	}
}

// The following tests the getVPCLoadBalancer function of the ibm_vpc_loadbalancer.go
// file.  The function being tested returns whether the specified load balancer exists, and
// if so, what its status is.
func TestGetVPCLoadBalancer(t *testing.T) {
	clusterName := "Test-Cluster-Name"
	ctx := context.Background()
	cloud, _, _ := getTestCloud()
	cloud.Config.Prov.ClusterID = "bkielqu20bvnkn9nr400"

	// Patch and defer restore of execVpcCommand()
	oldExecVpc := execVpcCommand
	spoofVpcBinary()
	defer func() { execVpcCommand = oldExecVpc }()

	{
		// TEST CASE #1: Test that the correct status, exists, err is returned when the LoadBalancer does not exist
		t.Log("TEST CASE: Load balancer does not exist")
		status, exists, err := cloud.getVpcLoadBalancer(ctx, clusterName, getLoadBalancerService("loadBalancerDoesNOTexist"))
		if nil != status || exists || nil != err {
			t.Fatalf("Unexpectedly found a load balancer which does not exist: %v, %v, %v", status, exists, err)
		} else {
			t.Log("SUCCESS: Failed to find non-existent loadbalancer as expected")
		}
	}

	{
		// TEST CASE #2: Single LoadBalancer exists
		t.Log("TEST CASE: Single load balancer exists")
		status, exists, err := cloud.getVpcLoadBalancer(ctx, clusterName, getLoadBalancerService("loadBalancerExists"))
		if nil == status || !exists || nil != err {
			t.Fatalf("Unexpected error finding existing load balancer: %v, %v, %v", status, exists, err)
		} else {
			t.Log("SUCCESS: Load balancer found successfully!")
		}
	}

	{
		// TEST CASE #3: LoadBalancer is in pending state
		t.Log("TEST CASE: Load balancer is pending")
		status, exists, err := cloud.getVpcLoadBalancer(ctx, clusterName, getLoadBalancerService("loadBalancerPending"))
		if nil == status || !exists || nil != err {
			t.Fatalf("Unexpected error finding pending load balancer: %v, %v, %v", status, exists, err)
		} else {
			t.Log("SUCCESS: Pending load balancer found successfully!")
		}
	}

	{
		// TEST CASE #4: LoadBalancer has no name
		t.Log("TEST CASE: Anonymous load balancer (no name)")
		status, exists, err := cloud.getVpcLoadBalancer(ctx, clusterName, getLoadBalancerService(""))
		if nil != status || exists || nil == err {
			t.Fatalf("Unexpected error finding nameless load balancer '': %v, %v, %v", status, exists, err)
		} else {
			t.Log("SUCCESS: Received an error/no status for anonymous load balancer as expected")
		}
	}

	{
		// TEST CASE #5: Binary returns a mixture of good and bad output
		t.Log("TEST CASE: Bogus binary output")
		status, exists, err := cloud.getVpcLoadBalancer(ctx, clusterName, getLoadBalancerService("infoBogusSuccess"))
		if nil == status || !exists || nil != err {
			t.Fatalf("Unexpected error in processing output from binary: %v, %v, %v", status, exists, err)
		} else {
			t.Log("SUCCESS: Correctly handles bogus information")
		}
	}
}

// The following tests the ensureVPCLoadBalancer function of the ibm_vpc_loadbalancer.go
// file.  The funcion is really an alias for lb-create, so the load balancer will be crated when
// this fuction is called. The tester has several variations based on the logic in the go function.
func TestEnsureVPCLoadBalancer(t *testing.T) {
	// build up inputs for the call to the VPCLoadBlanancer class that are used for all test variations
	clusterName := "clusterName-Ensure"
	ctx := context.Background()
	cloud, _, _ := getTestCloud()
	cloud.Config.Prov.ClusterID = "clusterID_Ensure" // Required as it's included in the name

	// ibm_vpc_loadbalancer.go is more of an interface/wrapper that does its work by calling a separately
	// installed binary.  That binary does not exist in this context, so we patch (mock) execVpcCommand().
	// The original reference is saved so it can be restored once the tester is done.  If we don't spoof
	// then the VPC load blancer will attempt to call the actual binary.  That reference won't resolve in
	// this context and we will get nothing but errors.
	oldExecVpc := execVpcCommand
	spoofVpcBinary()
	defer func() { execVpcCommand = oldExecVpc }()

	{
		// We guide what we want the mocked binary to do based on the service name.  The first
		// test is to force the mocked logic to return an error.  This will make sure
		// ibm_vpc_loadbalancer.go can handle an error back from the real binary.
		t.Log("starting VPC create test variation 1 ... the mock service intentionally throws an error")
		service := getLoadBalancerService("service-EnsureCreateError")
		lbStatus, err := cloud.ensureVpcLoadBalancer(ctx, clusterName, service, nil)

		if nil != lbStatus {
			t.Fatalf("service-EnsureCreateError: unexpected status object returned when we are in an error state")
		}

		if err != nil {
			t.Logf("service-EnsureCreateError: the test case expects an error and it received one ... %v", err)
			t.Log("completed VPC create test variation 1 ... the mock service intentionally throws an error")
		} else {
			t.Fatalf("service-EnsureCreateError: Expected error from call but no error was returned")
		}
	}

	{
		// We guide what we want the mocked binary to do based on the service name.  This
		// test is to force the mocked logic to return an error because the binary is returning
		// unexpected data.  This test will make sure ibm_vpc_loadbalancer.go can handle
		// unexpected data back back from the real binary (assuming that would ever happen)
		t.Log("starting VPC create test variation 2 ... the mock service intentionally returns invalid data")
		service := getLoadBalancerService("service-EnsureCreateErrorData")
		lbStatus, err := cloud.ensureVpcLoadBalancer(ctx, clusterName, service, nil)

		if nil != lbStatus {
			t.Fatalf("service-EnsureCreateErrorData: unexpected status object returned when we are in an error state")
		}

		if err != nil {
			t.Logf("service-EnsureCreateErrorData: the test case expects an error and it received one ... %v", err)
			t.Log("completed VPC create test variation 2 ... the mock service intentionally returns invalid data")
		} else {
			t.Fatalf("service-EnsureCreateErrorData: Expected error from call but no error was returned")
		}
	}

	{
		// We guide what we want the mocked binary to do based on the service name.  This
		// test is to force the mocked logic to return PENDING.  The implementation
		// currently throws an error for this case so we will start by parsing the response
		// for an error.  The logic of the test may change as we learn more about what the
		// behavior should be.
		t.Log("starting VPC create test variation 3 ... the mock service intentionally returns PENDING")
		service := getLoadBalancerService("service-EnsureCreatePending")
		lbStatus, err := cloud.ensureVpcLoadBalancer(ctx, clusterName, service, nil)

		if nil != lbStatus {
			t.Fatalf("service-EnsureCreateErrorPending: unexpected status object returned when we are in a pending state")
		}

		if err != nil {
			t.Logf("service-EnsureCreatePending: the test case expects an error and it received one ... %v", err)
			t.Log("completed VPC create test variation 3 ... the mock service intentionally returns invalid data")
		} else {
			t.Fatalf("service-EnsureCreatePending: Expected error from call but no error was returned")
		}
	}

	{
		// We guide what we want the mocked binary to do based on the service name.  This
		// test is for creating a new LB.
		t.Log("starting VPC create test variation 4 ... create new LB")
		service := getLoadBalancerService("service-EnsureCreateNew")
		lbStatus, err := cloud.ensureVpcLoadBalancer(ctx, clusterName, service, nil)

		if lbStatus == nil {
			t.Fatalf("service-EnsureCreateNew: expected status object back but did not get one")
		}

		if err != nil {
			t.Fatalf("service-EnsureCreateNew: Expected status object back but received an error object instead")
		}

		if lbStatus.Ingress == nil {
			t.Fatalf("service-EnsureCreateNew: Expected an ingress array in the status object, but the array is null")
		}

		ingressArrayLength := len(lbStatus.Ingress)
		if ingressArrayLength == 0 {
			t.Fatalf("service-EnsureCreateNew: Expected an ingress array in the status object, but the array length is 0")
		}

		hostName := lbStatus.Ingress[0].Hostname

		if "hostnew1" != hostName {
			t.Fatalf("service-EnsureCreateNew: the returned hostname in the status object is not what we expect")
		} else {
			t.Log("completed VPC create test variation 4 ... create new LB")
		}
	}

	{
		// Pass nil in as one of the required parameters.
		t.Log("starting VPC create test variation 5 ... pass nil as a required parameter")
		service := getLoadBalancerService("service-EnsureCreateNil")

		// not currently going to test for a nil service.  The non-VPC code does not check
		// that the service is not null, which is the pattern we are following.  So will check
		// for a nil context and continue on.
		lbStatus, err := cloud.ensureVpcLoadBalancer(context.TODO(), clusterName, service, nil)

		if nil != lbStatus {
			t.Fatalf("service-EnsureCreateNil: unexpected status object returned when we are in a pending state")
		}

		if err != nil {
			t.Logf("service-EnsureCreateNil: the test case expects an error and it received one ... %v", err)
		} else {
			t.Fatalf("service-EnsureCreateNil: Expected error from call but no error was returned")
		}

		t.Log("completed VPC create test variation 5 ... pass nil for a rquired parameter")
	}

}

// The following tests the ensureVPCLoadBalancerDeleted function of the ibm_vpc_loadbalancer.go
// file.  The function being tested will delete a VPC load balancer.  The tester has several
// variations based on the logic of the go function being tested.
func TestEnsureVPCLoadBalancerDeleted(t *testing.T) {
	// build up inputs for the call to the VPCLoadBlanancer class that are used for all test variations
	clusterName := "clusterName-EnsureDeleted"
	ctx := context.Background()
	cloud, _, _ := getTestCloud()
	cloud.Config.Prov.ClusterID = "clusterID_EnsureDeleted" // Required as it's included in the name

	// ibm_vpc_loadbalancer.go is more of an interface/wrapper that does its task by calling a separately
	// installed binary.  That binary does not exist in this context, so we patch (mock) execVpcCommand().
	// The original reference is saved so it can be restored once the tester is done.  If we don't spoof
	// then the VPC load blancer will attempt to call the actual binary.  That reference won't resolve in
	// this context and we will get nothing but errors.
	oldExecVpc := execVpcCommand
	spoofVpcBinary()
	defer func() { execVpcCommand = oldExecVpc }()

	{
		// We guide what we want the mocked binary to do based on the service name.  The first
		// test is to force the mocked logic to return an error.  This will make sure
		// ibm_vpc_loadbalancer.go can handle an error back from the real binary.
		t.Log("starting VPC delete test variation 1 ... the mock service intentionally throws an error")
		service := getLoadBalancerService("service-EnsureDeletedError")
		err := cloud.ensureVpcLoadBalancerDeleted(ctx, clusterName, service)
		if err != nil {
			t.Logf("service-EnsureDeletedError: the test case expects an error and it received one ... %v", err)
		} else {
			t.Fatalf("service-EnsureDeletedError: Expected error from call but no error was returned")
		}
	}

	{
		// We guide what we want the mocked binary to do based on the service name.  The next
		// test is to get success back from the mocked logic.
		t.Log("starting VPC delete test variation 2 ... the mock service deletes the LB (success)")
		service := getLoadBalancerService("service-EnsureDeletedSuccess")
		err := cloud.ensureVpcLoadBalancerDeleted(ctx, clusterName, service)
		if err != nil {
			t.Fatalf("service-EnsureDeletedSuccess: unexpected error from from the VPC load balancer")
		} else {
			t.Log("completed VPC delete test variation 2 ... the mock service deletes the LB (success)")
		}
	}

	// For now we are going to comment out the pending test case.  This test is based on the current behavior
	// of the caller, but the interface is still being defined.  This test will be updated once we know what
	// the behavior for pending should be.
	// {
	// 	// We guide what we want the mocked binary to do based on the service name.  The next
	// 	// test is to get success-pending back from the mocked logic.
	// 	t.Log("starting VPC delete test variation 3 ... the mock service deletes the LB (pending)")
	// 	service := getLoadBalancerService("service-EnsureDeletedPending")
	// 	err := cloud.ensureVpcLoadBalancerDeleted(ctx, clusterName, service)
	// 	if err != nil {
	// 		t.Logf("service-EnsureDeletedPending: the test case expects an error and it received one ... %v", err)
	// 	} else {
	// 		t.Fatalf("service-EnsureDeletedPending: unexpected error from from the VPC load balancer")
	// 	}
	// }

	{
		// We guide what we want the mocked binary to do based on the service name.  The next
		// test is to get success-notFound back from the mocked logic.
		t.Log("starting VPC delete test variation 4 ... the mock service deletes the LB (not found)")
		service := getLoadBalancerService("service-EnsureDeletedNotFound")
		err := cloud.ensureVpcLoadBalancerDeleted(ctx, clusterName, service)
		if err != nil {
			t.Fatalf("service-EnsureDeletedNotFound: unexpected error from from the VPC load balancer")
		} else {
			t.Log("completed VPC delete test variation 4 ... the mock service deleted the LB (not found)")
		}
	}

	{
		// We guide what we want the mocked binary to do based on the service name.  The next
		// test will run through an error path in the load balancer.  The mock binary will return
		// only invalid data to the lb code to make sure unexpected data is handled.
		t.Log("starting VPC delete test variation 5 ... the mock service creates an unexpected messages")
		service := getLoadBalancerService("service-EnsureDeletedUnknown1")
		err := cloud.ensureVpcLoadBalancerDeleted(ctx, clusterName, service)
		if err != nil {
			t.Log("completed VPC delete test variation 5 ... the mock service creates an unexpected message and an error is returned")
		} else {
			t.Fatalf("service-EnsureDeletedError: Expected error from call but no error was returned")
		}
	}

	{
		// We guide what we want the mocked binary to do based on the service name.  The next
		// test will run through another invalid data path in the load balancer.  The mock binary will
		// return a mix of valid and invalid data to the lb code to make sure unexpected data is ignored.
		t.Log("starting VPC delete test variation 6 ... the mock service creates an unexpected message followed by success")
		service := getLoadBalancerService("service-EnsureDeletedUnknown2")
		err := cloud.ensureVpcLoadBalancerDeleted(ctx, clusterName, service)
		if err != nil {
			t.Fatalf("service-EnsureDeletedEnknown2: unexpected error from from the VPC load balancer")
		} else {
			t.Log("completed VPC delete test variation 6 ... the mock service creates an unexpected messages")
		}
	}

	{
		// We guide what we want the mocked binary to do based on the service name.  The next
		// test is to get success with additional info messages back from the mocked logic.
		t.Log("starting VPC delete test variation 7 ... the mock service deletes the LB (info)")
		service := getLoadBalancerService("service-EnsureDeletedInfo")
		err := cloud.ensureVpcLoadBalancerDeleted(ctx, clusterName, service)
		if err != nil {
			t.Fatalf("service-EnsureDeletedInfo: unexpected error from from the VPC load balancer")
		} else {
			t.Log("completed VPC delete test variation 7 ... the mock service deletes the LB (info)")
		}
	}
}

const testServiceUID1 = "48fa9e64-9398-11e9-846b-6e8481030173"
const testServiceUID2 = "537c31f6-9395-11e9-846b-6e8481030173"
const TIMEOUT = 3 * time.Second

var monitorTestDesiredStatus string

// whisperer is a testing tool used to observe the 'newStatus' input parameter
// to the 'triggerEvent' function
var whisperer = make(chan string)

func mockTriggerEvent(eventRecorder *CloudEventRecorder, service *v1.Service, newStatus string) {
	// Gossip on the channel about the newStatus
	whisperer <- newStatus
}

// The following tests the monitorVpcLoadBalancer function of the ibm_vpc_loadbalancer.go
// file. The function being tested triggers events based on status of VPC load balancers
// for a variety of scenarios.
func TestMonitorVpcLoadBalancers(t *testing.T) {
	cloud, _, _ := getVpcCloud()
	cloud.Config.Prov.ClusterID = "8989b5a22f23474c85f02985dd60ea88"
	services, _ := cloud.KubeClient.CoreV1().Services(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})

	testCases := []struct {
		name           string
		oldStatus      string
		newStatus      string
		expectedResult string
	}{
		{ // TEST CASE #1: Test that normal events are generated for load balancer coming online
			name:           "VPC Load Balancer Coming Online",
			oldStatus:      vpcStatusOfflineCreatePending,
			newStatus:      vpcStatusOnlineActive,
			expectedResult: vpcStatusOnlineActive,
		},
		{ // TEST CASE #2: Test that missing load balancer objects in RIaaS are properly detected
			name:           "VPC Load Balancer Not Found",
			oldStatus:      vpcStatusOfflineNotFound,
			newStatus:      vpcStatusOfflineNotFound,
			expectedResult: vpcStatusOfflineNotFound,
		},
		{ // TEST CASE #3: Test that load balancer failures are properly detected
			name:           "VPC Load Balancer Failure",
			oldStatus:      vpcStatusOfflineFailed,
			newStatus:      vpcStatusOfflineFailed,
			expectedResult: vpcStatusOfflineFailed,
		},
		{ // TEST CASE #4: Test that a normal event is generated for "fast" load balancer creates (no state from previous call to monitor)
			name:           "VPC Load Balancer Fast Create",
			oldStatus:      "", // simulate case where previous call to Monitor did not return a status for the LBaaS
			newStatus:      vpcStatusOnlineActive,
			expectedResult: vpcStatusOnlineActive,
		},
	}

	// Patch and defer restore of execVpcCommand()
	oldExecVpc := execVpcCommand
	spoofVpcBinary()
	defer func() { execVpcCommand = oldExecVpc }()

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Logf("TEST CASE: %s", testCase.name)

			// Event generation is dependent on both the old status and current status
			// of a VPC load balancer. Here we initialize the "oldStatus" of the
			// load balancer which would normally be set by a previous call to monitor
			// and log data in the map for aid in test debugging
			status := make(map[string]string)
			if testCase.oldStatus != "" {
				status[testServiceUID1] = testCase.oldStatus
			}
			t.Log("Previous Monitor Data: ", status)

			// Transition to new status and monitor the load balancer
			newStatus := testCase.newStatus
			monitorTestDesiredStatus = newStatus

			done := make(chan string)
			go func(data map[string]string) {
				t.Log("Running the monitor...")
				monitorVpcLoadBalancers(cloud, services, data, mockTriggerEvent)
				done <- "Monitor finished"
			}(status)

			var result string
			select {
			case result = <-whisperer: // Observe the input parameters of triggerEvent
			case <-time.After(TIMEOUT): // Handle the case where no event is generated
				t.Log("WARNING: No event triggered. Ensure this is expected behavior. If not, consider increasing the timeout length")
				result = "Non-Event/Timeout"
			}
			<-done

			// Log updated data in the map for aid in test debugging
			t.Log("Current Monitor Data: ", status)

			// Verify that events are being triggered when expected
			if result != testCase.expectedResult {
				t.Fatalf("FAILURE: Unexpected event trigger for VPC load balancer status. Want: Event for %s\tGot: %s", testCase.expectedResult, result)
			} else {
				t.Logf("SUCCESS: VPC load balancer event for status: %s created successfully", result)
			}
			// NOTE: The 'triggerEvent' subroutine of the 'monitorVpcLoadBalancer' function decides which type of event to
			// generate based off the 'newStatus' of the VPC load balancer. We are checking that events are being created when expected
			// by checking the value of 'newStatus' when 'triggerEvent' is called using a channel. Is checking the newStatus as good
			// as testing whether the correct event was generated? Changes to 'triggerEvent' could cause this test to fail silently,
			// though this seems somewhat tolerable as this test is meant to exercise the logic which decides WHEN events are generated.
			// Create a separate test for 'triggerEvent'
		})
	}
}

// The following tests the isNewLoadBalancer function of the ibm_vpc_loadbalancer.go
// file. The function being tested returns a boolean representing whether the load balancer
// has been created within the last 24 hours.
func TestIsNewLoadBalancer(t *testing.T) {

	testCases := []struct {
		name            string
		loadBalancerAge time.Duration // Age of VPC load balancer in hours
		expectedResult  bool
	}{
		{
			name:            "Correctly Identifies New Load Balancer Service",
			loadBalancerAge: 0,
			expectedResult:  true,
		},
		{
			name:            "Correctly Identifies Old Load Balancer Service",
			loadBalancerAge: 25,
			expectedResult:  false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			creationTime := metav1.Time{Time: time.Now().Add(time.Hour * -1 * testCase.loadBalancerAge)}
			service := createTestVPCLoadBalancerService("test-lb", testServiceUID1, creationTime)

			result := isNewLoadBalancer(service)
			if result != testCase.expectedResult {
				t.Fatalf("Unexpected ruling on load balancer novelty. Want isNew: %t\tGot: %t", testCase.expectedResult, result)
			} else {
				t.Logf("SUCCESS: Load balancer is new: - %t", result)
			}
		})
	}
}

func TestFindField(t *testing.T) {
	testCases := []struct {
		name           string
		lineData       string
		prefix         string
		expectedResult string
	}{
		{
			name:           "Correctly Finds Status",
			lineData:       "INFO: ServiceUID:48fa9e64-9398-11e9-846b-6e8481030173 Name:lbd50c-48fa9e64939811e9846b6e8481030173 ID:96495e91-7163-47d0-b0e1-70006f2933b9 Hostname:96495e91-us-south.lb.appdomain.cloud Status:online/active Pools:tcp-30703 Private:10.240.0.17,10.240.0.21 Public:169.61.246.106,169.61.246.70",
			prefix:         "Status",
			expectedResult: "online/active",
		},
		{
			name:           "Correctly Finds ServiceUID",
			lineData:       "INFO: ServiceUID:48fa9e64-9398-11e9-846b-6e8481030173 Name:lbd50c-48fa9e64939811e9846b6e8481030173 ID:96495e91-7163-47d0-b0e1-70006f2933b9 Hostname:96495e91-us-south.lb.appdomain.cloud Status:online/active Pools:tcp-30703 Private:10.240.0.17,10.240.0.21 Public:169.61.246.106,169.61.246.70",
			prefix:         "ServiceUID",
			expectedResult: "48fa9e64-9398-11e9-846b-6e8481030173",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			data := findField(testCase.lineData, testCase.prefix)
			if data != testCase.expectedResult {
				t.Fatalf("Expected data missing/not found. Want: %s\tGot: %s", testCase.expectedResult, data)
			} else {
				t.Logf("SUCCESS: Found the correct data: - %s: %s", testCase.prefix, data)
			}
		})
	}
}

// spoofVpcBinary reassigns execVpcCommand from a function which calls the `vpcctl` binary
// to a mocked version of the binary for testing purposes.
func spoofVpcBinary() {
	execVpcCommand = func(argString string, envvars []string) ([]string, error) {
		args := strings.Fields(argString)
		args = append([]string{"vpcctl"}, args...)
		if len(args) < 2 {
			fmt.Println("ERROR: Invalid input to binary: too few arguments", args)
			return nil, nil
		}

		cmd := strings.ToLower(args[1])
		var arg1 string
		var arg2 string
		if len(args) > 2 {
			arg1 = args[2]
		}
		if len(args) > 3 {
			arg2 = args[3]
		}

		switch cmd {
		// Cloud provider options
		case "create-lb":
			outputArray, err := spoofCreateLB(arg1, arg2)
			return outputArray, err
		case "delete-lb":
			outputArray, err := spoofDeleteLB(arg1, arg2) // multiple messages can be returned so spoofDeleteLB returns a string array
			return outputArray, err
		case "monitor":
			return spoofMonitor()
		case "status-lb":
			outputArray, err := spoofStatusLB(arg1, arg2)
			return outputArray, err
		case "update-lb":
		default:
			fmt.Printf("Invalid option: %s\n", cmd)
		}
		return nil, nil
	}
}

// spoofMonitor dynamically returns 1 of 7 different statuses for a VPC load balancer
// as requested by the caller via setting of the monitorTestDesiredStatus  global variable.
// The ability to manipulate representation of load balancer state allows for simulation of all status transitions
func spoofMonitor() ([]string, error) {
	/*
		VPC load balancer status:

		previous status <- data map[string]string
		current status <- binary

		the previous state is provided by a map and is easily manipulated by the caller during testing.
		the current state is provided by the 'vpcctl' binary and is spoofed here.

	*/

	desiredStatus := monitorTestDesiredStatus
	var vpcCurrentStatus string

	// Return the status requested by the caller (TestMonitorVpcLoadBalancers)
	switch desiredStatus {
	case vpcStatusOfflineNotFound:
		// Status: VPC load balancer is offline/not_found
		vpcCurrentStatus = fmt.Sprintf("NOT_FOUND: ServiceUID:%s Message:VPC load balancer not found for service", testServiceUID1)
	default:
		// The data returned by the vpcctl binary is of the same form for all
		// load balancer statuses except "not_found" (handled above)
		vpcCurrentStatus = fmt.Sprintf("INFO: ServiceUID:%s Status:%s", testServiceUID1, desiredStatus)
	}

	// Return the requested VPC load balancer status
	var stringArray = []string{vpcCurrentStatus}
	return stringArray, nil
}

func spoofCreateLB(lbName string, serviceName string) ([]string, error) {
	serviceID := strings.Split(lbName, "-")[2] // lbName is of the form "kube-<CLUSTERID>-<SERVICE_UID_DASHES_REMOVED>"
	testcase := serviceID

	switch testcase {
	case "serviceEnsureCreateError":
		stringArray := make([]string, 1)
		stringArray[0] = "ERROR: The mock service is intentionally throwing an error to exercise the error leg of the code."
		return stringArray, errors.New("the mock service is intentionally throwing error in the delete case")
	case "serviceEnsureCreateErrorData":
		stringArray := make([]string, 1)
		stringArray[0] = "This data is not valid since it does not start with KEY:.  The caller should generate an error."
		return stringArray, nil
	case "serviceEnsureCreatePending":
		stringArray := make([]string, 3)
		stringArray[0] = "INFO: the VPC LB creation started"
		stringArray[1] = "INFO: the VPC LB creation is still running"
		stringArray[2] = "PENDING: hostnew2" // the convention is the hostname follows the key
		return stringArray, nil
	case "serviceEnsureCreateNew":
		stringArray := make([]string, 3)
		stringArray[0] = "INFO: the VPC LB creation started"
		stringArray[1] = "INFO: the VPC LB creation is still running"
		stringArray[2] = "SUCCESS: hostnew1" // the convention is the hostname follows the "SUCCESS:" key
		return stringArray, nil
	default:
		stringArray := make([]string, 1)
		stringArray[0] = "ERROR: The LoadBalancer name did not match one of the test cases"
		return stringArray, errors.New("Failed in the tester because the mock service didn't know what to do")
	}
}

// A mock helper method spoofing cloud.StatusLB() from vpcctl
func spoofStatusLB(lbName string, serviceName string) ([]string, error) {
	serviceID := strings.Split(lbName, "-")[2] // lbName is of the form "kube-<CLUSTERID>-<SERVICE_UID_DASHES_REMOVED>"
	stringArray := make([]string, 0, 2)

	switch serviceID {
	case "loadBalancerExists":
		stringArray = append(stringArray, "SUCCESS: The load balancer exists!")
		return stringArray, nil
	case "loadBalancerDoesNOTexist":
		stringArray = append(stringArray, "NOT_FOUND: The load balancer was not found")
		return stringArray, nil
	case "loadBalancerPending":
		stringArray = append(stringArray, "PENDING: The load balancer is pending")
		return stringArray, nil
	case "infoBogusSuccess":
		stringArray = append(stringArray, "INFO: Hold on. We are checking up on your load balancer")
		stringArray = append(stringArray, "We do not have the usual [LineType: linedata] here")
		stringArray = append(stringArray, "SUCCESS: The load balancer exists!")
		return stringArray, nil
	default:
		stringArray = append(stringArray, "ERROR: The LoadBalancer name did not match one of these cases")
		return stringArray, errors.New("Failed getting LoadBalancer")
	}
}

func spoofDeleteLB(lbName string, serviceName string) ([]string, error) {
	serviceID := strings.Split(lbName, "-")[2] // lbName is of the form "kube-<CLUSTERID>-<SERVICE_UID_DASHES_REMOVED>"
	testcase := serviceID

	// The serviceID is used to indicate which variation to run.
	// Notice that except in the force-error case, an array of messages is returned to the caller.  The
	// caller will parse the returned array until a message starts with ERROR, NOT_FOUND, PENDING or SUCCESS.
	// Messages before that are logged and parsing continues.  For example, the array may contain multiple
	// INFO messages before SUCCESS is returned.  If there is an ERROR after success then that ERROR is
	// ignored since the caller stops parsing at SUCCESS.

	switch testcase {
	case "serviceEnsureDeletedError":
		stringArray := make([]string, 1)
		stringArray[0] = "ERROR: The mock service is intentionally throwing an error to exercise the error leg of the code."
		return stringArray, errors.New("the mock service is intentionally throwing error in the delete case")
	case "serviceEnsureDeletedSuccess":
		stringArray := make([]string, 1)
		stringArray[0] = "SUCCESS: the VPC LB is deleted"
		return stringArray, nil
	case "serviceEnsureDeletedPending":
		stringArray := make([]string, 4)
		stringArray[0] = "INFO: the VPC LB deletion started"
		stringArray[1] = "INFO: the VPC LB deletion is still running"
		stringArray[2] = "INFO: the VPC LB deletion is taking too long"
		stringArray[3] = "PENDING: the VPC LB deletion is pending"
		return stringArray, nil
	case "serviceEnsureDeletedNotFound":
		stringArray := make([]string, 1)
		stringArray[0] = "NOT_FOUND: the VPC LB deletion is successful because the service is not found"
		return stringArray, nil
	case "serviceEnsureDeletedInfo":
		stringArray := make([]string, 3)
		stringArray[0] = "INFO: the VPC LB deletion will work, we are returning some info statements in addition to the success line"
		stringArray[1] = "there is not colon so this line is ignored"
		stringArray[2] = "SUCCESS: the VPC LB is deleted"
		return stringArray, nil
	case "serviceEnsureDeletedUnknown1":
		stringArray := make([]string, 2)
		stringArray[0] = "INFO: adding an info message in front of the bogus message that should cause an error in the load balancer"
		stringArray[1] = "BOGUS: the label on this message is not expected"
		return stringArray, nil
	case "serviceEnsureDeletedUnknown2":
		stringArray := make([]string, 3)
		stringArray[0] = "INFO: adding an info message in front of the bogus message, but no error since the next message is success"
		stringArray[1] = "BOGUS: the label on this message is not expected"
		stringArray[2] = "SUCCESS: the VPC LB is deleted"
		return stringArray, nil
	default:
		stringArray := make([]string, 1)
		stringArray[0] = "ERROR: The LoadBalancer name did not match one of these cases"
		return stringArray, errors.New("Failed getting LoadBalancer")
	}
}

// getVpcCloud builds Cloud object which carries VPC related items only
// This is a modification of the getCloud method for Classic
func getVpcCloud() (*Cloud, string, *fake.Clientset) {
	var cc CloudConfig

	// Build fake client resources for test cloud.
	s1 := createTestVPCLoadBalancerService("test-lb", testServiceUID1, metav1.Time{Time: time.Now()})
	s2 := createTestVPCLoadBalancerService("test-lb2", testServiceUID2, metav1.Time{Time: time.Now()})
	fakeKubeClient := fake.NewSimpleClientset(s1, s2)
	fakeKubeClientV1 := fake.NewSimpleClientset()

	// Build test cloud.
	cc.Global.Version = "1.0.0"
	cc.Kubernetes.ConfigFilePaths = []string{"../test-fixtures/kubernetes/k8s-config"}
	cc.Prov.ProviderType = "gc"

	c := Cloud{
		Name:       "ibm",
		KubeClient: fakeKubeClient,
		Config:     &cc,
		Recorder:   NewCloudEventRecorderV1("ibm", fakeKubeClientV1.CoreV1().Events(lbDeploymentNamespace)),
		CloudTasks: map[string]*CloudTask{},
	}
	return &c, "test", fakeKubeClient
}

// createTestVPCLoadBalancerService creates a Kubernetes service object of type load balancer
func createTestVPCLoadBalancerService(serviceName, serviceUID string, creationTime metav1.Time) *v1.Service {
	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:              serviceName,
			Namespace:         "ibm-system",
			UID:               types.UID(serviceUID),
			SelfLink:          "/apis/apps/v1/namespaces/" + lbDeploymentNamespace + "/services/" + serviceName,
			CreationTimestamp: creationTime,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{
				Port:     80,
				Protocol: v1.ProtocolTCP,
			}},
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{{IP: "1.1.1.1"}},
			},
		},
	}
	return s
}

func TestDetermineCommandArgs(t *testing.T) {
	testSvc := &v1.Service{ObjectMeta: metav1.ObjectMeta{
		Name:      "serviceName",
		Namespace: "testNamespace"}}
	cloud, _, _ := getVpcCloud()
	cmd := cloud.determineCommandArgs("cmd", "testLBName", testSvc)
	if cmd != "cmd testLBName testNamespace/serviceName" {
		t.Fatalf("Incorrect command arguments generated.")
	}
}

func TestDetermineEnvironemtSettings(t *testing.T) {
	testCases := []struct {
		clusterID   string
		nodes       []*v1.Node
		provider    string
		expectedEnv []string
	}{
		{ // VPC Gen2 Provider.  List of nodes and cluster ID provided
			clusterID:   "clusterID",
			nodes:       []*v1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "192.168.1.1"}}, {ObjectMeta: metav1.ObjectMeta{Name: "192.168.2.2"}}},
			provider:    lbVpcNextGenProvider,
			expectedEnv: []string{"KUBECONFIG=../test-fixtures/kubernetes/k8s-config", "G2_WORKER_SERVICE_ACCOUNT_ID=accountID", "VPCCTL_CLUSTER_ID=clusterID", "VPCCTL_NODE_LIST=192.168.1.1,192.168.2.2"},
		},
		{ // VPC Gen2 Provider. List of nodes not provided.
			clusterID:   "clusterID",
			provider:    lbVpcNextGenProvider,
			expectedEnv: []string{"KUBECONFIG=../test-fixtures/kubernetes/k8s-config", "G2_WORKER_SERVICE_ACCOUNT_ID=accountID", "VPCCTL_CLUSTER_ID=clusterID"},
		},
		{ // VPC Gen2 Provider. List of nodes not provided. ClusterID not set in config
			provider:    lbVpcNextGenProvider,
			expectedEnv: []string{"KUBECONFIG=../test-fixtures/kubernetes/k8s-config", "G2_WORKER_SERVICE_ACCOUNT_ID=accountID"},
		},
		{ // Classic Provider. List of nodes not provided. ClusterID not set in config
			provider:    lbVpcClassicProvider,
			expectedEnv: []string{"KUBECONFIG=../test-fixtures/kubernetes/k8s-config"},
		},
	}

	for _, tc := range testCases {
		// Testing G2
		cloud, _, _ := getVpcCloud()
		cloud.Config.Prov.ClusterID = tc.clusterID
		cloud.Config.Prov.ProviderType = tc.provider
		cloud.Config.Prov.G2WorkerServiceAccountID = "accountID"

		env := cloud.determineVpcEnvSettings(tc.nodes)
		if len(env) != len(tc.expectedEnv) {
			t.Fatalf("Incorrect environment settings generated. Expected: %v, Got %v", tc.expectedEnv, env)
		}
		for i, v := range env {
			if v != tc.expectedEnv[i] {
				t.Fatalf("Incorrect environment settings generated. Expected: %s, Got %s", tc.expectedEnv[i], v)
			}
		}
	}
}
