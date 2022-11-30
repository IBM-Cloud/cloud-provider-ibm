/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2021, 2022 All Rights Reserved.
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
	"k8s.io/client-go/kubernetes/fake"
)

func getNodeWatchTestCloud() (*Cloud, *fake.Clientset) {
	var cc CloudConfig
	fakeKubeClient := fake.NewSimpleClientset()
	cc.Prov.AccountID = "testAccount"
	cc.Prov.ClusterID = "testCluster"
	cloudMetadata := NewMetadataService(nil, fakeKubeClient)
	c := Cloud{
		Name:       "ibm",
		KubeClient: fakeKubeClient,
		Config:     &cc,
		Metadata:   cloudMetadata,
	}
	return &c, fakeKubeClient
}

func TestNodeWatch(t *testing.T) {
	c, k8sclient := getNodeWatchTestCloud()
	var err error
	var expectedInstanceID string
	var labels map[string]string
	var nodeName string = "original-internal-ip"
	expectedInstanceID = "ibm://testAccount///testCluster/original-worker-id"
	labels = map[string]string{
		"ibm-cloud.kubernetes.io/internal-ip":  nodeName,
		"ibm-cloud.kubernetes.io/external-ip":  "test-external-ip",
		"ibm-cloud.kubernetes.io/zone":         "test-failure-domain",
		"ibm-cloud.kubernetes.io/region":       "test-region",
		"ibm-cloud.kubernetes.io/worker-id":    "original-worker-id",
		"ibm-cloud.kubernetes.io/machine-type": "test-machine-type",
	}
	k8snode := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodeName,
			Labels: labels},
	}
	_, err = k8sclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node: %v", err)
	}
	metadata, err := c.InstanceMetadata(context.Background(), &k8snode)
	if nil != err {
		t.Fatalf("Got an error getting instanceID: %v", err)
	}
	if metadata.ProviderID != expectedInstanceID {
		t.Fatal("InstanceID not correct for original node.")
	}

	// delete node
	k8sclient.CoreV1().Nodes().Delete(context.TODO(), nodeName, metav1.DeleteOptions{})
	c.handleNodeDelete(&k8snode)
	_, err = c.InstanceMetadata(context.Background(), &k8snode)
	if nil == err {
		t.Fatalf("Did not get expected error after deleting node.")
	}

	// create node, delete it, and immediately recreate
	_, err = k8sclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node: %v", err)
	}
	k8sclient.CoreV1().Nodes().Delete(context.TODO(), nodeName, metav1.DeleteOptions{})
	c.handleNodeDelete(&k8snode)
	expectedInstanceID = "ibm://testAccount///testCluster/replaced-worker-id"
	labels["ibm-cloud.kubernetes.io/worker-id"] = "replaced-worker-id"
	k8snode = corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodeName,
			Labels: labels},
	}
	_, err = k8sclient.CoreV1().Nodes().Create(context.TODO(), &k8snode, metav1.CreateOptions{})
	if nil != err {
		t.Fatalf("Failed to create Node: %v", err)
	}
	metadata, err = c.InstanceMetadata(context.Background(), &k8snode)
	if nil != err {
		t.Fatalf("Got an error getting instanceID: %v", err)
	}
	if metadata.ProviderID != expectedInstanceID {
		t.Fatal("InstanceID not correct for replaced node.")
	}
}
