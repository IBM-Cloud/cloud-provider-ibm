/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2019, 2023 All Rights Reserved.
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
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestMetadataService(t *testing.T) {
	k8sclient := fake.NewSimpleClientset()
	mdService := NewMetadataService(nil, k8sclient)
	var node NodeMetadata
	var err error
	var expectedMetadata NodeMetadata
	var labels map[string]string
	var cmp bool

	// ask for node that is not defined
	_, err = mdService.GetNodeMetadata("nosuchnode")
	if nil == err {
		t.Fatalf("Did not get an error for non-existent node")
	}

	// ask for node with all labels.
	expectedMetadata = NodeMetadata{
		InternalIP:    "test-internal-ip",
		ExternalIP:    "test-external-ip",
		WorkerID:      "test-worker-id",
		InstanceType:  "test-machine-type",
		FailureDomain: "test-failure-domain",
		Region:        "test-region",
		ProviderID:    "test-provider-id",
	}
	labels = map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  expectedMetadata.InternalIP,
		"ibm-cloud.kubernetes.io/external-ip":  expectedMetadata.ExternalIP,
		"ibm-cloud.kubernetes.io/zone":         expectedMetadata.FailureDomain,
		"ibm-cloud.kubernetes.io/region":       expectedMetadata.Region,
		"ibm-cloud.kubernetes.io/worker-id":    expectedMetadata.WorkerID,
		"ibm-cloud.kubernetes.io/machine-type": expectedMetadata.InstanceType,
	}
	k8snode := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "goodnode",
			Labels: labels},
		Spec: corev1.NodeSpec{
			ProviderID: expectedMetadata.ProviderID,
		},
	}
	_, err = k8sclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node goodnode: %v", err)
	}
	node, err = mdService.GetNodeMetadata("goodnode")
	if nil != err {
		t.Fatalf("Got an error for goodnode: %v", err)
	}
	cmp = reflect.DeepEqual(expectedMetadata, node)
	if !cmp {
		t.Fatal("NodeMetadata not correct for 'goodnode'.")
	}

	// modify goodnode and verify we don't get new data until after time has passed
	mdService.nodeCacheStart = time.Now().Add(-cacheTTL + time.Second)
	labels["ibm-cloud.kubernetes.io/region"] = "modified-region"
	k8snode = corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "goodnode",
			Labels: labels},
		Spec: corev1.NodeSpec{
			ProviderID: expectedMetadata.ProviderID,
		},
	}
	k8sclient.CoreV1().Nodes().Update(context.TODO(), &k8snode, metav1.UpdateOptions{})
	if nil != err {
		t.Fatalf("Failed to update Node goodnode: %v", err)
	}
	node, err = mdService.GetNodeMetadata("goodnode")
	if nil != err {
		t.Fatalf("Got an error for goodnode: %v", err)
	}
	cmp = reflect.DeepEqual(expectedMetadata, node)
	if !cmp {
		t.Fatal("NodeMetadata not correct for modified 'goodnode'.")
	}
	// set md cache start back in time and try again...
	expectedMetadata.Region = "modified-region"
	mdService.nodeCacheStart = time.Now().Add(-cacheTTL - time.Second)
	node, err = mdService.GetNodeMetadata("goodnode")
	if nil != err {
		t.Fatalf("Got an error for goodnode: %v", err)
	}
	cmp = reflect.DeepEqual(expectedMetadata, node)
	if !cmp {
		t.Fatal("NodeMetadata not correct for modified 'goodnode' after cache expired.")
	}
	// delete 'goodnode'
	k8sclient.CoreV1().Nodes().Delete(context.TODO(), "goodnode", metav1.DeleteOptions{})
	mdService.deleteCachedNode("goodnode")
	_, err = mdService.GetNodeMetadata("goodnode")
	if nil == err {
		t.Fatalf("Did not get expected error after deleting goodnode.")
	}

	// ask for node missing a required label
	requiredLabels := []string{
		"ibm-cloud.kubernetes.io/internal-ip",
		// "ibm-cloud.kubernetes.io/external-ip", this is optional
		"ibm-cloud.kubernetes.io/zone",
		"ibm-cloud.kubernetes.io/region",
		"ibm-cloud.kubernetes.io/worker-id",
		"ibm-cloud.kubernetes.io/machine-type",
		"foo",
	}
	for i := range requiredLabels {
		l := requiredLabels[i]
		labels = map[string]string{
			"ibm-cloud.kubernetes.io/internal-ip":  expectedMetadata.InternalIP,
			"ibm-cloud.kubernetes.io/external-ip":  expectedMetadata.ExternalIP,
			"ibm-cloud.kubernetes.io/zone":         expectedMetadata.FailureDomain,
			"ibm-cloud.kubernetes.io/region":       expectedMetadata.Region,
			"ibm-cloud.kubernetes.io/worker-id":    expectedMetadata.WorkerID,
			"ibm-cloud.kubernetes.io/machine-type": expectedMetadata.InstanceType,
		}
		delete(labels, l)
		k8snode = corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "partialnode",
				Labels: labels},
			Spec: corev1.NodeSpec{
				ProviderID: expectedMetadata.ProviderID,
			},
		}
		_, err = k8sclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
		if nil != err {
			t.Fatalf("Failed to create Node partialnode: %v", err)
		}
		_, err = mdService.GetNodeMetadata("partialnode")
		// as a sanity check, err should be nil for l=foo (no missing labels)
		if nil == err && l != "foo" {
			t.Fatalf("Did not get an error for partial node missing label=%s", l)
		}
		k8sclient.CoreV1().Nodes().Delete(context.TODO(), "partialnode", metav1.DeleteOptions{})
	}

	// ask for node with no external IP
	expectedMetadata = NodeMetadata{
		InternalIP:    "test-internal-ip",
		WorkerID:      "test-worker-id",
		InstanceType:  "test-machine-type",
		FailureDomain: "test-failure-domain",
		Region:        "test-region",
		ProviderID:    "test-provider-id",
	}
	labels = map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip": expectedMetadata.InternalIP,
		// "ibm-cloud.kubernetes.io/external-ip": expectedMetadata.ExternalIP,
		"ibm-cloud.kubernetes.io/zone":         expectedMetadata.FailureDomain,
		"ibm-cloud.kubernetes.io/region":       expectedMetadata.Region,
		"ibm-cloud.kubernetes.io/worker-id":    expectedMetadata.WorkerID,
		"ibm-cloud.kubernetes.io/machine-type": expectedMetadata.InstanceType,
	}
	k8snode = corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "privateonlynode",
			Labels: labels},
		Spec: corev1.NodeSpec{
			ProviderID: expectedMetadata.ProviderID,
		},
	}
	_, err = k8sclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node privateonlynode: %v", err)
	}
	node, err = mdService.GetNodeMetadata("privateonlynode")
	if nil != err {
		t.Fatalf("Got an error for privateonlynode: %v", err)
	}
	cmp = reflect.DeepEqual(expectedMetadata, node)
	if !cmp {
		t.Fatal("NodeMetadata not correct for 'privateonlynode'.")
	}

	// ask for node with no providerID
	expectedMetadata = NodeMetadata{
		InternalIP:    "test-internal-ip",
		WorkerID:      "test-worker-id",
		InstanceType:  "test-machine-type",
		FailureDomain: "test-failure-domain",
		Region:        "test-region",
		ProviderID:    "",
	}
	labels = map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  expectedMetadata.InternalIP,
		"ibm-cloud.kubernetes.io/external-ip":  expectedMetadata.ExternalIP,
		"ibm-cloud.kubernetes.io/zone":         expectedMetadata.FailureDomain,
		"ibm-cloud.kubernetes.io/region":       expectedMetadata.Region,
		"ibm-cloud.kubernetes.io/worker-id":    expectedMetadata.WorkerID,
		"ibm-cloud.kubernetes.io/machine-type": expectedMetadata.InstanceType,
	}
	k8snode = corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "noprovideridnode",
			Labels: labels},
	}
	_, err = k8sclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node noprovideridnode: %v", err)
	}
	node, err = mdService.GetNodeMetadata("noprovideridnode")
	if nil != err {
		t.Fatalf("Got an error for noprovideridnode: %v", err)
	}
	cmp = reflect.DeepEqual(expectedMetadata, node)
	if !cmp {
		t.Fatal("NodeMetadata not correct for 'noprovideridnode'.")
	}
}
