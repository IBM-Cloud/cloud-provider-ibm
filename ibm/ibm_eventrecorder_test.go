/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2023 All Rights Reserved.
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
	"fmt"
	"strings"
	"testing"
	"time"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	lbDeploymentNamespace = "ibm-system"
)

func createTestResources() (*apps.Deployment, *v1.Service) {
	lbDeployment := &apps.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lbDeploymentName",
			Namespace: lbDeploymentNamespace,
			SelfLink:  "/apis/apps/v1/namespaces/" + lbDeploymentNamespace + "/deployments/lbDeploymentName",
		},
	}
	lbService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lbServiceName",
			Namespace: lbDeploymentNamespace,
			UID:       types.UID("lbServiceUID"),
			SelfLink:  "/apis/apps/v1/namespaces/" + lbDeploymentNamespace + "/services/lbServiceName",
		},
		Spec: v1.ServiceSpec{
			LoadBalancerIP: "192.168.10.30",
		},
	}
	return lbDeployment, lbService
}

func TestNewCloudEventRecorder(t *testing.T) {
	cer := NewCloudEventRecorderV1("ibm", fake.NewSimpleClientset().CoreV1().Events(lbDeploymentNamespace))
	if nil == cer {
		t.Fatalf("Failed to create cloud event recorder")
	} else if 0 != strings.Compare("ibm-cloud-provider", cer.Name) {
		t.Fatalf("Invalid cloud event recorder name: %v", cer.Name)
	}
}

func TestLoadBalancerServiceWarningEvent(t *testing.T) {
	errorMessage := "TestLoadBalancerServiceWarningEvent"
	_, lbService := createTestResources()
	fakeClient := fake.NewSimpleClientset()
	cer := NewCloudEventRecorderV1("ibm", fakeClient.CoreV1().Events(lbDeploymentNamespace))
	err := cer.LoadBalancerServiceWarningEvent(lbService, DeletingCloudLoadBalancerFailed, errorMessage)
	if nil == err {
		t.Fatalf("Failed to create load balancer service warning event")
	}
	errorSubStrings := []string{
		GetCloudProviderLoadBalancerName(lbService),
		lbService.ObjectMeta.Name,
		lbService.ObjectMeta.Namespace,
		fmt.Sprintf("%v", lbService.ObjectMeta.UID),
		errorMessage,
	}
	errorString := fmt.Sprintf("%v", err)
	for _, errorSubString := range errorSubStrings {
		if !strings.Contains(errorString, errorSubString) {
			t.Fatalf("Error message missing data %v: %v", errorSubString, errorString)
		}
	}
	time.Sleep(time.Second * 10)
	eventsGenerated, err := fakeClient.CoreV1().Events(lbDeploymentNamespace).List(context.TODO(), metav1.ListOptions{})
	if nil != err || 1 != len(eventsGenerated.Items) {
		t.Fatalf("Failed to generate events: error: %v, events: %v", err, eventsGenerated.Items)
	}
}
