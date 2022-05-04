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
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	scheduling "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"
)

var (
	testNamespace           = "randomTestNamespace"
	lbCPUResourceRequest    = "5m"
	lbMemoryResourceRequest = "10Mi"
	edge                    = "edge"
	gateway                 = "gateway"
)

func getLoadBalancerService(lbName string) *v1.Service {
	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(lbName),
			Name:      lbName,
			Namespace: lbDeploymentNamespace,
			SelfLink:  "/apis/apps/v1/namespaces/" + lbDeploymentNamespace + "/services/" + lbName,
		},
	}
	s.Annotations = map[string]string{}
	s.Spec.Type = v1.ServiceTypeLoadBalancer
	s.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{{IP: "1.1.1.1"}}
	return s
}

func getLoadBalancerMixedService(lbName string) *v1.Service {
	s := getLoadBalancerService(lbName)
	s.Spec.Ports = []v1.ServicePort{
		{
			Port:     80,
			Protocol: v1.ProtocolTCP,
		},
		{
			Port:     80,
			Protocol: v1.ProtocolUDP,
		},
	}
	return s
}

func getTestLoadBlancerName(lbName string) string {
	return GetCloudProviderLoadBalancerName(getLoadBalancerService(lbName))
}

func createTestLoadBalancerDeployment(lbName, cloudProviderIP string, replicas int32, initializeToleration bool, addEdgeNodeAffinity bool, ipvsFeature bool, dedicatedLabelValue string, privateNlb bool) (*apps.Deployment, *apps.ReplicaSet) {
	lbName = getTestLoadBlancerName(lbName)

	var edgeToleration []v1.Toleration
	if initializeToleration {
		edgeToleration = []v1.Toleration{{}}
	}

	var edgeNodeAffinity *v1.NodeAffinity
	if addEdgeNodeAffinity {
		edgeNodeAffinity = &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      "dedicated",
								Operator: "In",
								Values: []string{
									dedicatedLabelValue,
								},
							},
						},
					},
				},
			},
		}
		if privateNlb {
			edgeNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = append(edgeNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions,
				v1.NodeSelectorRequirement{
					Key:      lbPrivateVlanLabel,
					Operator: "In",
					Values: []string{
						"1",
					},
				})
		} else {
			edgeNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = append(edgeNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions,
				v1.NodeSelectorRequirement{
					Key:      lbPublicVlanLabel,
					Operator: "In",
					Values: []string{
						"1",
					},
				})
		}
	}

	lbDeploymentName := lbDeploymentNamePrefix + cloudProviderIP
	lbDeploymentLabels := map[string]string{
		lbIPLabel:   cloudProviderIP,
		lbNameLabel: lbName,
	}
	lbDeploymentLabelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			lbIPLabel:   cloudProviderIP,
			lbNameLabel: lbName,
		},
	}

	envVars := []v1.EnvVar{}
	if ipvsFeature {
		envVars = []v1.EnvVar{
			{Name: "FEATURES", Value: lbFeatureIPVS},
		}
	}
	d := &apps.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			// #nosec G404 used for testing only
			UID:       types.UID(strconv.FormatInt(rand.Int63(), 10)),
			Name:      lbDeploymentName,
			Namespace: lbDeploymentNamespace,
			Labels:    lbDeploymentLabels,
			SelfLink:  "/apis/apps/v1/namespaces/" + lbDeploymentNamespace + "/deployments/" + lbDeploymentName,
		},
		Spec: apps.DeploymentSpec{
			Replicas: &replicas,
			Selector: lbDeploymentLabelSelector,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   lbDeploymentName,
					Labels: lbDeploymentLabels,
				},
				// NOTE(tjcocozz): TestEnsureLoadBalancerSourceIpUpdate uses this spec definition to vaildate there can only be one
				// RequiredDuringSchedulingIgnoredDuringExecution Pod Affinity Term when customer is using the "externalTrafficPolicy":"Local"
				// on their service
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  lbDeploymentName,
							Image: "FAKE_IMAGE",
							Env:   envVars,
						},
					},
					Affinity: &v1.Affinity{
						NodeAffinity: edgeNodeAffinity,
						PodAffinity: &v1.PodAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
								{
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"fake": "value",
										},
									},
									TopologyKey: v1.LabelHostname,
									Namespaces:  []string{"temp"},
								}, {
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"fake1": "value1",
										},
									},
									TopologyKey: v1.LabelHostname,
									Namespaces:  []string{"temp"},
								},
							},
							PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{},
						},
						PodAntiAffinity: &v1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
								{
									LabelSelector: lbDeploymentLabelSelector,
									TopologyKey:   v1.LabelHostname,
								},
							},
						},
					},
					Tolerations: edgeToleration,
				},
			},
		},
		Status: apps.DeploymentStatus{
			AvailableReplicas: replicas,
		},
	}
	var rs *apps.ReplicaSet
	if replicas > 0 {
		isController := true
		cp := d.DeepCopyObject().(*apps.Deployment)
		rsName := lbDeploymentName + "-replicaset"
		rsNamespace := lbDeploymentNamespace
		rsTemplate := cp.Spec.Template
		rs = &apps.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				// #nosec G404 used for testing only
				UID:       types.UID(strconv.FormatInt(rand.Int63(), 10)),
				Name:      rsName,
				Namespace: rsNamespace,
				Labels:    rsTemplate.Labels,
				SelfLink:  "/apis/apps/v1/namespaces/" + rsNamespace + "/replicasets/" + rsName,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       d.GetName(),
					UID:        d.GetUID(),
					Controller: &isController,
				}},
			},
			Spec: apps.ReplicaSetSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: rsTemplate.Labels},
				Template: rsTemplate,
			},
		}
	}
	return d, rs
}

func createTestCloudProviderVlanIPConfigMaps() (*v1.ConfigMap, *v1.ConfigMap, *v1.ConfigMap, *v1.ConfigMap, *v1.ConfigMap, *v1.ConfigMap) {
	cm1 := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nodata",
			Namespace: k8sNamespace,
		},
	}
	emptyData := map[string]string{"vlanipmap.json": "{}"}
	cm2 := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "emptydata",
			Namespace: k8sNamespace,
		},
		Data: emptyData,
	}
	testData := map[string]string{
		"vlanipmap.json": `
		{
		"reserved_ips":[
			{"ip": "192.168.10.15", "subnet_id": "11", "vlan_id": "1", "is_public": true, "zone": "dal09"},
			{"ip": "10.10.10.20", "subnet_id": "22", "vlan_id": "2", "is_public": false, "zone": "dal09"},
			{"ip": "192.168.10.16", "subnet_id": "44", "vlan_id": "4", "is_public": true, "zone": "dal10"},
			{"ip": "10.10.10.30", "subnet_id": "55", "vlan_id": "5", "is_public": false, "zone": "dal10"}],
		"vlans":[
			{"id": "1", "subnets":[{"id": "11", "ips": ["192.168.10.30", "192.168.10.31", "192.168.10.32", "192.168.10.33", "192.168.10.34", "192.168.10.35", "192.168.10.36", "192.168.10.37", "192.168.10.38", "192.168.10.39","192.168.10.50","192.168.10.51","192.168.10.52","192.168.10.53"], "is_public": true}], "zone": "dal09"},
			{"id": "2", "subnets":[{"id": "22", "ips": ["10.10.10.21", "10.10.10.22"], "is_public": false}], "zone": "dal09"},
			{"id": "3", "subnets":[{"id": "33", "ips": ["2001:db8::1"], "is_public": true}], "zone": "dal09"},
			{"id": "4", "subnets":[{"id": "44", "ips": ["192.168.10.40", "192.168.10.41", "192.168.10.42", "192.168.10.43", "192.168.10.44", "192.168.10.45"], "is_public": true}], "zone": "dal10"},
			{"id": "5", "subnets":[{"id": "55", "ips": ["10.10.10.31", "10.10.10.32"], "is_public": false}], "zone": "dal10"}],
		"vlan_errors":[
			{"id":"1502181","subnets":[
				{"is_public":false,"is_byoip":false,"error_reason_code":"ErrorSubnetLimitReached","error_message":"There are already the maximum number of subnets permitted in this VLAN","status":"Failed to create subnet"}], "zone":"mex01","region":"us-south"},
			{"id":"1502179","subnets":[
				{"is_public":true,"is_byoip":false,"error_reason_code":"ErrorSubnetLimitReached","error_message":"There are already the maximum number of subnets permitted in this VLAN","status":"Failed to create subnet"},
				{"is_public":true,"is_byoip":false,"error_reason_code":"ErrorSubnetLimitReached","error_message":"There are already the maximum number of subnets permitted in this VLAN","status":"Failed to create subnet"},
				{"is_public":true,"is_byoip":false,"error_reason_code":"ErrorSoftlayerDown","error_message":"Softlayer is experiencing issues please try ordering your subnet later","status":"Failed to create subnet"}], "zone":"mex01","region":"us-south"}]
		}`}
	cm3_1 := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ibm-cloud-provider-vlan-ip-config",
			Namespace: k8sNamespace,
		},
		Data: testData,
	}
	cm3_2 := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ibm-cloud-provider-vlan-ip-config-ibm-namespace",
			Namespace: lbDeploymentNamespace,
		},
		Data: testData,
	}
	testData = map[string]string{
		"vlanipmap.json": `
		{
		"vlans":[
			{"id": "100", "subnets":[{"id": "1100", "ips": ["192.168.100.30"], "is_public": true}], "zone": "dal09"},
			{"id": "200", "subnets":[{"id": "2200", "ips": ["10.10.200.20"], "is_public": false}], "zone": "dal09"}]
		}`}
	cm4 := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unavailablevlanips",
			Namespace: k8sNamespace,
		},
		Data: testData,
	}
	testDataOnlyErrors := map[string]string{
		"vlanipmap.json": `
		{
		"vlan_errors":[
			{"id":"1502181","subnets":[
				{"is_public":false,"is_byoip":false,"error_reason_code":"ErrorSubnetLimitReached","error_message":"There are already the maximum number of subnets permitted in this VLAN","status":"Failed to create subnet"}], "zone":"mex01","region":"us-south"},
			{"id":"1502179","subnets":[
				{"is_public":true,"is_byoip":false,"error_reason_code":"ErrorSubnetLimitReached","error_message":"There are already the maximum number of subnets permitted in this VLAN","status":"Failed to create subnet"},
				{"is_public":true,"is_byoip":false,"error_reason_code":"ErrorSubnetLimitReached","error_message":"There are already the maximum number of subnets permitted in this VLAN","status":"Failed to create subnet"},
				{"is_public":true,"is_byoip":false,"error_reason_code":"ErrorSoftlayerDown","error_message":"Softlayer is experiencing issues please try ordering your subnet later","status":"Failed to create subnet"}], "zone":"mex01","region":"us-south"}]
		}`}
	cm5 := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "errorlanips",
			Namespace: k8sNamespace,
		},
		Data: testDataOnlyErrors,
	}
	return cm1, cm2, cm3_1, cm3_2, cm4, cm5
}

func createTestCalicoCMandSecret() (*v1.ConfigMap, *v1.Secret) {
	testData := map[string]string{
		"etcd_host": "1.2.3.4",
		"etcd_port": "1111",
	}
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-info",
			Namespace: k8sNamespace,
		},
		Data: testData,
	}

	secretData := map[string][]byte{
		"etcd-ca":   []byte("somecabytes"),
		"etcd-cert": []byte("somecertbytes"),
		"etcd-key":  []byte("somekeybytes"),
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "calico-etcd-secrets",
			Namespace: k8sNamespace,
		},
		Data: secretData,
	}

	return cm, secret
}

func createTestNodePortService(serviceName string) *v1.Service {
	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: testNamespace,
			UID:       types.UID(serviceName),
			SelfLink:  "/apis/apps/v1/namespaces/" + lbDeploymentNamespace + "/services/" + serviceName,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeNodePort,
			Ports: []v1.ServicePort{{
				Port:     80,
				Protocol: v1.ProtocolTCP,
			}},
		},
	}
	return s
}

func createTestLoadBalancerService(serviceName, serviceIP string, localOnlyTraffic bool, publicLBService bool) *v1.Service {
	var trafficPolicy v1.ServiceExternalTrafficPolicyType
	var lbAnnotations = map[string]string{}

	if localOnlyTraffic {
		trafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	} else {
		trafficPolicy = v1.ServiceExternalTrafficPolicyTypeCluster
	}

	if publicLBService {
		lbAnnotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = fmt.Sprintf("%v", PublicIP)
	} else {
		lbAnnotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = fmt.Sprintf("%v", PrivateIP)
	}

	s := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Namespace:   testNamespace,
			UID:         types.UID(serviceName),
			SelfLink:    "/apis/apps/v1/namespaces/" + lbDeploymentNamespace + "/services/" + serviceName,
			Annotations: lbAnnotations,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{{
				Port:     80,
				Protocol: v1.ProtocolTCP,
			}},
			ExternalTrafficPolicy: trafficPolicy,
		},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{{IP: serviceIP}},
			},
		},
	}
	return s
}

func createTestLoadBalancerServiceIPVS(serviceName, serviceIP string, localOnlyTraffic bool, publicLBService bool) (*v1.Service, *v1.ConfigMap) {
	service := createTestLoadBalancerService(serviceName, serviceIP, localOnlyTraffic, publicLBService)
	annotationMap := map[string]string{}
	annotationMap[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures] = lbFeatureIPVS
	service.Annotations = annotationMap

	dataMap := map[string]string{}
	dataMap["externalIP"] = serviceIP
	dataMap["nodes"] = "192.168.10.5,192.168.10.6"
	dataMap["ports"] = "80:30001/TCP"

	labels := map[string]string{}
	labels[lbNameLabel] = "a" + serviceName

	ipName := lbDeploymentNamePrefix + getCloudProviderIPLabelValue(serviceIP)
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ipName,
			Namespace: lbDeploymentNamespace,
			Labels:    labels,
		},
		Data: dataMap,
	}
	return service, cm
}

func createServiceEndpoints(service *v1.Service) *v1.Endpoints {
	nodeName := "" + service.Status.LoadBalancer.Ingress[0].IP
	endpoints := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.ObjectMeta.Name,
			Namespace: service.ObjectMeta.Namespace,
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP:       "1.2.3.4",
						NodeName: &nodeName,
					},
				},
				Ports: []v1.EndpointPort{
					{
						Port: 80,
					},
				},
			},
		},
	}
	return endpoints
}

func createKeepalivedPod(service *v1.Service) *v1.Pod {
	labels := map[string]string{}
	labels[lbIPLabel] = getCloudProviderIPLabelValue(service.Status.LoadBalancer.Ingress[0].IP)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummyKeepalivedPod" + service.ObjectMeta.Name,
			Namespace: lbDeploymentNamespace,
			Labels:    labels,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "dummyContainer",
					Image: "dummyImage:1.8",
				},
			},
		},
		Status: v1.PodStatus{
			HostIP: service.Status.LoadBalancer.Ingress[0].IP,
		},
	}
	return pod
}

func createTestCloudNodes() (*v1.Node, *v1.Node, *v1.Node, *v1.Node) {
	n1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.5",
			Labels: map[string]string{lbPublicVlanLabel: "1", lbPrivateVlanLabel: "2"},
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "192.168.10.5",
				},
			},
		},
	}
	// Create node on bad vlans
	n2 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.6",
			Labels: map[string]string{lbPublicVlanLabel: "11", lbPrivateVlanLabel: "22"},
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "192.168.10.6",
				},
			},
		},
	}
	n3 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.7",
			Labels: map[string]string{lbPublicVlanLabel: "1", lbPrivateVlanLabel: "2"},
		},
	}
	n4 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.8",
			Labels: map[string]string{lbPublicVlanLabel: "4", lbPrivateVlanLabel: "5"},
		},
	}
	return n1, n2, n3, n4
}

func createTestPriorityClass() *scheduling.PriorityClass {
	pc1 := &scheduling.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: lbPriorityClassName,
		},
		Value: 500000000,
	}
	return pc1
}

func createTestServiceAccount() *v1.ServiceAccount {
	sa1 := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      lbDeploymentServiceAccountName,
			Namespace: lbDeploymentNamespace,
		},
	}
	return sa1
}

func getTestCloud() (*Cloud, string, *fake.Clientset) {
	var cc CloudConfig

	// Build fake client resources for test cloud.
	d1, rs1 := createTestLoadBalancerDeployment("test", "192-168-10-30", 2, false, false, false, "", false)
	d2, rs2 := createTestLoadBalancerDeployment("dup", "192-168-10-31", 2, false, false, false, "", false)
	d3, rs3 := createTestLoadBalancerDeployment("dup", "192-168-10-32", 2, false, false, false, "", false)
	d4, _ := createTestLoadBalancerDeployment("noreplicas", "192-168-10-33", 0, false, false, false, "", false)
	d5, rs4 := createTestLoadBalancerDeployment("testSourceIP", "192-168-10-36", 2, false, false, false, "", false)
	d6, rs5 := createTestLoadBalancerDeployment("testEdgeNodesWithNilToleration", "192-168-10-37", 2, false, true, false, edge, false)
	d7, rs6 := createTestLoadBalancerDeployment("testEdgeNodesWithEmptyToleration", "192-168-10-38", 2, true, true, false, edge, false)
	d8, rs7 := createTestLoadBalancerDeployment("testIPVSDeleteCM", "192-168-10-50", 2, false, false, true, "", false)
	d9, rs8 := createTestLoadBalancerDeployment("testIPVSDeleteCMDeploy", "192-168-10-52", 2, false, false, true, "", false)
	d10, rs9 := createTestLoadBalancerDeployment("testGatewayNodesWithNilToleration", "192-168-10-39", 2, false, true, false, gateway, false)
	d11, rs10 := createTestLoadBalancerDeployment("testGatewayNodesWithEmptyToleration", "192-168-10-40", 2, true, true, false, gateway, false)
	s1 := createTestNodePortService("np")
	// s1 := createTestNodePortService("np")
	s2 := createTestLoadBalancerService("test", "192.168.10.30", false, true)
	s3 := createTestLoadBalancerService("dup", "192.168.10.31", false, true)
	s4 := createTestLoadBalancerService("noreplicas", "192.168.10.33", false, true)
	s5 := createTestLoadBalancerService("testSourceIP", "192.168.10.36", true, true)
	s6 := createTestLoadBalancerService("testEdgeNodesWithNilToleration", "192.168.10.37", false, true)
	s7 := createTestLoadBalancerService("testEdgeNodesWithEmptyToleration", "192.168.10.38", false, true)
	s8, cm8 := createTestLoadBalancerServiceIPVS("testIPVSDeleteCM", "192.168.10.50", true, true)
	s9, cm9 := createTestLoadBalancerServiceIPVS("testIPVSDeleteCMCreate", "192.168.10.51", true, true)
	s10, cm10 := createTestLoadBalancerServiceIPVS("testIPVSDeleteCMDeploy", "192.168.10.52", true, true)
	s11 := createTestLoadBalancerService("testGatewayNodesWithNilToleration", "192.168.10.39", false, true)
	s12 := createTestLoadBalancerService("testGatewayNodesWithEmptyToleration", "192.168.10.40", false, true)
	ep1 := createServiceEndpoints(s5)
	ep2 := createServiceEndpoints(s9)
	ep3 := createServiceEndpoints(s10)
	p1 := createKeepalivedPod(s5)
	p2 := createKeepalivedPod(s9)
	p3 := createKeepalivedPod(s10)
	cm1, cm2, cm3, cm4, cm5, cm6 := createTestCloudProviderVlanIPConfigMaps()
	cm7, sec1 := createTestCalicoCMandSecret()
	n1, n2, n3, n4 := createTestCloudNodes()
	pc1 := createTestPriorityClass()
	sa1 := createTestServiceAccount()
	fakeKubeClient := fake.NewSimpleClientset(
		d1, d2, d3, d4, d5, d6, d7, d8, d9, d10, d11,
		rs1, rs2, rs3, rs4, rs5, rs6, rs7, rs8, rs9, rs10,
		s1, s2, s3, s4, s5, s6, s7, s8, s9, s10, s11, s12,
		cm1, cm2, cm3, cm4, cm5, cm6, cm7, cm8, cm9, cm10,
		n1, n2, n3, n4,
		sec1,
		pc1,
		sa1,
		ep1, ep2, ep3,
		p1, p2, p3,
	)
	fakeKubeClientV1 := fake.NewSimpleClientset()

	// Build test cloud.
	cc.Global.Version = "1.0.0"
	cc.Kubernetes.ConfigFilePaths = []string{"../test-fixtures/kubernetes/k8s-config"}
	cc.LBDeployment.Image = "registry.ng.bluemix.net/armada-master/keepalived:1328"
	cc.LBDeployment.Application = "keepalived"
	cc.LBDeployment.VlanIPConfigMap = "ibm-cloud-provider-vlan-ip-config"
	c := Cloud{
		Name:       "ibm",
		KubeClient: fakeKubeClient,
		Config:     &cc,
		Recorder:   NewCloudEventRecorderV1("ibm", fakeKubeClientV1.CoreV1().Events(lbDeploymentNamespace)),
		CloudTasks: map[string]*CloudTask{},
	}
	return &c, "test", fakeKubeClient
}

func TestLoadBalancer(t *testing.T) {
	c := &Cloud{CloudTasks: map[string]*CloudTask{}}
	cloud, ok := c.LoadBalancer()
	if !ok {
		t.Fatalf("LoadBalancer implementation missing")
	}
	if c != cloud {
		t.Fatalf("Cloud not returned")
	}
}

func TestGetCloudProviderVlanIPsRequest(t *testing.T) {
	c, _, _ := getTestCloud()
	lbServce := getLoadBalancerService("request")

	// Default request
	lbServce.Annotations = map[string]string{}
	ipType, ipRes, label, zone, vlan, err := c.getCloudProviderVlanIPsRequest(lbServce)
	if PublicIP != ipType || UnreservedIP != ipRes || lbPublicVlanLabel != label || zone != "" || vlan != "" || nil != err {
		t.Fatalf("Unexpected default cloud provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Public request
	lbServce.Annotations = map[string]string{ServiceAnnotationLoadBalancerCloudProviderIPType: "public"}
	ipType, ipRes, label, zone, vlan, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if PublicIP != ipType || UnreservedIP != ipRes || lbPublicVlanLabel != label || zone != "" || vlan != "" || nil != err {
		t.Fatalf("Unexpected public cloud provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Public request using Zone
	lbServce.Annotations = map[string]string{ServiceAnnotationLoadBalancerCloudProviderZone: "dal10"}
	ipType, ipRes, label, zone, _, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if PublicIP != ipType || UnreservedIP != ipRes || lbPublicVlanLabel != label || zone != "dal10" || nil != err {
		t.Fatalf("Unexpected public cloud provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Public request using Vlan
	lbServce.Annotations = map[string]string{ServiceAnnotationLoadBalancerCloudProviderVlan: "1"}
	ipType, ipRes, label, _, vlan, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if PublicIP != ipType || UnreservedIP != ipRes || lbPublicVlanLabel != label || vlan != "1" || nil != err {
		t.Fatalf("Unexpected public cloud provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Invalid request using Vlan
	lbServce.Annotations = map[string]string{ServiceAnnotationLoadBalancerCloudProviderVlan: "abc"}
	ipType, ipRes, label, _, vlan, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if PublicIP != ipType || UnreservedIP != ipRes || lbPublicVlanLabel != label || vlan == "" || nil != err {
		t.Fatalf("Unexpected public cloud provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Private request
	lbServce.Annotations = map[string]string{ServiceAnnotationLoadBalancerCloudProviderIPType: "private"}
	ipType, ipRes, label, zone, _, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if PrivateIP != ipType || UnreservedIP != ipRes || lbPrivateVlanLabel != label || zone != "" || nil != err {
		t.Fatalf("Unexpected private cloud provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Invalid request
	lbServce.Annotations = map[string]string{ServiceAnnotationLoadBalancerCloudProviderIPType: "invalid"}
	ipType, ipRes, label, zone, _, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if 0 != len(ipType) || 0 != len(ipRes) || 0 != len(label) || 0 != len(zone) || nil == err {
		t.Fatalf("Unexpected invalid provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Public ingress request
	lbServce.Annotations = map[string]string{ServiceAnnotationIngressControllerPublic: "1.2.3.4"}
	ipType, ipRes, label, zone, _, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if PublicIP != ipType || ReservedIP != ipRes || lbPublicVlanLabel != label || zone != "" || nil != err {
		t.Fatalf("Unexpected public ingress cloud provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Private ingress request
	lbServce.Annotations = map[string]string{ServiceAnnotationIngressControllerPrivate: "10.20.30.40"}
	ipType, ipRes, label, _, _, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if PrivateIP != ipType || ReservedIP != ipRes || lbPrivateVlanLabel != label || zone != "" || nil != err {
		t.Fatalf("Unexpected private ingress cloud provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}

	// Conflicting request
	lbServce.Annotations = map[string]string{
		ServiceAnnotationLoadBalancerCloudProviderIPType: "public",
		ServiceAnnotationIngressControllerPublic:         "1.2.3.4",
	}
	ipType, ipRes, label, _, _, err = c.getCloudProviderVlanIPsRequest(lbServce)
	if 0 != len(ipType) || 0 != len(ipRes) || 0 != len(label) || nil == err {
		t.Fatalf("Unexpected conflicting provider VLAN IPs request: %v %v %v %v", ipType, ipRes, label, err)
	}
}

func TestGetCloudProviderIPLabelValue(t *testing.T) {
	labelValue := getCloudProviderIPLabelValue("192.168.10.20")
	if 0 != strings.Compare("192-168-10-20", labelValue) {
		t.Fatalf("Unexpected cloud provider IP label value: %v", labelValue)
	}
}

func TestGetLoadBalancerDeploymentName(t *testing.T) {
	lbDeploymentName := getLoadBalancerDeploymentName("192.168.10.20")
	if 0 != strings.Compare(lbIPLabel+"-192-168-10-20", lbDeploymentName) {
		t.Fatalf("Unexpected load balancer deployment name: %v", lbDeploymentName)
	}
}

func TestGetLoadBalancerStatus(t *testing.T) {
	lbStatus := getLoadBalancerStatus("192.168.10.20")
	if "192.168.10.20" != lbStatus.Ingress[0].IP {
		t.Fatalf("Unexpected load balancer status: %v", lbStatus)
	}
}

func TestGetSelectorCloudProviderIP(t *testing.T) {
	cloudProviderIP := "192.168.10.20"
	lbDeploymentLabels := map[string]string{
		lbIPLabel: getCloudProviderIPLabelValue(cloudProviderIP),
	}
	lbDeployment := &apps.Deployment{
		Spec: apps.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: lbDeploymentLabels,
			},
		},
	}
	emptyLbDeployment := &apps.Deployment{}

	lbDeploymentCloudProviderIP := getSelectorCloudProviderIP(lbDeployment.Spec.Selector)
	if 0 != strings.Compare(cloudProviderIP, lbDeploymentCloudProviderIP) {
		t.Fatalf("Unexpected load balancer deployment cloud provider IP: %v", lbDeploymentCloudProviderIP)
	}

	delete(lbDeployment.Spec.Selector.MatchLabels, lbIPLabel)
	lbDeploymentCloudProviderIP = getSelectorCloudProviderIP(lbDeployment.Spec.Selector)
	if 0 != len(lbDeploymentCloudProviderIP) {
		t.Fatalf("Unexpected load balancer deployment cloud provider IP: %v", lbDeploymentCloudProviderIP)
	}

	lbDeploymentCloudProviderIP = getSelectorCloudProviderIP(emptyLbDeployment.Spec.Selector)
	if 0 != len(lbDeploymentCloudProviderIP) {
		t.Fatalf("Unexpected load balancer deployment cloud provider IP: %v", lbDeploymentCloudProviderIP)
	}
}

func TestGetLoadBalancerLogName(t *testing.T) {
	lbLogName := getLoadBalancerLogName("foo", "192.168.10.20")
	if 0 != strings.Compare("(name: foo, IP: 192.168.10.20)", lbLogName) {
		t.Fatalf("Unexpected load balancer log name: %v", lbLogName)
	}
}

func TestGetCloudProviderVlanIPConfig(t *testing.T) {
	var err error
	var config *cloudProviderVlanIPConfig

	c, _, fakeKubeClient := getTestCloud()

	// Config doesn't exist
	c.Config.LBDeployment.VlanIPConfigMap = "doesntexist"
	config, err = c.getCloudProviderVlanIPConfig()
	if nil != config || nil == err {
		t.Fatalf("Unexpected cloud provider VLAN IP config 'doesntexist' found: %v, %v", config, err)
	}

	// Error parsing Config
	c.Config.LBDeployment.VlanIPConfigMap = "nodata"
	config, err = c.getCloudProviderVlanIPConfig()
	if nil != config || nil == err {
		t.Fatalf("Unexpected parse of cloud provider VLAN IP config 'nodata': %v, %v", config, err)
	}

	// Get config from load balancer deployment namespace
	c.Config.LBDeployment.VlanIPConfigMap = "ibm-cloud-provider-vlan-ip-config"
	config, err = c.getCloudProviderVlanIPConfig()
	if nil == config || nil != err {
		t.Fatalf("Unexpected error getting cloud provider VLAN IP config 'ibm-cloud-provider-vlan-ip-config': %v, %v", config, err)
	}

	// Get config from kubernetes namespace
	c.Config.LBDeployment.VlanIPConfigMap = "ibm-cloud-provider-vlan-ip-config-ibm-namespace"
	config, err = c.getCloudProviderVlanIPConfig()
	if nil == config || nil != err {
		t.Fatalf("Unexpected error getting cloud provider VLAN IP config 'ibm-cloud-provider-vlan-ip-config-ibm-namespace': %v, %v", config, err)
	}

	// Get config fails with k8s API error.
	fakeKubeClient.PrependReactor("get", "configmaps", func(action core.Action) (bool, runtime.Object, error) {
		return true, &v1.ConfigMap{}, &errors.StatusError{
			ErrStatus: metav1.Status{Reason: metav1.StatusReasonUnauthorized},
		}
	})
	config, err = c.getCloudProviderVlanIPConfig()
	if nil != config || nil == err {
		t.Fatalf("Unexpected cloud provider VLAN IP config 'ibm-cloud-provider-vlan-ip-config' found: %v, %v", config, err)
	}
}

func verifyPopulateAvailableCloudProviderVlanIPConfig(
	t *testing.T, c *Cloud,
	cloudProviderIPType CloudProviderIPType,
	cloudProviderIPReservation CloudProviderIPReservation,
	expectedCloudProviderVLANs map[string][]string,
	expectedCloudProviderIPs map[string]string,
	cloudProviderZone string,
	cloudProviderVlan string) {

	c.Config.LBDeployment.VlanIPConfigMap = "ibm-cloud-provider-vlan-ip-config"
	availableCloudProviderVLANs := map[string][]string{}
	availableCloudProviderIPs := map[string]string{}
	availableCloudProviderVlanErrors := map[string][]subnetConfigErrorField{}
	err := c.populateAvailableCloudProviderVlanIPConfig(
		availableCloudProviderVLANs,
		availableCloudProviderIPs,
		availableCloudProviderVlanErrors,
		cloudProviderIPType,
		cloudProviderIPReservation,
		cloudProviderZone,
		cloudProviderVlan,
	)
	if nil != err {
		t.Fatalf("Unexpected error getting %v %v cloud provider VLAN IP config: %v", cloudProviderIPType, cloudProviderIPReservation, err)
	}
	if !reflect.DeepEqual(expectedCloudProviderVLANs, availableCloudProviderVLANs) {
		t.Fatalf("Unexpected available %v %v cloud provider VLANs: %v", cloudProviderIPType, cloudProviderIPReservation, availableCloudProviderVLANs)
	}
	if !reflect.DeepEqual(expectedCloudProviderIPs, availableCloudProviderIPs) {
		t.Fatalf("Unexpected available %v %v cloud provider IPs: %v", cloudProviderIPType, cloudProviderIPReservation, availableCloudProviderIPs)
	}
}

func verifyLoadBalancerEdgeDeployment(t *testing.T, c *Cloud, clusterName string, lbService *v1.Service, serviceName string, expectedKeys map[string]string) {
	var err error
	var status *v1.LoadBalancerStatus
	var d *apps.Deployment

	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer '%v' created: %v, %v", serviceName, status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName(serviceName))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer '%v': %v, %v", serviceName, d, err)
	}
	// Verify the load balancer deployment toleration
	dSpecTemplateTolerations := d.Spec.Template.Spec.Tolerations
	if len(dSpecTemplateTolerations) < 1 {
		t.Fatalf("Unexpected length of load balancer Tolerations for '%v'. Expected at least 1 for edge toleration", d.Name)
	}
	containsEdgeToleration := false
	for _, toleration := range dSpecTemplateTolerations {
		if toleration.Key == lbDedicatedLabel && lbEdgeNodeValue == toleration.Value {
			containsEdgeToleration = true
		}
	}
	if !containsEdgeToleration {
		t.Fatalf("Expected load balancer deployment to contain edge tolerations '%v'", d.Name)
	}

	// Verify the load balancer deployment affinity
	dSpecTemplateAffinity := d.Spec.Template.Spec.Affinity
	if nil == dSpecTemplateAffinity {
		t.Fatalf("Unexpected affinity for load balancer '%v': %v", serviceName, dSpecTemplateAffinity)
	}
	nodeAffinity := dSpecTemplateAffinity.NodeAffinity
	if nil == nodeAffinity {
		t.Fatalf("Unexpected node affinity for load balancer '%v': %v", serviceName, nodeAffinity)
	}
	if nil == nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
		t.Fatalf("Unexpected node affinity for load balancer '%v': %v", serviceName, nodeAffinity)
	}
	if 1 != len(nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) {
		t.Fatalf("Unexpected node affinity for load balancer '%v': %v", serviceName, nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
	}
	if len(expectedKeys) != len(nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions) {
		t.Fatalf("Unexpected node affinity for load balancer '%v': %v.  Expected: %v", serviceName,
			nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, expectedKeys)
	}
	nodeSelectorReq := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	for _, nodeSelector := range nodeSelectorReq {
		if _, exist := expectedKeys[nodeSelector.Key]; !exist {
			t.Fatalf("Unexpected node affinifty for load balancer '%v': %v.  Expected: %v", serviceName, nodeSelectorReq, expectedKeys)
		}
		if expectedKeys[nodeSelector.Key] != nodeSelector.Values[0] {
			t.Fatalf("Unexpected node affinifty for load balancer '%v': %v.  Expected: %v", serviceName, nodeSelectorReq, expectedKeys)
		}
	}
	if d.Spec.MinReadySeconds != 90 {
		t.Fatalf("Unexpected min ready seconds for load balancer 'new': %v", d.Spec.MinReadySeconds)
	}
}

func verifyLoadBalancerGatewayDeployment(t *testing.T, c *Cloud, clusterName string, lbService *v1.Service, serviceName string, expectedKeys map[string]string) {
	var err error
	var status *v1.LoadBalancerStatus
	var d *apps.Deployment

	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer '%v' created: %v, %v", serviceName, status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName(serviceName))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer '%v': %v, %v", serviceName, d, err)
	}
	// Verify the load balancer deployment toleration
	dSpecTemplateTolerations := d.Spec.Template.Spec.Tolerations
	if len(dSpecTemplateTolerations) < 1 {
		t.Fatalf("Unexpected length of load balancer Tolerations for '%v'. Expected at least 1 for edge toleration", d.Name)
	}
	containsGatewayToleration := false
	for _, toleration := range dSpecTemplateTolerations {
		if toleration.Key == lbDedicatedLabel && lbGatewayNodeValue == toleration.Value {
			containsGatewayToleration = true
		}
	}
	if !containsGatewayToleration {
		t.Fatalf("Expected load balancer deployment to contain edge tolerations '%v'", d.Name)
	}

	// Verify the load balancer deployment affinity
	dSpecTemplateAffinity := d.Spec.Template.Spec.Affinity
	if nil == dSpecTemplateAffinity {
		t.Fatalf("Unexpected affinity for load balancer '%v': %v", serviceName, dSpecTemplateAffinity)
	}
	nodeAffinity := dSpecTemplateAffinity.NodeAffinity
	if nil == nodeAffinity {
		t.Fatalf("Unexpected node affinity for load balancer '%v': %v", serviceName, nodeAffinity)
	}
	if nil == nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
		t.Fatalf("Unexpected node affinity for load balancer '%v': %v", serviceName, nodeAffinity)
	}
	if 1 != len(nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) {
		t.Fatalf("Unexpected node affinity for load balancer '%v': %v", serviceName, nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
	}
	if len(expectedKeys) != len(nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions) {
		t.Fatalf("Unexpected node affinity for load balancer '%v': %v.  Expected: %v", serviceName,
			nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, expectedKeys)
	}
	nodeSelectorReq := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	for _, nodeSelector := range nodeSelectorReq {
		if _, exist := expectedKeys[nodeSelector.Key]; !exist {
			t.Fatalf("Unexpected node affinifty for load balancer '%v': %v.  Expected: %v", serviceName, nodeSelectorReq, expectedKeys)
		}
		if expectedKeys[nodeSelector.Key] != nodeSelector.Values[0] {
			t.Fatalf("Unexpected node affinifty for load balancer '%v': %v.  Expected: %v", serviceName, nodeSelectorReq, expectedKeys)
		}
	}
	if d.Spec.MinReadySeconds != 90 {
		t.Fatalf("Unexpected min ready seconds for load balancer 'new': %v", d.Spec.MinReadySeconds)
	}
}

func verifyLoadBalancerSourceIPIPVS(t *testing.T, c *Cloud, clusterName string, lbService *v1.Service, serviceName string, numNodes int) {
	var err error
	var status *v1.LoadBalancerStatus
	var d *apps.Deployment

	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer '%v' created: %v, %v", serviceName, status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName(serviceName))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer '%v': %v, %v", serviceName, d, err)
	}

	// Verify the load balancer deployment affinity
	dSpecTemplateAffinity := d.Spec.Template.Spec.Affinity
	if dSpecTemplateAffinity == nil {
		t.Fatalf("Unexpected affinity for load balancer '%v': %v", serviceName, dSpecTemplateAffinity)
	} else if d.Spec.Template.Spec.Affinity.PodAffinity != nil {
		t.Fatalf("Unexpected Pod affinity for load balancer '%v': %v", serviceName, dSpecTemplateAffinity)
	}

	// If we have fewer than 2 nodes we should have maxUnavailable=100%
	if numNodes < 2 && 0 != strings.Compare("100%", d.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal) {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new': %v", d.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}
	if numNodes < 2 && d.Spec.Strategy.RollingUpdate.MaxUnavailable.IntVal == 1 {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new': %v", d.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}
	if numNodes < 2 && 0 == strings.Compare("1", d.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal) {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new': %v", d.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}

	if nil == d.Spec.Strategy.RollingUpdate.MaxUnavailable {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new'")
	}
	if lbService.Spec.ExternalTrafficPolicy != v1.ServiceExternalTrafficPolicyTypeLocal {
		t.Fatalf("Unexpected ExternalTrafficPolicy for load balancer service '%v': %v", serviceName, dSpecTemplateAffinity)
	}
	if d.Spec.MinReadySeconds != 90 {
		t.Fatalf("Unexpected min ready seconds for load balancer 'new': %v", d.Spec.MinReadySeconds)
	}
}

func verifyLoadBalancerSourceIP(t *testing.T, c *Cloud, clusterName string, lbService *v1.Service, serviceName string, expectedKeys map[string]string) {
	var err error
	var status *v1.LoadBalancerStatus
	var d *apps.Deployment

	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer '%v' created: %v, %v", serviceName, status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName(serviceName))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer '%v': %v, %v", serviceName, d, err)
	}

	// Verify the load balancer deployment affinity
	dSpecTemplateAffinity := d.Spec.Template.Spec.Affinity
	if dSpecTemplateAffinity == nil {
		t.Fatalf("Unexpected affinity for load balancer '%v': %v", serviceName, dSpecTemplateAffinity)
	} else if d.Spec.Template.Spec.Affinity.PodAffinity == nil {
		t.Fatalf("Unexpected Pod affinity for load balancer '%v': %v", serviceName, dSpecTemplateAffinity)
	}
	lbPodAffinity := d.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels

	if len(expectedKeys) != len(lbPodAffinity) {
		t.Fatalf("Unexpected Difference in Pod Affinety Terms - Expected '%v': Actual, %v", expectedKeys, lbPodAffinity)
	}
	if nil == d.Spec.Strategy.RollingUpdate.MaxUnavailable {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new'")
	}
	if 0 != strings.Compare("100%", d.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal) {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new': %v", d.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}

	// Compare length of Affinity Match Labels between the LB Deployment and the expected keys
	if len(expectedKeys) != len(lbPodAffinity) {
		t.Fatalf("Expected pod affinifty for load balancer to be the same length as the expected keys - Actual '%v' Expected: %v",
			lbPodAffinity,
			expectedKeys)
	}

	// Compare Affinity Match Labels between the LB Deployment and the expected keys
	for expectedAffinityKey, expectedAffinityValue := range expectedKeys {

		if _, exist := lbPodAffinity[expectedAffinityKey]; !exist {
			t.Fatalf("Expected pod affinifty for load balancer '%v' Expected: %v=%v",
				serviceName,
				expectedAffinityKey,
				expectedAffinityValue)
		}

		if lbPodAffinity[expectedAffinityKey] != expectedAffinityValue {
			t.Fatalf("Unexpected pod affinifty for load balancer '%v' Actual: %v=%v.  Expected: %v=%v",
				serviceName,
				expectedAffinityKey,
				lbPodAffinity[expectedAffinityKey],
				expectedAffinityKey,
				expectedAffinityValue)
		}
	}

	if lbService.Spec.ExternalTrafficPolicy != v1.ServiceExternalTrafficPolicyTypeLocal {
		t.Fatalf("Unexpected ExternalTrafficPolicy for load balancer service '%v': %v", serviceName, dSpecTemplateAffinity)
	}
	if d.Spec.MinReadySeconds != 90 {
		t.Fatalf("Unexpected min ready seconds for load balancer 'new': %v", d.Spec.MinReadySeconds)
	}
}

func verifyLoadBalancerSourceIPRemoved(t *testing.T, c *Cloud, clusterName string, lbService *v1.Service, serviceName string, expectedKeys map[string]string, numNodes int) {
	var err error
	var status *v1.LoadBalancerStatus
	var d *apps.Deployment

	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer '%v' created: %v, %v", serviceName, status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName(serviceName))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer '%v': %v, %v", serviceName, d, err)
	}
	if numNodes > 2 && (nil == d.Spec.Strategy.RollingUpdate.MaxUnavailable || 0 == strings.Compare("100%", d.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal)) {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new': %v", d.Spec.Strategy.RollingUpdate.MaxUnavailable)
	} else if nil == d.Spec.Strategy.RollingUpdate.MaxUnavailable || 0 == strings.Compare("1", d.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal) {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new': %v", d.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}

	if d.Spec.Template.Spec.Affinity != nil {
		// Verify the load balancer deployment affinity
		if d.Spec.Template.Spec.Affinity.PodAffinity != nil {
			if numNodes > 2 && len(d.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution) != 0 {
				t.Fatalf("Unexpected Pod affinity for load balancer '%v': %v", serviceName, d.Spec.Template.Spec.Affinity.PodAffinity)
			} else {
				lbPodAffinity := d.Spec.Template.Spec.Affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].LabelSelector.MatchLabels
				if len(expectedKeys) != len(lbPodAffinity) {
					t.Fatalf("Unexpected Difference in Pod Affinety Terms - Expected '%v': Actual, %v", expectedKeys, lbPodAffinity)
				}
			}
		}
	}

	if lbService.Spec.ExternalTrafficPolicy != v1.ServiceExternalTrafficPolicyTypeCluster {
		t.Fatalf("Unexpected ExternalTrafficPolicy for load balancer service '%v': %v", serviceName, d.Spec.Template.Spec.Affinity.PodAffinity)
	}
	if d.Spec.MinReadySeconds != 90 {
		t.Fatalf("Unexpected min ready seconds for load balancer 'new': %v", d.Spec.MinReadySeconds)
	}
}

func getIPVSService(localOnlyTraffic bool, privateNlb bool) *v1.Service {
	svc := getLoadBalancerService("ipvs")
	annotationMap := map[string]string{}
	annotationMap[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures] = lbFeatureIPVS
	if privateNlb {
		annotationMap[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	}
	svc.Annotations = annotationMap

	if localOnlyTraffic {
		svc.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	} else {
		svc.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeCluster
	}
	svc.Spec.LoadBalancerIP = "1.2.3.4"
	svc.Spec.HealthCheckNodePort = 30001
	svc.Spec.Ports = []v1.ServicePort{
		{
			Protocol: v1.ProtocolTCP,
			Port:     1020,
			NodePort: 30003,
		},
	}
	return svc
}

func TestCreateIPVSConfigMap(t *testing.T) {
	var err error

	c, _, _ := getTestCloud()
	svc := getIPVSService(true, false)
	annotationMap := map[string]string{}
	annotationMap[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures] = lbFeatureIPVS
	svc.Annotations = annotationMap

	svc.Spec.LoadBalancerIP = "1.2.3.4"
	svc.Spec.HealthCheckNodePort = 30001
	svc.Spec.Ports = []v1.ServicePort{
		{
			Protocol: v1.ProtocolTCP,
			Port:     1020,
			NodePort: 30003,
		},
	}

	nodes := []*v1.Node{
		{
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: "10.2.0.1",
					},
				},
			},
		},
		{
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: "10.2.0.2",
					},
				},
			},
		},
	}
	localCM, err := c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, nodes)
	if err != nil {
		t.Fatalf("Error generating IPVS configmap %v", err)
	}
	cm, err := c.createIPVSConfigMap(localCM)
	if err != nil {
		t.Fatalf("Error creating IPVS configmap %v", err)
	}

	if cm.Data["externalIP"] != "1.2.3.4" {
		t.Fatalf("externalIP is not correct.  expected %v, actual %v", "1.2.3.4", cm.Data["externalIP"])
	}
	if cm.Data["healthCheckNodePort"] != "30001" {
		t.Fatalf("healthCheckNodePort is not correct.  expected %v, actual %v", "30001", cm.Data["healthCheckNodePort"])
	}
	if cm.Data["nodes"] != "10.2.0.1,10.2.0.2" {
		t.Fatalf("nodes is not correct.  expected %v, actual %v", "10.2.0.1,10.2.0.2", cm.Data["nodes"])
	}
	if cm.Data["ports"] != "1020:30003/TCP" {
		t.Fatalf("ports is not correct.  expected %v, actual %v", "1020:30003/TCP", cm.Data["ports"])
	}
	if cm.Name != "ibm-cloud-provider-ip-1-2-3-4" {
		t.Fatalf("name is not correct.  expected %v, actual %v", "ibm-cloud-provider-ip-1-2-3-4", cm.Name)
	}
	if cm.Data["externalTrafficPolicy"] == "" {
		t.Fatalf("externalTrafficPolicy is not correct.  did not expect ''")
	}
	// Test scheduler key doesn't exist if scheduler isn't set
	if _, okay := cm.Data["scheduler"]; okay {
		t.Fatalf("scheduler is not correct.  expected 'rr', actual %v", cm.Data["scheduler"])
	}

	svc.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	svc.Spec.Ports = []v1.ServicePort{
		{
			Protocol: v1.ProtocolTCP,
			Port:     1020,
			NodePort: 30003,
		},
		{
			Protocol: v1.ProtocolUDP,
			Port:     1022,
			NodePort: 30004,
		},
	}

	localCM, err = c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, nodes)
	if err != nil {
		t.Fatalf("Error generating IPVS configmap %v", err)
	}
	cm, err = c.createIPVSConfigMap(localCM)
	if err != nil {
		t.Fatalf("Error creating IPVS configmap %v", err)
	}
	if cm.Data["ports"] != "1020:30003/TCP,1022:30004/UDP" {
		t.Fatalf("ports is not correct.  expected %v, actual %v", "1020:30003/TCP,1022:30004/UDP", cm.Data["ports"])
	}
	if cm.Data["externalTrafficPolicy"] != string(v1.ServiceExternalTrafficPolicyTypeLocal) {
		t.Fatalf("externalTrafficPolicy is not correct.  expected %v, actual %v", v1.ServiceExternalTrafficPolicyTypeLocal, cm.Data["externalTrafficPolicy"])
	}

	// Test modifying the service updates the CM correctly
	annotationMap[ServiceAnnotationLoadBalancerCloudProviderIPVSSchedulingAlgorithm] = "sh"
	svc.Annotations = annotationMap
	localCM, err = c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, nodes)
	if err != nil {
		t.Fatalf("Error generating IPVS configmap %v", err)
	}
	cm, err = c.createIPVSConfigMap(localCM)
	if err != nil {
		t.Fatalf("Error creating IPVS configmap %v", err)
	}
	if cm.Data["scheduler"] != "sh" {
		t.Fatalf("scheduler is not correct.  expected 'sh', actual %v", cm.Data["scheduler"])
	}

	// Test using an usuported scheduler throws an error
	annotationMap[ServiceAnnotationLoadBalancerCloudProviderIPVSSchedulingAlgorithm] = "unsuportedScheduler"
	svc.Annotations = annotationMap
	localCM, err = c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, nodes)
	if err == nil {
		t.Fatalf("Expected Error generating IPVS configmap %v", err)
	}
	if localCM != nil {
		t.Fatalf("CM expected to be nil. Actual %v", cm)
	}
}

func TestIsIPVSConfigMapEqual(t *testing.T) {
	var err error

	c, _, _ := getTestCloud()
	svc := getIPVSService(true, false)
	svc.Spec.LoadBalancerIP = "1.2.3.4"
	svc.Spec.HealthCheckNodePort = 30001
	svc.Spec.Ports = []v1.ServicePort{
		{
			Protocol: v1.ProtocolTCP,
			Port:     1020,
			NodePort: 30003,
		},
	}

	nodes := []*v1.Node{
		{
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: "10.2.0.1",
					},
				},
			},
		},
		{
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: "10.2.0.2",
					},
				},
			},
		},
	}
	leftCM, err := c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, nodes)
	if err != nil {
		t.Fatalf("Error generating IPVS configmap %v", err)
	}
	rightCM, err := c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, nodes)
	if err != nil {
		t.Fatalf("Error generating IPVS configmap %v", err)
	}

	if !c.isIPVSConfigMapEqual(leftCM, rightCM) {
		t.Fatal("IPVS configmaps not equal and should be")
	}

	newPort := v1.ServicePort{
		Protocol: v1.ProtocolTCP,
		Port:     1030,
		NodePort: 30005,
	}
	svc.Spec.Ports = append(svc.Spec.Ports, newPort)
	newRightCM, err := c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, nodes)
	if err != nil {
		t.Fatalf("Error generating IPVS configmap %v", err)
	}

	if c.isIPVSConfigMapEqual(leftCM, newRightCM) {
		t.Fatal("IPVS configmaps equal and should not be")
	}

	newNode1 := v1.Node{
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "10.2.0.11",
				},
			},
		},
	}

	newNode2 := v1.Node{
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "10.2.0.22",
				},
			},
		},
	}

	newNodeList1 := nodes
	newNodeList1 = append(newNodeList1, &newNode1, &newNode2)
	newNodeList2 := nodes
	newNodeList2 = append(newNodeList2, &newNode2, &newNode1)
	leftCM, err = c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, newNodeList1)
	if err != nil {
		t.Fatalf("Error generating IPVS configmap %v", err)
	}
	rightCM, err = c.createIPVSConfigMapStruct(svc, svc.Spec.LoadBalancerIP, newNodeList2)
	if err != nil {
		t.Fatalf("Error generating IPVS configmap %v", err)
	}

	if !c.isIPVSConfigMapEqual(leftCM, rightCM) {
		t.Fatal("IPVS configmaps not equal and should be")
	}
}

func TestCreateCalicoCfg(t *testing.T) {
	c, _, _ := getTestCloud()
	calicoCtlCfgFile, err := c.createCalicoCfg()
	if err != nil {
		t.Fatalf("Error while calling createCalicoCfg() %v", err)
	}
	calicoCtlCfgBytes, err := ioutil.ReadFile(calicoCtlCfgFile)
	if err != nil {
		t.Fatalf("Could not read calicoctl config file: %v. error is: %v", calicoCtlCfgFile, err)
	}
	calicoCtlCfgLines := strings.Split(string(calicoCtlCfgBytes), "\n")
	if len(calicoCtlCfgLines) != 8 {
		t.Fatalf("calicoctl config is not 8 lines.  config content: %v", string(calicoCtlCfgBytes))
	}
	if calicoCtlCfgLines[4] != "  etcdEndpoints: https://1.2.3.4:1111" {
		t.Fatalf("calicoctl config etcdEndpoints key is not correct.  config content: %v", string(calicoCtlCfgBytes))
	}
	if !strings.HasPrefix(calicoCtlCfgLines[5], "  etcdKeyFile: ") {
		t.Fatalf("calicoctl config etcdKeyFile key is not correct.  config content: %v", string(calicoCtlCfgBytes))
	}
	colonIndex := strings.Index(calicoCtlCfgLines[5], ": ") + 2
	keyFileName := calicoCtlCfgLines[5][colonIndex:]
	keyFileBytes, err := ioutil.ReadFile(keyFileName)
	if err != nil {
		t.Fatalf("Could not read calicoctl key file: %v. error is: %v", keyFileName, err)
	}
	if string(keyFileBytes) != "somekeybytes" {
		t.Fatalf("calicoctl key file contents invalid: %v.", string(keyFileBytes))
	}

	if !strings.HasPrefix(calicoCtlCfgLines[6], "  etcdCertFile: ") {
		t.Fatalf("calicoctl config etcdKeyFile key is not correct.  config content: %v", string(calicoCtlCfgBytes))
	}
	colonIndex = strings.Index(calicoCtlCfgLines[6], ": ") + 2
	certFileName := calicoCtlCfgLines[6][colonIndex:]
	certFileBytes, err := ioutil.ReadFile(certFileName)
	if err != nil {
		t.Fatalf("Could not read calicoctl cert file: %v. error is: %v", certFileName, err)
	}
	if string(certFileBytes) != "somecertbytes" {
		t.Fatalf("calicoctl cert file contents invalid: %v.", string(certFileBytes))
	}

	if !strings.HasPrefix(calicoCtlCfgLines[7], "  etcdCACertFile: ") {
		t.Fatalf("calicoctl config etcdCACertFile key is not correct.  config content: %v", string(calicoCtlCfgBytes))
	}
	colonIndex = strings.Index(calicoCtlCfgLines[7], ": ") + 2
	caCertFileName := calicoCtlCfgLines[7][colonIndex:]
	caCertFileBytes, err := ioutil.ReadFile(caCertFileName)
	if err != nil {
		t.Fatalf("Could not read calicoctl CA cert file: %v. error is: %v", caCertFileName, err)
	}
	if string(caCertFileBytes) != "somecabytes" {
		t.Fatalf("calicoctl cert file contents invalid: %v.", string(caCertFileBytes))
	}
}

func TestCreateCalicoKDDCfg(t *testing.T) {
	c, _, _ := getTestCloud()
	c.Config.Kubernetes.CalicoDatastore = "KDD"

	calicoCfgFile, err := c.createCalicoCfg()
	if err != nil {
		t.Fatalf("Error while calling createCalicoCfg() %v", err)
	}

	actualCalicoCfg, err := ioutil.ReadFile(calicoCfgFile)
	if err != nil {
		t.Fatalf("Could not read created calicoctl config file: %v. error is: %v", calicoCfgFile, err)
	}

	expectedCalicoCfg, _ := ioutil.ReadFile("../test-fixtures/kdd-calico-config.yaml")
	if string(actualCalicoCfg) != strings.TrimSpace(string(expectedCalicoCfg)) {
		t.Errorf("FAILURE: unable to generate expected yaml. expected \n%+v, actual \n%+v", string(expectedCalicoCfg), string(actualCalicoCfg))
	}

}

func TestCreateCalicoPublicIngressPolicy(t *testing.T) {
	var err error

	c, _, _ := getTestCloud()
	svc := getIPVSService(true, false)
	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCreateCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "POLICY_TYPE=PUBLIC"}
		return cmd
	}

	lbDeployment, _ := createTestLoadBalancerDeployment("TestCreateCalicoExecDummy", "192-168-10-52", 2, false, true, true, "", false)
	err = c.createCalicoIngressPolicy(svc, svc.Spec.LoadBalancerIP, c.getLoadBalancerIPTypeLabel(lbDeployment))
	if err != nil {
		t.Fatalf("createCalicoIngressPolicy call failed: %v.", err)
	}
}

func TestCreateCalicoPrivateIngressPolicy(t *testing.T) {
	var err error

	c, _, _ := getTestCloud()
	svc := getIPVSService(true, true)
	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCreateCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "POLICY_TYPE=PRIVATE"}
		return cmd
	}

	lbDeployment, _ := createTestLoadBalancerDeployment("TestCreateCalicoExecDummy", "192-168-10-52", 2, false, true, true, "", true)
	err = c.createCalicoIngressPolicy(svc, svc.Spec.LoadBalancerIP, c.getLoadBalancerIPTypeLabel(lbDeployment))
	if err != nil {
		t.Fatalf("createCalicoIngressPolicy call failed: %v.", err)
	}
}

func TestCreateCalicoExecDummy(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			//nolint staticcheck SA4006 complex loop to dump "--" from arguments
			args = args[1:]
			break
		}
		args = args[1:]
	}

	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		t.Fatalf("unable to read calico policy yaml: %v", err)
	}
	calicoPolicyYaml := string(b)
	calicoPolicyYamlLines := strings.Split(calicoPolicyYaml, "\n")
	if len(calicoPolicyYamlLines) != 17 {
		t.Fatalf("Calico policy is not 17 lines.  policy content: %v", calicoPolicyYaml)
	}
	if calicoPolicyYamlLines[3] != "  name: allow-lb-aipvs" {
		t.Fatalf("Calico policy name is not correct.  policy content: %v", calicoPolicyYaml)
	}
	if calicoPolicyYamlLines[7] != "  selector: ibm.role in { 'worker_private' }" && os.Getenv("POLICY_TYPE") == "PRIVATE" {
		t.Fatalf("Calico policy endpoint selector is not correct for private NLB.  policy content: %v", calicoPolicyYaml)
	} else if calicoPolicyYamlLines[7] != "  selector: ibm.role in { 'worker_public' }" && os.Getenv("POLICY_TYPE") == "PUBLIC" {
		t.Fatalf("Calico policy endpoint selector is not correct for public NLB.  policy content: %v", calicoPolicyYaml)
	}

	if calicoPolicyYamlLines[11] != "      protocol: TCP" {
		t.Fatalf("Calico policy protocol is not correct.  policy content: %v", calicoPolicyYaml)
	}
	if calicoPolicyYamlLines[14] != "        - 1.2.3.4/32" {
		t.Fatalf("Calico policy destination nets is not correct.  policy content: %v", calicoPolicyYaml)
	}
	if calicoPolicyYamlLines[16] != "        - 1020" {
		t.Fatalf("Calico policy destination port is not correct.  policy content: %v", calicoPolicyYaml)
	}

	os.Exit(0)
}

func TestDeleteCalicoPublicIngressPolicy(t *testing.T) {
	c, _, _ := getTestCloud()
	svc := getIPVSService(true, false)
	var policyNameParm string

	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "POLICY_TYPE=PUBLIC"}

		policyNameParm = parms[3]
		return cmd
	}

	err := c.deleteCalicoIngressPolicy(svc)
	if policyNameParm != "allow-lb-aipvs" {
		t.Fatalf("policy name parameter incorrect: %v", policyNameParm)
	}
	if err != nil {
		t.Fatalf("deleteCalicoIngressPolicy call failed: %v", err)
	}

	// Test #2: Ensure deleting the calico policy doesn't fail if it doesn't exist
	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummyWithError", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "POLICY_TYPE=PUBLIC"}

		policyNameParm = parms[3]
		return cmd
	}

	policyNameParm = ""
	err = c.deleteCalicoIngressPolicy(svc)
	if policyNameParm != "allow-lb-aipvs" {
		t.Fatalf("policy name parameter incorrect: %v", policyNameParm)
	}
	expectedError := "Error running calicoctl: resource does not exist: bla bla bla, exit status 1"
	if err.Error() != expectedError {
		t.Fatalf("Expected Error: '%s'. Actual Error: '%v'", expectedError, err)
	}
}

func TestDeleteCalicoPrivateIngressPolicy(t *testing.T) {
	c, _, _ := getTestCloud()
	svc := getIPVSService(true, false)
	var policyNameParm string

	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "POLICY_TYPE=PRIVATE"}

		policyNameParm = parms[3]
		return cmd
	}

	err := c.deleteCalicoIngressPolicy(svc)
	if policyNameParm != "allow-lb-aipvs" {
		t.Fatalf("policy name parameter incorrect: %v", policyNameParm)
	}
	if err != nil {
		t.Fatalf("deleteCalicoIngressPolicy call failed: %v", err)
	}

	// Test #2: Ensure deleting the calico policy doesn't fail if it doesn't exist
	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummyWithError", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "POLICY_TYPE=PRIVATE"}

		policyNameParm = parms[3]
		return cmd
	}

	policyNameParm = ""
	err = c.deleteCalicoIngressPolicy(svc)
	if policyNameParm != "allow-lb-aipvs" {
		t.Fatalf("policy name parameter incorrect: %v", policyNameParm)
	}
	expectedError := "Error running calicoctl: resource does not exist: bla bla bla, exit status 1"
	if err.Error() != expectedError {
		t.Fatalf("Expected Error: '%s'. Actual Error: '%v'", expectedError, err)
	}
}

func TestCalicoExecDummy(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	os.Exit(0)
}

func TestCalicoExecDummyWithError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	fmt.Fprintf(os.Stderr, "resource does not exist: bla bla bla")
	os.Exit(1)
}

func TestIsFeatureEnabled(t *testing.T) {
	svc := getLoadBalancerService("feature")
	annotationMap := map[string]string{}
	annotationMap[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures] = "feature1"
	svc.Annotations = annotationMap

	if !isFeatureEnabled(svc, "feature1") {
		t.Fatalf("isFeatureEnabled returned false when should have returned true.  Enabled features = %v", annotationMap)
	}

	annotationMap[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures] = "feature1,feature2"
	if !isFeatureEnabled(svc, "feature1") {
		t.Fatalf("isFeatureEnabled returned false for feature1 when should have returned true.  Enabled features = %v", annotationMap)
	}
	if !isFeatureEnabled(svc, "feature2") {
		t.Fatalf("isFeatureEnabled returned false for feature2 when should have returned true.  Enabled features = %v", annotationMap)
	}
	if isFeatureEnabled(svc, "notset") {
		t.Fatalf("isFeatureEnabled returned true for notset when should have returned false.  Enabled features = %v", annotationMap)
	}
}

func TestIsUpdateSourceIPRequired(t *testing.T) {
	lbDeployment, _ := createTestLoadBalancerDeployment("TestIsUpdateSourceIPRequired", "192-168-222-111", 2, false, true, true, "", false)
	s1 := createTestLoadBalancerService("TestIsUpdateSourceIPRequired1", "192.168.222.111", false, true)
	s2 := createTestLoadBalancerService("TestIsUpdateSourceIPRequired2", "192.168.222.111", true, false)

	localUpdatesRequired := isUpdateSourceIPRequired(lbDeployment, s1)
	if len(localUpdatesRequired) == 0 {
		t.Fatalf("isUpdateSourceIPRequired returned with empty slice")
	} else if localUpdatesRequired[0] != "RemoveSourceIPPodAffinity" {
		t.Fatalf("isUpdateSourceIPRequired returned with %s", localUpdatesRequired[0])
	}

	var lbDeploymentMaxSurge = intstr.FromInt(1)
	var lbDeploymentMaxUnavailable = intstr.FromString("100%")
	var lbDeploymentMaxUnavailable50 = intstr.FromString("50%")

	lbDeployment.Spec.Strategy = apps.DeploymentStrategy{
		Type: apps.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &apps.RollingUpdateDeployment{
			MaxUnavailable: &lbDeploymentMaxUnavailable,
			MaxSurge:       &lbDeploymentMaxSurge,
		},
	}

	lbDeployment.Spec.Strategy.Type = apps.RecreateDeploymentStrategyType
	localUpdatesRequired = isUpdateSourceIPRequired(lbDeployment, s2)
	if len(localUpdatesRequired) == 0 {
		t.Fatalf("isUpdateSourceIPRequired returned with empty slice")
	} else if localUpdatesRequired[0] != "UpdateSourceIPPodMaxUnavailable" {
		t.Fatalf("isUpdateSourceIPRequired returned with %s", localUpdatesRequired[0])
	}

	lbDeployment.Spec.Strategy.Type = apps.RollingUpdateDeploymentStrategyType
	lbDeployment.Spec.Strategy.RollingUpdate.MaxUnavailable = &lbDeploymentMaxUnavailable50
	localUpdatesRequired = isUpdateSourceIPRequired(lbDeployment, s2)
	if len(localUpdatesRequired) == 0 {
		t.Fatalf("isUpdateSourceIPRequired returned with empty slice")
	} else if localUpdatesRequired[0] != "UpdateSourceIPPodMaxUnavailable" {
		t.Fatalf("isUpdateSourceIPRequired returned with %s", localUpdatesRequired[0])
	}

	lbDeployment.Spec.Strategy.RollingUpdate.MaxUnavailable = nil
	localUpdatesRequired = isUpdateSourceIPRequired(lbDeployment, s2)
	if len(localUpdatesRequired) == 0 {
		t.Fatalf("isUpdateSourceIPRequired returned with empty slice")
	} else if localUpdatesRequired[0] != "UpdateSourceIPPodMaxUnavailable" {
		t.Fatalf("isUpdateSourceIPRequired returned with %s", localUpdatesRequired[0])
	}

	lbDeployment.Spec.Strategy.RollingUpdate = nil
	localUpdatesRequired = isUpdateSourceIPRequired(lbDeployment, s2)
	if len(localUpdatesRequired) == 0 {
		t.Fatalf("isUpdateSourceIPRequired returned with empty slice")
	} else if localUpdatesRequired[0] != "UpdateSourceIPPodMaxUnavailable" {
		t.Fatalf("isUpdateSourceIPRequired returned with %s", localUpdatesRequired[0])
	}

	lbDeployment.Spec.Strategy = apps.DeploymentStrategy{
		Type: apps.RollingUpdateDeploymentStrategyType,
		RollingUpdate: &apps.RollingUpdateDeployment{
			MaxUnavailable: &lbDeploymentMaxUnavailable,
			MaxSurge:       &lbDeploymentMaxSurge,
		},
	}

	lbDeployment.Spec.Template.Spec.Affinity = &v1.Affinity{}
	lbDeployment.Spec.Template.Spec.Affinity.PodAffinity = &v1.PodAffinity{}
	localUpdatesRequired = isUpdateSourceIPRequired(lbDeployment, s2)
	if len(localUpdatesRequired) == 0 {
		t.Fatalf("isUpdateSourceIPRequired returned with empty slice")
	} else if localUpdatesRequired[0] != "DifferentSourceIPAffinityWithPodAffinity" {
		t.Fatalf("isUpdateSourceIPRequired returned with %s", localUpdatesRequired[0])
	}

	s2.Spec.Selector = map[string]string{"tomIsCool": "true"}
	lbServiceLabelSelector := &metav1.LabelSelector{
		MatchLabels: s2.Spec.Selector,
	}
	lbPodAffinity := &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
			{
				LabelSelector: lbServiceLabelSelector,
				TopologyKey:   v1.LabelHostname,
				Namespaces:    []string{s2.Namespace},
			},
		},
	}
	lbDeployment.Spec.Template.Spec.Affinity.PodAffinity = lbPodAffinity
	localUpdatesRequired = isUpdateSourceIPRequired(lbDeployment, s2)
	if len(localUpdatesRequired) > 0 {
		t.Fatalf("isUpdateSourceIPRequired returned with %s", localUpdatesRequired)
	}
}

func TestPopulateAvailableCloudProviderVlanIPConfig(t *testing.T) {
	var err error
	var availableCloudProviderVLANs map[string][]string
	var availableCloudProviderIPs map[string]string
	emptyCloudProviderZone := ""
	emptyCloudProviderVlan := ""

	c, _, _ := getTestCloud()

	// Config doesn't exist
	c.Config.LBDeployment.VlanIPConfigMap = "doesntexist"
	availableCloudProviderVLANs = map[string][]string{}
	availableCloudProviderIPs = map[string]string{}
	availableCloudProviderVlanErrors := map[string][]subnetConfigErrorField{}
	err = c.populateAvailableCloudProviderVlanIPConfig(
		availableCloudProviderVLANs,
		availableCloudProviderIPs,
		availableCloudProviderVlanErrors,
		PublicIP,
		UnreservedIP,
		emptyCloudProviderZone,
		emptyCloudProviderVlan,
	)
	if nil == err {
		t.Fatalf("Unexpected cloud provider VLAN IP config 'doesntexist' found: %v", err)
	}

	// No cloud provider VLAN IPs exist
	c.Config.LBDeployment.VlanIPConfigMap = "emptydata"
	availableCloudProviderVLANs = map[string][]string{}
	availableCloudProviderIPs = map[string]string{}
	availableCloudProviderVlanErrors = map[string][]subnetConfigErrorField{}
	err = c.populateAvailableCloudProviderVlanIPConfig(
		availableCloudProviderVLANs,
		availableCloudProviderIPs,
		availableCloudProviderVlanErrors,
		PublicIP,
		UnreservedIP,
		emptyCloudProviderZone,
		emptyCloudProviderVlan,
	)
	if nil != err {
		t.Fatalf("Unexpected error getting cloud provider VLAN IP config 'emptydata': %v", err)
	}
	if 0 != len(availableCloudProviderVLANs) {
		t.Fatalf("Unexpected cloud provider VLANs found: %v", availableCloudProviderVLANs)
	}
	if 0 != len(availableCloudProviderIPs) {
		t.Fatalf("Unexpected cloud provider IPs found: %v", availableCloudProviderIPs)
	}

	// Public, unreserved cloud provider VLAN IPs exist
	expectedCloudProviderVLANs := map[string][]string{
		"1": {"192.168.10.30", "192.168.10.31", "192.168.10.32", "192.168.10.33", "192.168.10.34", "192.168.10.35", "192.168.10.36", "192.168.10.37", "192.168.10.38", "192.168.10.39", "192.168.10.50", "192.168.10.51", "192.168.10.52", "192.168.10.53"},
		"3": {"2001:db8::1"},
		"4": {"192.168.10.40", "192.168.10.41", "192.168.10.42", "192.168.10.43", "192.168.10.44", "192.168.10.45"},
	}
	expectedCloudProviderIPs := map[string]string{
		"192.168.10.30": "1", "192.168.10.31": "1", "192.168.10.32": "1", "192.168.10.33": "1", "192.168.10.34": "1", "192.168.10.35": "1", "192.168.10.36": "1", "192.168.10.37": "1", "192.168.10.38": "1", "192.168.10.39": "1",
		"192.168.10.50": "1", "192.168.10.51": "1", "192.168.10.52": "1", "192.168.10.53": "1", "192.168.10.40": "4", "192.168.10.41": "4", "192.168.10.42": "4", "192.168.10.43": "4", "192.168.10.44": "4", "192.168.10.45": "4",
	}
	verifyPopulateAvailableCloudProviderVlanIPConfig(
		t, c, PublicIP, UnreservedIP,
		expectedCloudProviderVLANs,
		expectedCloudProviderIPs,
		emptyCloudProviderZone,
		emptyCloudProviderVlan,
	)

	// Public, reserved cloud provider VLAN IPs exist
	expectedCloudProviderVLANs = map[string][]string{"1": {"192.168.10.15"}, "4": {"192.168.10.16"}}
	expectedCloudProviderIPs = map[string]string{"192.168.10.15": "1", "192.168.10.16": "4"}
	verifyPopulateAvailableCloudProviderVlanIPConfig(
		t, c, PublicIP, ReservedIP,
		expectedCloudProviderVLANs,
		expectedCloudProviderIPs,
		emptyCloudProviderZone,
		emptyCloudProviderVlan,
	)

	// Public, reserved cloud provider VLAN IPs on dal10 exist
	expectedCloudProviderVLANs = map[string][]string{"4": {"192.168.10.16"}}
	expectedCloudProviderIPs = map[string]string{"192.168.10.16": "4"}
	verifyPopulateAvailableCloudProviderVlanIPConfig(
		t, c, PublicIP, ReservedIP,
		expectedCloudProviderVLANs,
		expectedCloudProviderIPs,
		"dal10",
		emptyCloudProviderVlan,
	)

	// Private, unreserved cloud provider VLAN IPs exist
	expectedCloudProviderVLANs = map[string][]string{"2": {"10.10.10.21", "10.10.10.22"}, "5": {"10.10.10.31", "10.10.10.32"}}
	expectedCloudProviderIPs = map[string]string{"10.10.10.21": "2", "10.10.10.22": "2", "10.10.10.31": "5", "10.10.10.32": "5"}
	verifyPopulateAvailableCloudProviderVlanIPConfig(
		t, c, PrivateIP, UnreservedIP,
		expectedCloudProviderVLANs,
		expectedCloudProviderIPs,
		emptyCloudProviderZone,
		emptyCloudProviderVlan,
	)

	// Private, unreserved cloud provider VLAN IPs on dal10 exist
	expectedCloudProviderVLANs = map[string][]string{"5": {"10.10.10.31", "10.10.10.32"}}
	expectedCloudProviderIPs = map[string]string{"10.10.10.31": "5", "10.10.10.32": "5"}
	verifyPopulateAvailableCloudProviderVlanIPConfig(
		t, c, PrivateIP, UnreservedIP,
		expectedCloudProviderVLANs,
		expectedCloudProviderIPs,
		"dal10",
		emptyCloudProviderVlan,
	)

	// Private, reserved cloud provider VLAN IPs exist
	expectedCloudProviderVLANs = map[string][]string{"2": {"10.10.10.20"}, "5": {"10.10.10.30"}}
	expectedCloudProviderIPs = map[string]string{"10.10.10.20": "2", "10.10.10.30": "5"}
	verifyPopulateAvailableCloudProviderVlanIPConfig(
		t, c, PrivateIP, ReservedIP,
		expectedCloudProviderVLANs,
		expectedCloudProviderIPs,
		emptyCloudProviderZone,
		emptyCloudProviderVlan,
	)

	// Private, reserved cloud provider VLAN IPs on dal10 exist
	expectedCloudProviderVLANs = map[string][]string{"5": {"10.10.10.30"}}
	expectedCloudProviderIPs = map[string]string{"10.10.10.30": "5"}
	verifyPopulateAvailableCloudProviderVlanIPConfig(
		t, c, PrivateIP, ReservedIP,
		expectedCloudProviderVLANs,
		expectedCloudProviderIPs,
		"dal10",
		emptyCloudProviderVlan,
	)
}

func TestGetLoadBalancerDeployment(t *testing.T) {
	var err error
	var d *apps.Deployment

	c, _, _ := getTestCloud()

	// Load balancer deployment doesn't exist
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("doesntexist"))
	if nil != d || nil != err {
		t.Fatalf("Unexpected load balancer 'doesntexist' found: %v, %v", d, err)
	}

	// Single load balancer deployment exits
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("test"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'test': %v, %v", d, err)
	}

	// Duplicate load balancer deployments exist
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("dup"))
	if nil != d || nil == err {
		t.Fatalf("Unexpected load balancer 'dup' found: %v, %v", d, err)
	}
}

func TestGetLoadBalancer(t *testing.T) {
	var err error
	var status *v1.LoadBalancerStatus
	var exists bool

	c, clusterName, _ := getTestCloud()

	// Load balancer doesn't exist
	status, exists, err = c.GetLoadBalancer(context.Background(), clusterName, getLoadBalancerService("doesntexist"))
	if nil != status || exists || nil != err {
		t.Fatalf("Unexpected load balancer 'doesntexist' found: %v, %v, %v", status, exists, err)
	}

	// Single load balancer exits
	status, exists, err = c.GetLoadBalancer(context.Background(), clusterName, getLoadBalancerService("test"))
	if nil == status || !exists || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'test': %v, %v, %v", status, exists, err)
	}
	if 0 != strings.Compare("192.168.10.30", status.Ingress[0].IP) {
		t.Fatalf("Unexpected load balancer 'test' status: %v", status.Ingress[0].IP)
	}

	// Duplicate load balancers exist
	status, exists, err = c.GetLoadBalancer(context.Background(), clusterName, getLoadBalancerService("dup"))
	if nil != status || exists || nil == err {
		t.Fatalf("Unexpected load balancer 'dup' found: %v, %v, %v", status, exists, err)
	}
}

func TestEnsureLoadBalancer(t *testing.T) {
	var err error
	var status *v1.LoadBalancerStatus
	var d *apps.Deployment

	c, clusterName, _ := getTestCloud()

	// Delete nodes to ensure tests work as previously expected
	err = c.KubeClient.CoreV1().Nodes().Delete(context.TODO(), "192.168.10.7", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete the node. Error: %v", err)
	}
	err = c.KubeClient.CoreV1().Nodes().Delete(context.TODO(), "192.168.10.8", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete the node. Error: %v", err)
	}

	// used to ensure the exec for calicoctl is redirected internally
	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		return cmd
	}

	// Eat up an IP of the portable Subnet Config to allow this test to
	// continue working properly
	service, _ := createTestLoadBalancerServiceIPVS("testIPVSDeleteCMCreate", "192.168.10.51", true, true)
	service.Spec.LoadBalancerIP = "192.168.10.51"
	node1, node2, _, _ := createTestCloudNodes()
	nodes := []*v1.Node{node1, node2}
	c.EnsureLoadBalancer(context.Background(), clusterName, service, nodes)

	// Load balancer exists
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("test"), nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensuring load balancer 'test': %v, %v", status, err)
	}
	if 0 != strings.Compare("192.168.10.30", status.Ingress[0].IP) {
		t.Fatalf("Unexpected load balancer 'test' status: %v", status.Ingress[0].IP)
	}

	// Update the LB deployment keepalived Image
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("test"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'new': %v, %v", d, err)
	}
	d.Spec.Template.Spec.Containers[0].Image = "registry.ng.bluemix.net/armada-master/keepalived:0000"
	d.Spec.Template.Spec.InitContainers[0].Image = "registry.ng.bluemix.net/armada-master/keepalived:0000"
	_, err = c.KubeClient.AppsV1().Deployments(d.ObjectMeta.Namespace).Update(context.TODO(), d, metav1.UpdateOptions{})
	if nil != err {
		t.Fatalf("Unexpected error updating load balancer 'new': %v", err)
	}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("test"), nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'new' updated: %v, %v", status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("test"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'new': %v, %v", d, err)
	}
	if d.Spec.Template.Spec.Containers[0].Image != c.Config.LBDeployment.Image {
		t.Fatalf("Unexpected error updating load balancer deployment keepalived container Image: %v", d.Spec.Template.Spec.Containers[0].Image)
	}
	if d.Spec.Template.Spec.InitContainers[0].Image != c.Config.LBDeployment.Image {
		t.Fatalf("Unexpected error updating load balancer deployment keepalived Init container Image: %v", d.Spec.Template.Spec.Containers[0].Image)
	}

	// Don't update LB deployment if keepalived image version is higher than cloud config keepalived image version 1328
	d.Spec.Template.Spec.Containers[0].Image = "registry.ng.bluemix.net/armada-master/keepalived:9999"
	d.Spec.Template.Spec.InitContainers[0].Image = "registry.ng.bluemix.net/armada-master/keepalived:9999"
	_, err = c.KubeClient.AppsV1().Deployments(d.ObjectMeta.Namespace).Update(context.TODO(), d, metav1.UpdateOptions{})
	if nil != err {
		t.Fatalf("Unexpected error updating load balancer 'new': %v", err)
	}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("test"), nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'new' updated: %v, %v", status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("test"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'new': %v, %v", d, err)
	}
	if d.Spec.Template.Spec.Containers[0].Image == c.Config.LBDeployment.Image {
		t.Fatalf("Unexpected error updating load balancer deployment keepalived container Image: %v", d.Spec.Template.Spec.Containers[0].Image)
	}
	if d.Spec.Template.Spec.InitContainers[0].Image == c.Config.LBDeployment.Image {
		t.Fatalf("Unexpected error updating load balancer deployment keepalived Init container Image: %v", d.Spec.Template.Spec.Containers[0].Image)
	}

	// Update LB deployment image if non numeric image values doesn't match with cluster provider config image
	d.Spec.Template.Spec.Containers[0].Image = "registry.ng.bluemix.net/armada-master/keepalived:xyz"
	d.Spec.Template.Spec.InitContainers[0].Image = "registry.ng.bluemix.net/armada-master/keepalived:xyz"
	_, err = c.KubeClient.AppsV1().Deployments(d.ObjectMeta.Namespace).Update(context.TODO(), d, metav1.UpdateOptions{})
	if nil != err {
		t.Fatalf("Unexpected error updating load balancer 'new': %v", err)
	}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("test"), nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'new' updated: %v, %v", status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("test"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'new': %v, %v", d, err)
	}
	if d.Spec.Template.Spec.Containers[0].Image != c.Config.LBDeployment.Image {
		t.Fatalf("Unexpected error updating load balancer deployment keepalived container Image: %v", d.Spec.Template.Spec.Containers[0].Image)
	}
	if d.Spec.Template.Spec.InitContainers[0].Image != c.Config.LBDeployment.Image {
		t.Fatalf("Unexpected error updating load balancer deployment keepalived Init container Image: %v", d.Spec.Template.Spec.Containers[0].Image)
	}

	// MixedProtocol (i.e. both TCP and UDP ports) is NOT supportd for now
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerMixedService("testMixed"), nil)
	if nil == status && err != nil {
		assert.Contains(t, err.Error(), "mixed protocol")
	} else {
		t.Fatalf("MixedProtocol did not return error")
	}

	// Duplicate load balancers exist
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("dup"), nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'dup' created: %v, %v", status, err)
	}

	// Cloud provider VLAN IP config doesn't exist
	c.Config.LBDeployment.VlanIPConfigMap = "doesntexist"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("doesntexist"), nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'doesntexist' created: %v, %v", status, err)
	}

	expectedErrorDoesNotExist1 := "Resolve the current issues before adding another Subnet"
	expectedError1 := "[ErrorSubnetLimitReached: There are already the maximum number of subnets permitted in this VLAN - Number of Occurrences: 2.]"
	expectedError2 := "[ErrorSoftlayerDown: Softlayer is experiencing issues please try ordering your subnet later - Number of Occurrences: 1.]"

	// No cloud provider VLAN IPs exist
	c.Config.LBDeployment.VlanIPConfigMap = "emptydata"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("emptydata"), nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'emptydata' created: %v, %v", status, err)
	}
	// Verify `lbPortableSubnetMessage` error message doesn't show up since we don't have any errors in the CM
	if strings.Contains(fmt.Sprintf("%v", err), expectedErrorDoesNotExist1) {
		t.Fatalf("Expected errors 'noips' error message: %v, %v", status, err)
	}

	// Verify `lbPortableSubnetMessage` error message shows up since we don't have any errors in the CM
	c.Config.LBDeployment.VlanIPConfigMap = "errorlanips"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("errorlanips"), nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'errorlanips' created: %v, %v", status, err)
	}
	if !strings.Contains(fmt.Sprintf("%v", err), expectedError1) || !strings.Contains(fmt.Sprintf("%v", err), expectedError2) {
		t.Fatalf("Expected errors 'noips' error message: %v, %v", status, err)
	}

	// No nodes exist on cloud provider VLANs
	c.Config.LBDeployment.VlanIPConfigMap = "unavailablevlanips"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("unavailablevlanips"), nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'unavailablevlanips' created: %v, %v", status, err)
	}

	// Set valid VLAN IP config map
	c.Config.LBDeployment.VlanIPConfigMap = "ibm-cloud-provider-vlan-ip-config"

	// Request cloud provider IP that is unavailable
	lbService := getLoadBalancerService("unavailableip")
	lbService.Spec.LoadBalancerIP = "192.168.100.34"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'unavilableip' created: %v, %v", status, err)
	}

	// Ensure load balancer created with requested cloud provider IP on dal09
	lbService = getLoadBalancerService("requestip-dal09")
	lbService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "public"
	lbService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal09"
	lbService.Spec.LoadBalancerIP = "192.168.10.35"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'requestip-dal09' created: %v, %v", status, err)
	}
	if 1 != len(status.Ingress) || 0 != strings.Compare("192.168.10.35", status.Ingress[0].IP) || 0 != len(status.Ingress[0].Hostname) {
		t.Fatalf("Unexpected load balancer 'requestip-dal09' status: %v", status)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("requestip-dal09"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'requestip-dal09': %v, %v", d, err)
	}

	// Ensure load balancer fails with requested cloud provider IP on dal10
	lbService = getLoadBalancerService("requestip-dal10")
	lbService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "public"
	lbService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal10"
	lbService.Spec.LoadBalancerIP = "192.168.10.35"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'requestip-dal10' created: %v, %v", status, err)
	}

	// Request updated cloud provider IP
	lbService.Spec.LoadBalancerIP = "192.168.10.34"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'requestip' updated: %v, %v", status, err)
	}

	// Request in-use cloud provider IP
	lbService = getLoadBalancerService("inuseip")
	lbService.Spec.LoadBalancerIP = "192.168.10.35"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'inuseip' created: %v, %v", status, err)
	}

	// Ensure SCTP protocol load balancer fails
	lbService = getLoadBalancerService("sctp-protocol")
	lbService.Spec.Ports = []v1.ServicePort{{
		Name:     "sctp",
		Protocol: v1.ProtocolSCTP,
		Port:     80,
	}}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'sctp-protocol' created: %v, %v", status, err)
	}

	// Ensure application protocol load balancer fails
	lbService = getLoadBalancerService("app-protocol")
	lbService.Spec.Ports = []v1.ServicePort{{
		Name:        "app",
		Protocol:    v1.ProtocolTCP,
		Port:        80,
		AppProtocol: strPtr("https"),
	}}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'app-protocol' created: %v, %v", status, err)
	}

	// Request IPVS LB service
	ipvsService := getIPVSService(true, false)
	ipvsService.ObjectMeta.Name = "ipvs-service-dal10"
	ipvsService.Spec.LoadBalancerIP = ""
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, ipvsService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'ipvs-service-dal10' created: %v, %v", status, err)
	}

	cmName := lbDeploymentNamePrefix + getCloudProviderIPLabelValue(status.Ingress[0].IP)
	result, err := c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	if result == nil || err != nil {
		t.Fatalf("Unexpected error ensure load balancer 'ipvs-service-dal10'.  Not able to get Configmap %v, %v", cmName, err)
	}

	// Ensure load balancer created
	lbService = getLoadBalancerService("new")
	lbService.Spec.LoadBalancerSourceRanges = []string{"192.168.10.34/32"}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, lbService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'new' created: %v, %v", status, err)
	}

	if 1 != len(status.Ingress) || !strings.HasPrefix(status.Ingress[0].IP, "192.168.10.") || 0 != len(status.Ingress[0].Hostname) {
		t.Fatalf("Unexpected load balancer 'new' status: %v", status.Ingress[0].IP)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("new"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'new': %v, %v", d, err)
	}

	// Setup expected data for verification
	expectedIPLabel := getCloudProviderIPLabelValue(status.Ingress[0].IP)
	expectedName := lbDeploymentNamePrefix + expectedIPLabel

	// Verify the load balancer deployment containers count
	if 1 != len(d.Spec.Template.Spec.Containers) {
		t.Fatalf("Unexpected containers for load balancer 'new': %v", d.Spec.Template.Spec.Containers)
	}

	// Verify the load balancer deployment is created with an initContainer
	if 1 != len(d.Spec.Template.Spec.InitContainers) {
		t.Fatal("Missing initContainer for load balancer 'new'")
	}

	// Verify the load balancer deployment name
	if 0 != strings.Compare(expectedName, d.ObjectMeta.Name) {
		t.Fatalf("Unexpected name for load balancer 'new': %v", d.ObjectMeta.Name)
	}
	if 0 != strings.Compare(expectedName, d.Spec.Template.ObjectMeta.Name) {
		t.Fatalf("Unexpected spec name for load balancer 'new': %v", d.Spec.Template.ObjectMeta.Name)
	}
	if 0 != strings.Compare(expectedName, d.Spec.Template.Spec.Containers[0].Name) {
		t.Fatalf("Unexpected container name for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].Name)
	}

	// Verify the load balancer deployment namespace
	if 0 != strings.Compare(lbDeploymentNamespace, d.ObjectMeta.Namespace) {
		t.Fatalf("Unexpected namespace for load balancer 'new': %v", d.ObjectMeta.Namespace)
	}

	// Verify the load balancer deployment labels
	dLabels := d.ObjectMeta.Labels
	if 3 != len(dLabels) {
		t.Fatalf("Unexpected number of labels for load balancer 'new': %v", dLabels)
	}
	if 0 != strings.Compare(expectedIPLabel, dLabels[lbIPLabel]) {
		t.Fatalf("Unexpected label %v for load balancer 'new': %v", lbIPLabel, dLabels)
	}
	if 0 != strings.Compare(getTestLoadBlancerName("new"), dLabels[lbNameLabel]) {
		t.Fatalf("Unexpected label %v for load balancer 'new': %v", lbNameLabel, dLabels)
	}
	if 0 != strings.Compare(c.Config.LBDeployment.Application, dLabels[lbApplicationLabel]) {
		t.Fatalf("Unexpected label %v for load balancer 'new': %v", lbApplicationLabel, dLabels)
	}
	dSpecTemplateLabels := d.Spec.Template.ObjectMeta.Labels
	if 3 != len(dSpecTemplateLabels) {
		t.Fatalf("Unexpected number of spec labels for load balancer 'new': %v", dSpecTemplateLabels)
	}
	if 0 != strings.Compare(expectedIPLabel, dSpecTemplateLabels[lbIPLabel]) {
		t.Fatalf("Unexpected spec label %v for load balancer 'new': %v", lbIPLabel, dSpecTemplateLabels)
	}
	if 0 != strings.Compare(getTestLoadBlancerName("new"), dSpecTemplateLabels[lbNameLabel]) {
		t.Fatalf("Unexpected spec label %v for load balancer 'new': %v", lbNameLabel, dSpecTemplateLabels)
	}
	if 0 != strings.Compare(c.Config.LBDeployment.Application, dSpecTemplateLabels[lbApplicationLabel]) {
		t.Fatalf("Unexpected spec label %v for load balancer 'new': %v", lbApplicationLabel, dSpecTemplateLabels)
	}

	// Verify the load balancer deployment affinity
	dSpecTemplateAffinity := d.Spec.Template.Spec.Affinity
	if nil == dSpecTemplateAffinity {
		t.Fatalf("Unexpected affinity for load balancer 'new': %v", dSpecTemplateAffinity)
	}
	podAffinity := dSpecTemplateAffinity.PodAffinity
	if nil != podAffinity {
		t.Fatalf("Unexpected pod affinity for load balancer 'new': %v", podAffinity)
	}
	podAntiAffinity := dSpecTemplateAffinity.PodAntiAffinity
	if nil == podAntiAffinity {
		t.Fatalf("Unexpected pod anti-affinity for load balancer 'new': %v", podAntiAffinity)
	}
	if 1 != len(podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) {
		t.Fatalf("Unexpected required pod anti-affinity for load balancer 'new': %v", podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution)
	}
	if 1 != len(podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution) {
		t.Fatalf("Unexpected preferred pod anti-affinity for load balancer 'new': %v", podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)
	}
	nodeAffinity := dSpecTemplateAffinity.NodeAffinity
	if nil == nodeAffinity {
		t.Fatalf("Unexpected node affinity for load balancer 'new': %v", nodeAffinity)
	}
	if 0 != len(nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution) {
		t.Fatalf("Unexpected node affinity for load balancer 'new': %v", nodeAffinity)
	}
	if nil == nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
		t.Fatalf("Unexpected node affinity for load balancer 'new': %v", nodeAffinity)
	}
	if 1 != len(nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) {
		t.Fatalf("Unexpected node affinity for load balancer 'new': %v", nodeAffinity)
	}
	dSpecTemplateTolerations := d.Spec.Template.Spec.Tolerations

	if len(dSpecTemplateTolerations) != 2 {
		t.Fatalf("Unexpected toleration count for load balancer 'new': %v", dSpecTemplateTolerations)
	}

	for _, toleration := range dSpecTemplateTolerations {
		if lbTolerationKey != toleration.Key {
			t.Fatalf("Unexpected toleration Key for load balancer 'new': %v", toleration.Key)
		}

		if toleration.Value != lbTolerationValueGateway && toleration.Value != lbTolerationValueEdge {
			t.Fatalf("Unexpected toleration Value for load balancer 'new': %v", toleration.Value)
		}

		if 0 != len(string(toleration.Operator)) ||
			0 != len(string(toleration.Effect)) ||
			nil != toleration.TolerationSeconds {
			t.Fatalf("Unexpected toleration settings for load balancer 'new': %v", toleration.Value)
		}
	}

	// Verify the load balancer resource requests
	requests := d.Spec.Template.Spec.Containers[0].Resources.Requests
	if 2 != len(requests) ||
		lbCPUResourceRequest != requests.Cpu().String() ||
		lbMemoryResourceRequest != requests.Memory().String() {
		t.Fatalf("Unexpected resource requests for load balancer 'new': %v", requests)
	}

	// Verify the load balancer deployment spec
	replicas := d.Spec.Replicas
	if nil == replicas || 2 != *replicas {
		t.Fatalf("Unexpected replicas for load balancer 'new': %v", replicas)
	}
	if d.Spec.MinReadySeconds != 90 {
		t.Fatalf("Unexpected min ready seconds for load balancer 'new': %v", d.Spec.MinReadySeconds)
	}
	revisionHistoryLimit := d.Spec.RevisionHistoryLimit
	if nil == revisionHistoryLimit || 1 != *revisionHistoryLimit {
		t.Fatalf("Unexpected revision history limit for load balancer 'new': %v", revisionHistoryLimit)
	}

	// Verify the load balancer deployment strategy
	if apps.RollingUpdateDeploymentStrategyType != d.Spec.Strategy.Type {
		t.Fatalf("Unexpected strategy type for load balancer 'new': %v", d.Spec.Strategy.Type)
	}
	if nil == d.Spec.Strategy.RollingUpdate.MaxUnavailable || 0 != strings.Compare("100%", d.Spec.Strategy.RollingUpdate.MaxUnavailable.StrVal) {
		t.Fatalf("Unexpected strategy rolling update max unavailable for load balancer 'new': %v", d.Spec.Strategy.RollingUpdate.MaxUnavailable)
	}
	if nil == d.Spec.Strategy.RollingUpdate.MaxSurge || 1 != d.Spec.Strategy.RollingUpdate.MaxSurge.IntVal {
		t.Fatalf("Unexpected strategy rolling update max surge for load balancer 'new': %v", d.Spec.Strategy.RollingUpdate.MaxSurge)
	}

	// Verify the load balancer deployment security context
	if !d.Spec.Template.Spec.HostNetwork {
		t.Fatalf("Unexpected no host network security context for load balancer 'new'")
	}
	privileged := d.Spec.Template.Spec.Containers[0].SecurityContext.Privileged
	if nil != privileged && *privileged {
		t.Fatalf("Unexpected privileged security context for load balancer 'new': %v", *privileged)
	}
	// New load balancers should be created with non-root user/group
	lbUser := d.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser
	lbGroup := d.Spec.Template.Spec.Containers[0].SecurityContext.RunAsGroup
	if *lbUser != lbNonRootUser || *lbGroup != lbNonRootGroup {
		t.Fatalf("Unexpected user/group ID for load balancer - user: %v group: %v", *lbUser, *lbGroup)
	}
	capabilities := d.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities
	if nil == capabilities || 2 != len(capabilities.Add) || lbNetAdminCapability != capabilities.Add[0] ||
		lbNetRawCapability != capabilities.Add[1] || 0 != len(capabilities.Drop) {
		t.Fatalf("Unexpected capabilities security context for load balancer 'new': %v", capabilities)
	}
	priorityClassName := d.Spec.Template.Spec.PriorityClassName
	if lbPriorityClassName != priorityClassName {
		t.Fatalf("Unexpected priority class name for load balancer 'new': %v", priorityClassName)
	}

	// Verify the load balancer deployment image
	if 0 != strings.Compare(c.Config.LBDeployment.Image, d.Spec.Template.Spec.Containers[0].Image) {
		t.Fatalf("Unexpected image for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].Image)
	}
	if v1.PullIfNotPresent != d.Spec.Template.Spec.Containers[0].ImagePullPolicy {
		t.Fatalf("Unexpected image pull policy for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].ImagePullPolicy)
	}
	// Verify the load balancer deployment initContainer image
	if 0 != strings.Compare(c.Config.LBDeployment.Image, d.Spec.Template.Spec.InitContainers[0].Image) {
		t.Fatalf("Unexpected initContainer image for load balancer 'new': %v", d.Spec.Template.Spec.InitContainers[0].Image)
	}
	if v1.PullIfNotPresent != d.Spec.Template.Spec.InitContainers[0].ImagePullPolicy {
		t.Fatalf("Unexpected image pull policy for load balancer 'new': %v", d.Spec.Template.Spec.InitContainers[0].ImagePullPolicy)
	}

	// Verify the load balancer deployment volumes
	if 1 != len(d.Spec.Template.Spec.Volumes) {
		t.Fatalf("Unexpected volumes for load balancer 'new': %v", d.Spec.Template.Spec.Volumes)
	}
	if 0 != strings.Compare(c.Config.LBDeployment.Application+"-status", d.Spec.Template.Spec.Volumes[0].Name) {
		t.Fatalf("Unexpected volume name for load balancer 'new': %v", d.Spec.Template.Spec.Volumes[0].Name)
	}
	if 0 != strings.Compare("/tmp/"+c.Config.LBDeployment.Application, d.Spec.Template.Spec.Volumes[0].VolumeSource.HostPath.Path) {
		t.Fatalf("Unexpected volume host path for load balancer 'new': %v", d.Spec.Template.Spec.Volumes[0].VolumeSource.HostPath.Path)
	}
	if 1 != len(d.Spec.Template.Spec.Containers[0].VolumeMounts) {
		t.Fatalf("Unexpected volume mounts for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].VolumeMounts)
	}
	if 0 != strings.Compare(c.Config.LBDeployment.Application+"-status", d.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name) {
		t.Fatalf("Unexpected volume mount name for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].VolumeMounts[0].Name)
	}
	if 0 != strings.Compare("/status", d.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath) {
		t.Fatalf("Unexpected volume mount path for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].VolumeMounts[0].MountPath)
	}

	// Verify the load balancer deployment environment
	if 2 != len(d.Spec.Template.Spec.Containers[0].Env) {
		t.Fatalf("Unexpected environment variables for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].Env)
	}
	if 0 != strings.Compare("VIRTUAL_IP", d.Spec.Template.Spec.Containers[0].Env[0].Name) {
		t.Fatalf("Unexpected environment variable name for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].Env[0].Name)
	}
	if 0 != strings.Compare(status.Ingress[0].IP, d.Spec.Template.Spec.Containers[0].Env[0].Value) {
		t.Fatalf("Unexpected environment variable value for load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].Env[0].Value)
	}

	// Update the load balancer deployment.
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("new"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'new': %v, %v", d, err)
	}

	// Attempt to run the deployment as the root user
	d.Spec.Template.Spec.Containers[0].SecurityContext = &v1.SecurityContext{
		RunAsUser: new(int64),
	}

	d.Spec.Template.Spec.PriorityClassName = ""
	d.Spec.Template.Spec.Containers[0].Resources = v1.ResourceRequirements{}
	_, err = c.KubeClient.AppsV1().Deployments(d.ObjectMeta.Namespace).Update(context.TODO(), d, metav1.UpdateOptions{})
	if nil != err {
		t.Fatalf("Unexpected error updating load balancer 'new': %v", err)
	}
	newImage := c.Config.LBDeployment.Image + "0"
	c.Config.LBDeployment.Image = newImage
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("new"), nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'new' updated: %v, %v", status, err)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("new"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding updated load balancer 'new': %v, %v", d, err)
	}
	if 0 != strings.Compare(newImage, d.Spec.Template.Spec.Containers[0].Image) {
		t.Fatalf("Unexpected image for updated load balancer 'new': %v", d.Spec.Template.Spec.Containers[0].Image)
	}
	if 0 != strings.Compare(newImage, d.Spec.Template.Spec.InitContainers[0].Image) {
		t.Fatalf("Unexpected initContainer image for updated load balancer 'new': %v", d.Spec.Template.Spec.InitContainers[0].Image)
	}
	privileged = d.Spec.Template.Spec.Containers[0].SecurityContext.Privileged
	if nil != privileged && *privileged {
		t.Fatalf("Unexpected privileged security context for updated load balancer 'new': %v", *privileged)
	}

	// Verify Security Context after update
	lbUser = d.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser
	lbGroup = d.Spec.Template.Spec.Containers[0].SecurityContext.RunAsGroup
	if *lbUser != lbNonRootUser || *lbGroup != lbNonRootGroup {
		t.Fatalf("Unexpected user/group ID for load balancer after update - user: %v group: %v", *lbUser, *lbGroup)
	}
	capabilities = d.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities
	if nil == capabilities || 2 != len(capabilities.Add) || lbNetAdminCapability != capabilities.Add[0] ||
		lbNetRawCapability != capabilities.Add[1] || 0 != len(capabilities.Drop) {
		t.Fatalf("Unexpected capabilities security context for updated load balancer 'new': %v", capabilities)
	}
	priorityClassName = d.Spec.Template.Spec.PriorityClassName
	if lbPriorityClassName != priorityClassName {
		t.Fatalf("Unexpected priority class name for updated load balancer 'new': %v", priorityClassName)
	}

	// Verify the load balancer resource requests were updated
	requests = d.Spec.Template.Spec.Containers[0].Resources.Requests
	if 2 != len(requests) ||
		lbCPUResourceRequest != requests.Cpu().String() ||
		lbMemoryResourceRequest != requests.Memory().String() {
		t.Fatalf("Unexpected resource requests for updated load balancer 'new': %v", requests)
	}

	// Verify that we correctly handle existing root load balancers. These load balancers:
	// i.   Do NOT have a specified user (root by default)
	// ii.  Do NOT have an initContainer
	// iii. Do have an existing SecurityContext
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("new"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'new': %v, %v", d, err)
	}

	// Delete the keepalived initContainer
	d.Spec.Template.Spec.InitContainers = []v1.Container{}

	// Create a security context without a specified user
	d.Spec.Template.Spec.Containers[0].SecurityContext = &v1.SecurityContext{
		RunAsUser:    nil,
		Capabilities: d.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities,
	}

	// Register deployment changes with API server
	_, err = c.KubeClient.AppsV1().Deployments(d.ObjectMeta.Namespace).Update(context.TODO(), d, metav1.UpdateOptions{})
	if nil != err {
		t.Fatalf("Unexpected error updating load balancer 'new': %v", err)
	}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("new"), nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'new' updated: %v, %v", status, err)
	}

	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("new"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding updated load balancer 'new': %v, %v", d, err)
	}

	// Verify initContainer is added during load balancer update
	if 1 != len(d.Spec.Template.Spec.InitContainers) {
		t.Fatal("Missing initContainer after update for load balancer 'new'")
	}

	// Verify that we update to non-root user
	lbUser = d.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser
	lbGroup = d.Spec.Template.Spec.Containers[0].SecurityContext.RunAsGroup
	if *lbUser != lbNonRootUser || *lbGroup != lbNonRootGroup {
		t.Fatalf("Unexpected user/group ID for load balancer after update - user: %v group: %v", *lbUser, *lbGroup)
	}

	// No public unreserved cloud provider VLAN IPs available
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, getLoadBalancerService("noips"), nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'noips' created: %v, %v", status, err)
	}

	// Bad cloud provider IP type
	badiptype := getLoadBalancerService("badiptype")
	badiptype.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "badiptype"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, badiptype, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'badiptype' created: %v, %v", status, err)
	}

	// Ensure load balancer created with private IP on dal09
	privateIPService := getLoadBalancerService("privateip-dal09")
	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal09"
	privateIPService.Spec.LoadBalancerIP = "10.10.10.21"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, privateIPService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'privateip-dal09' created: %v, %v", status, err)
	}
	if 1 != len(status.Ingress) || 0 != strings.Compare("10.10.10.21", status.Ingress[0].IP) || 0 != len(status.Ingress[0].Hostname) {
		t.Fatalf("Unexpected load balancer 'privateip-dal09' status: %v", status.Ingress[0].IP)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("privateip-dal09"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'privateip': %v, %v", d, err)
	}

	// Ensure load balancer fails with private IP on dal10
	privateIPService = getLoadBalancerService("privateip-dal10")
	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal10"
	privateIPService.Spec.LoadBalancerIP = "10.10.10.21"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, privateIPService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'privateip-dal10' created: %v, %v", status, err)
	}

	// Ensure load balancer created for public ingress controller on dal09
	publicIngressService := getLoadBalancerService("public-ingress-controller-dal09")
	publicIngressService.Annotations[ServiceAnnotationIngressControllerPublic] = "192.168.10.15"
	publicIngressService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal09"
	publicIngressService.Annotations[ServiceAnnotationLoadBalancerCloudProviderVlan] = "1"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, publicIngressService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'public-ingress-controller-dal09' created: %v, %v", status, err)
	}
	if 1 != len(status.Ingress) || 0 != strings.Compare("192.168.10.15", status.Ingress[0].IP) || 0 != len(status.Ingress[0].Hostname) {
		t.Fatalf("Unexpected load balancer 'public-ingress-controller-dal09' status: %v", status.Ingress[0].IP)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("public-ingress-controller-dal09"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'public-ingress-controller-dal09': %v, %v", d, err)
	}

	// Ensure load balancer fails for public ingress controller on dal10
	publicIngressService = getLoadBalancerService("public-ingress-controller-dal10")
	publicIngressService.Annotations[ServiceAnnotationIngressControllerPublic] = "192.168.10.15"
	publicIngressService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal10"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, publicIngressService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'public-ingress-controller-dal10' created: %v, %v", status, err)
	}

	// Ensure load balancer created for private ingress controller on dal09
	privateIngressService := getLoadBalancerService("private-ingress-controller-dal09")
	privateIngressService.Annotations[ServiceAnnotationIngressControllerPrivate] = "10.10.10.20"
	privateIngressService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal09"
	publicIngressService.Annotations[ServiceAnnotationLoadBalancerCloudProviderVlan] = "2"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, privateIngressService, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'private-ingress-controller-dal09' created: %v, %v", status, err)
	}
	if 1 != len(status.Ingress) || 0 != strings.Compare("10.10.10.20", status.Ingress[0].IP) || 0 != len(status.Ingress[0].Hostname) {
		t.Fatalf("Unexpected load balancer 'private-ingress-controller-dal09' status: %v", status.Ingress[0].IP)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("private-ingress-controller-dal09"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'private-ingress-controller-dal09': %v, %v", d, err)
	}

	// Ensure load balancer fails for private ingress controller on dal10
	privateIngressService = getLoadBalancerService("private-ingress-controller-dal10")
	privateIngressService.Annotations[ServiceAnnotationIngressControllerPrivate] = "10.10.10.20"
	privateIngressService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal10"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, privateIngressService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'private-ingress-controller-dal10' created: %v, %v", status, err)
	}

	// Verify we fail when an IPVS lb has cluster networking enabled
	servicePrivateCluster, _ := createTestLoadBalancerServiceIPVS("testIPVSClusterNetworking", "192.168.10.38", false, false)
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, servicePrivateCluster, nodes)
	if nil != status || nil == err {
		t.Fatalf("Unexpected error expected service account to not be created: %v, %v", status, err)
	}
}

func TestEnsureLoadBalancerIPVSUpdate(t *testing.T) {
	// used to ensure the exec for calicoctl is redirected internally
	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		return cmd
	}
	c, clusterName, _ := getTestCloud()

	service, cm := createTestLoadBalancerServiceIPVS("testIPVSDeleteCMCreate", "192.168.10.45", true, true)
	service.Spec.LoadBalancerIP = "192.168.10.45"

	// Delete The service account to ensure we can't create an IPVS lb without a service account
	err := c.KubeClient.CoreV1().ServiceAccounts(lbDeploymentNamespace).Delete(context.TODO(), lbDeploymentServiceAccountName, metav1.DeleteOptions{})
	if nil != err {
		t.Fatalf("Unexpected error remove service account: %v, %v", lbDeploymentNamespace, lbDeploymentServiceAccountName)
	}
	// Test to add nodes to force a service update
	node1, node2, _, _ := createTestCloudNodes()
	nodes := []*v1.Node{node1, node2}
	status, err := c.EnsureLoadBalancer(context.Background(), clusterName, service, nodes)
	if nil != status || nil == err {
		t.Fatalf("Unexpected error expected service account to not be created: %v, %v", status, err)
	}

	// Call getTestCloud to create the service account that was deleted above
	c, clusterName, _ = getTestCloud()

	// Test to add nodes to force a service update
	node1, node2, _, _ = createTestCloudNodes()
	nodes = []*v1.Node{node1, node2}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, service, nodes)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer update 'testIPVSDeleteCMCreate' created: %v, %v", status, err)
	}
	updatedCm, err := c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Get(context.TODO(), cm.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unable to retrieve IPVS configmap after service update.  CM name: %v, %v", cm.Name, err)
	}
	if reflect.DeepEqual(cm.Data, updatedCm.Data) {
		t.Fatalf("IPVS configmap not updated after service update.  CM name: %v", cm.Name)
	}

	// Test updating a service that's deployment contains the feature flags works
	service, _ = createTestLoadBalancerServiceIPVS("testIPVSDeleteCMDeploy", "192.168.10.52", true, true)
	service.Spec.LoadBalancerIP = "192.168.10.52"
	// Test to add nodes to force a service update
	node1, node2, _, _ = createTestCloudNodes()
	nodes = []*v1.Node{node1, node2}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, service, nodes)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error expected service account to not be created: %v, %v", status, err)
	}

	// Test that checks the update of an ipvs service
	service, _ = createTestLoadBalancerServiceIPVS("testIPVSDeleteSA", "192.168.10.34", true, true)
	service.Spec.LoadBalancerIP = "192.168.10.34"
	// Delete The service account to ensure we can't create an IPVS lb without a service account
	err = c.KubeClient.CoreV1().ServiceAccounts(lbDeploymentNamespace).Delete(context.TODO(), lbDeploymentServiceAccountName, metav1.DeleteOptions{})
	if nil != err {
		t.Fatalf("Unexpected error remove service account: %v, %v. Error: %v", lbDeploymentNamespace, lbDeploymentServiceAccountName, err)
	}

	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, service, nil)
	if nil != status || nil == err || !strings.Contains(fmt.Sprintf("%v", err), "serviceaccounts \"ibm-cloud-provider-lb\" not found") {
		t.Fatalf("Unexpected error expected service account to not be created: %v, %v", status, err)
	}

	// Verify error when making public IPVS LB private
	service, _ = createTestLoadBalancerServiceIPVS("testIPVSDeleteCM", "192.168.10.50", true, false)
	service.Spec.LoadBalancerIP = "192.168.10.50"
	// Delete The service account to ensure we can't create an IPVS lb without a service account
	err = c.KubeClient.CoreV1().ServiceAccounts(lbDeploymentNamespace).Delete(context.TODO(), lbDeploymentServiceAccountName, metav1.DeleteOptions{})
	if nil == err {
		t.Fatalf("Expected error making a public ipvs service private: %v, %v. Error: %v", lbDeploymentNamespace, lbDeploymentServiceAccountName, err)
	}

	// Verify we fail when an IPVS lb has cluster networking enabled
	servicePrivateCluster, _ := createTestLoadBalancerServiceIPVS("testIPVSClusterNetworking", "192.168.10.38", false, false)
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, servicePrivateCluster, nodes)
	if nil != status || nil == err {
		t.Fatalf("Unexpected error expected service account to not be created: %v, %v", status, err)
	}
}

func TestEnsureLoadBalancerIPVSCalicoFail(t *testing.T) {
	successCaliCount := 0
	failExecCommand := func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummyWithError", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		return cmd
	}

	successExecCommand := func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		successCaliCount++
		return cmd
	}

	execCommand = failExecCommand
	c, clusterName, _ := getTestCloud()
	service, _ := createTestLoadBalancerServiceIPVS("testIPVSDeleteCMDeploy", "192.168.10.52", true, true)
	service.Spec.LoadBalancerIP = "192.168.10.52"
	status, err := c.EnsureLoadBalancer(context.Background(), clusterName, service, nil)

	if err == nil && status != nil {
		t.Fatalf("Expected calico error returned from EnsureLoadBalancer")
	}

	execCommand = successExecCommand
	// make sure that the 2nd attempt is successful when calico failed in the first attempt
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, service, nil)
	if err != nil && status == nil {
		t.Fatalf("Expected EnsureLoadBalancer to succeed. Error: %v", err)
	}
	if successCaliCount != 1 {
		t.Fatalf("Expected successCaliCount to be 1.  Actual value is: %v", successCaliCount)
	}

	// now update the service and force calico to fail during the update
	successCaliCount = 0
	execCommand = failExecCommand
	service.Spec.Ports = []v1.ServicePort{{
		Port:     81,
		Protocol: v1.ProtocolTCP,
	}}
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, service, nil)
	if err == nil && status != nil {
		t.Fatalf("Expected calico error returned from EnsureLoadBalancer")
	}

	// now make sure the update is successful on the 2nd attempt and ensure that calicoctl was called
	execCommand = successExecCommand
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, service, nil)
	if err != nil && status == nil {
		t.Fatalf("Expected EnsureLoadBalancer to succeed. Error: %v", err)
	}
	if successCaliCount != 1 {
		t.Fatalf("Expected successCaliCount to be 1.  Actual value is: %v", successCaliCount)
	}

	n1, _, _, _ := createTestCloudNodes()
	nodes := []*v1.Node{n1}
	execCommand = failExecCommand
	successCaliCount = 0
	err = c.UpdateLoadBalancer(context.Background(), clusterName, service, nodes)
	if err == nil {
		t.Fatalf("Expected calico error returned from UpdateLoadBalancer")
	}

	execCommand = successExecCommand
	err = c.UpdateLoadBalancer(context.Background(), clusterName, service, nodes)
	if err != nil {
		t.Fatalf("Expected UpdateLoadBalancer to succeed. Error: %v", err)
	}
	if successCaliCount != 1 {
		t.Fatalf("Expected successCaliCount to be 1.  Actual value is: %v", successCaliCount)
	}
}

func TestEnsureLoadBalancerVlanAnnotation(t *testing.T) {
	var err error
	var status *v1.LoadBalancerStatus
	var d *apps.Deployment

	c, clusterName, _ := getTestCloud()

	// Ensure load balancer created with private IP in dal09 on vlan 2
	privateIPServiceVlanAnnotation := getLoadBalancerService("privateip-vlan-annotation")
	privateIPServiceVlanAnnotation.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	privateIPServiceVlanAnnotation.Annotations[ServiceAnnotationLoadBalancerCloudProviderVlan] = "2"
	privateIPServiceVlanAnnotation.Spec.LoadBalancerIP = "10.10.10.22"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, privateIPServiceVlanAnnotation, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'privateip-vlan-annotation' created: %v, %v", status, err)
	}
	if 1 != len(status.Ingress) || 0 != strings.Compare("10.10.10.22", status.Ingress[0].IP) || 0 != len(status.Ingress[0].Hostname) {
		t.Fatalf("Unexpected load balancer 'privateip-vlan-annotation' status: %v", status.Ingress[0].IP)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("privateip-vlan-annotation"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'privateip': %v, %v", d, err)
	}

	// Ensure load balancer fails with vlan from the wrong zone
	privateIPServiceBadZone := getLoadBalancerService("privateip-bad-zone")
	privateIPServiceBadZone.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal10"
	privateIPServiceBadZone.Annotations[ServiceAnnotationLoadBalancerCloudProviderVlan] = "1"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, privateIPServiceBadZone, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'privateip-dal10-1' created: %v, %v", status, err)
	}

	// Ensure load balancer fails with random vlan
	privateIPServiceBadVlan := getLoadBalancerService("privateip-bad-vlan")
	privateIPServiceBadVlan.Annotations[ServiceAnnotationLoadBalancerCloudProviderVlan] = "randomVlan"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, privateIPServiceBadVlan, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'privateip-dal10-bad-vlan' created: %v, %v", status, err)
	}

	// Ensure load balancer fails for public ingress controller on wrong vlan
	publicIngressService := getLoadBalancerService("public-ingress-bad-vlan")
	publicIngressService.Annotations[ServiceAnnotationIngressControllerPublic] = "192.168.10.15"
	publicIngressService.Annotations[ServiceAnnotationLoadBalancerCloudProviderVlan] = "2"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, publicIngressService, nil)
	if nil != status || nil == err {
		t.Fatalf("Unexpected ensure load balancer 'public-ingress-bad-vlan' created: %v, %v", status, err)
	}

	// Ensure load balancer created with only the vlan annotation, on vlan: 4.
	// Note: This has to be the last test since its randomly picking an IP from the vlan
	publicIPServiceVlan := getLoadBalancerService("publicip-vlan-annotation")
	publicIPServiceVlan.Annotations[ServiceAnnotationLoadBalancerCloudProviderVlan] = "4"
	status, err = c.EnsureLoadBalancer(context.Background(), clusterName, publicIPServiceVlan, nil)
	if nil == status || nil != err {
		t.Fatalf("Unexpected error ensure load balancer 'publicip-vlan-annotation' created: %v, %v", status, err)
	}
	if 1 != len(status.Ingress) || !strings.Contains(status.Ingress[0].IP, "192.168.10.4") || 0 != len(status.Ingress[0].Hostname) {
		t.Fatalf("Unexpected load balancer 'publicip-vlan-annotation' status: %v", status.Ingress[0].IP)
	}
	d, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("publicip-vlan-annotation"))
	if nil == d || nil != err {
		t.Fatalf("Unexpected error finding load balancer 'publicip': %v, %v", d, err)
	}
}

func TestEnsureLoadBalancerEdgeLabel(t *testing.T) {
	c, clusterName, _ := getTestCloud()

	// Delete nodes to ensure tests work as previously expected
	err := c.KubeClient.CoreV1().Nodes().Delete(context.TODO(), "192.168.10.7", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete the node. Error: %v", err)
	}
	err = c.KubeClient.CoreV1().Nodes().Delete(context.TODO(), "192.168.10.8", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete the node. Error: %v", err)
	}

	// Add dedicated=edge label to node2
	node, _ := c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.6", metav1.GetOptions{})
	node.Labels[lbDedicatedLabel] = lbEdgeNodeValue
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 3 {
		t.Fatalf("dedicated label was not added to node: 192.168.10.6")
	}

	// Create a private Load Balancer - edge label NOT added to node affinity (because LB is on private network)
	privateIPService := getLoadBalancerService("privateip")
	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	privateIPService.Spec.LoadBalancerIP = "10.10.10.21"
	expectedKeys := map[string]string{lbPrivateVlanLabel: "2"}
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, privateIPService, "privateip", expectedKeys)

	// Create a non-edge Load Balancer - edge label NOT added to node affinity (because publicVLAN for IP selected does not have edge tag)
	nonedgeService := getLoadBalancerService("nonedge")
	expectedKeys = map[string]string{lbPublicVlanLabel: "1"}
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, nonedgeService, "nonedge", expectedKeys)

	// Remove dedicated=edge from node2
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.6", metav1.GetOptions{})
	delete(node.Labels, lbDedicatedLabel)
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 2 {
		t.Fatalf("dedicated label was not removed from node: 192.168.10.6")
	}

	// Add dedicated=edge label to node1
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.5", metav1.GetOptions{})
	node.Labels[lbDedicatedLabel] = lbEdgeNodeValue
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 3 {
		t.Fatalf("dedicated label was not added to node: 192.168.10.5")
	}

	// Create a edge Load Balancer - edge label added to node affinity
	edgeService := getLoadBalancerService("edge")
	expectedKeys = map[string]string{lbPublicVlanLabel: "1", lbDedicatedLabel: "edge"}
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)

	// Create a public Load Balancer - Nil Toleration set. Expecting edge toleration to be added.
	// This tests the case when a cluster with a lb that has been deployed a very long time ago when
	// the edge code was not in production, will be updated with the edge tolerations when the service is updated.
	edgeServiceNilTolerations := getLoadBalancerService("testEdgeNodesWithNilToleration")
	edgeServiceNilTolerations.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "public"
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, edgeServiceNilTolerations, "testEdgeNodesWithNilToleration", expectedKeys)

	// Create a public Load Balancer - Empty Toleration set. Expecting toleration to be added
	// This tests the case when a cluster with a lb that has been deployed a very long time ago when
	// the edge code was not in production, will be updated with the edge tolerations when the service is updated.
	edgeServiceEdgeEmptyTolerations := getLoadBalancerService("testEdgeNodesWithEmptyToleration")
	edgeServiceEdgeEmptyTolerations.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "public"
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, edgeServiceEdgeEmptyTolerations, "testEdgeNodesWithEmptyToleration", expectedKeys)

	// Update edge Load Balancer - edge label should still be applied
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)

	// Remove dedicated=edge from node1
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.5", metav1.GetOptions{})
	delete(node.Labels, lbDedicatedLabel)
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 2 {
		t.Fatalf("dedicated label was not removed from node: 192.168.10.5")
	}

	// Update the edge Load Balancer - edge label should be removed
	expectedKeys = map[string]string{lbPublicVlanLabel: "1"}
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)

	// Add dedicated=edge label to node1
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.5", metav1.GetOptions{})
	node.Labels[lbDedicatedLabel] = lbEdgeNodeValue
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 3 {
		t.Fatalf("dedicated label was not added to node: 192.168.10.5")
	}

	// Update edge Load Balancer - edge label should be re-applied
	expectedKeys = map[string]string{lbPublicVlanLabel: "1", lbDedicatedLabel: "edge"}
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)

	// Change label on node1 to dedicated=worker
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.5", metav1.GetOptions{})
	node.Labels[lbDedicatedLabel] = "worker"
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 3 {
		t.Fatalf("dedicated label was not changed oo node: 192.168.10.5")
	}

	// Update the edge Load Balancer - edge label should be removed
	expectedKeys = map[string]string{lbPublicVlanLabel: "1"}
	verifyLoadBalancerEdgeDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)
}

func TestEnsureLoadBalancerGatewayLabel(t *testing.T) {
	c, clusterName, _ := getTestCloud()

	// Delete nodes to ensure tests work as previously expected
	err := c.KubeClient.CoreV1().Nodes().Delete(context.TODO(), "192.168.10.7", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete the node. Error: %v", err)
	}
	err = c.KubeClient.CoreV1().Nodes().Delete(context.TODO(), "192.168.10.8", metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete the node. Error: %v", err)
	}

	// Add dedicated=edge label to node2
	node, _ := c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.6", metav1.GetOptions{})
	node.Labels[lbDedicatedLabel] = lbGatewayNodeValue
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 3 {
		t.Fatalf("dedicated label was not added to node: 192.168.10.6")
	}

	// Create a private Load Balancer - edge label NOT added to node affinity (because LB is on private network)
	privateIPService := getLoadBalancerService("privateip")
	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	privateIPService.Spec.LoadBalancerIP = "10.10.10.21"
	expectedKeys := map[string]string{lbPrivateVlanLabel: "2"}
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, privateIPService, "privateip", expectedKeys)

	// Create a non-edge Load Balancer - edge label NOT added to node affinity (because publicVLAN for IP selected does not have edge tag)
	nonedgeService := getLoadBalancerService("nonedge")
	expectedKeys = map[string]string{lbPublicVlanLabel: "1"}
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, nonedgeService, "nonedge", expectedKeys)

	// Remove dedicated=edge from node2
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.6", metav1.GetOptions{})
	delete(node.Labels, lbDedicatedLabel)
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 2 {
		t.Fatalf("dedicated label was not removed from node: 192.168.10.6")
	}

	// Add dedicated=edge label to node1
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.5", metav1.GetOptions{})
	node.Labels[lbDedicatedLabel] = lbGatewayNodeValue
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 3 {
		t.Fatalf("dedicated label was not added to node: 192.168.10.5")
	}

	// Create a edge Load Balancer - edge label added to node affinity
	edgeService := getLoadBalancerService("edge")
	expectedKeys = map[string]string{lbPublicVlanLabel: "1", lbDedicatedLabel: "gateway"}
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)

	// Create a public Load Balancer - Nil Toleration set. Expecting edge toleration to be added.
	// This tests the case when a cluster with a lb that has been deployed a very long time ago when
	// the edge code was not in production, will be updated with the edge tolerations when the service is updated.
	edgeServiceNilTolerations := getLoadBalancerService("testGatewayNodesWithNilToleration")
	edgeServiceNilTolerations.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "public"
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, edgeServiceNilTolerations, "testGatewayNodesWithNilToleration", expectedKeys)

	// Create a public Load Balancer - Empty Toleration set. Expecting toleration to be added
	// This tests the case when a cluster with a lb that has been deployed a very long time ago when
	// the edge code was not in production, will be updated with the edge tolerations when the service is updated.
	edgeServiceEdgeEmptyTolerations := getLoadBalancerService("testGatewayNodesWithEmptyToleration")
	edgeServiceEdgeEmptyTolerations.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "public"
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, edgeServiceEdgeEmptyTolerations, "testGatewayNodesWithEmptyToleration", expectedKeys)

	// Update edge Load Balancer - gateway label should still be applied
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)

	// Remove dedicated=gateway from node1
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.5", metav1.GetOptions{})
	delete(node.Labels, lbDedicatedLabel)
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 2 {
		t.Fatalf("dedicated label was not removed from node: 192.168.10.5")
	}

	// Update the edge Load Balancer - gateway label should be removed
	expectedKeys = map[string]string{lbPublicVlanLabel: "1"}
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)

	// Add dedicated=gateway label to node1
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.5", metav1.GetOptions{})
	node.Labels[lbDedicatedLabel] = lbGatewayNodeValue
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 3 {
		t.Fatalf("dedicated label was not added to node: 192.168.10.5")
	}

	// Update gateway Load Balancer - gateway label should be re-applied
	expectedKeys = map[string]string{lbPublicVlanLabel: "1", lbDedicatedLabel: "gateway"}
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)

	// Change label on node1 to dedicated=worker
	node, _ = c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.5", metav1.GetOptions{})
	node.Labels[lbDedicatedLabel] = "worker"
	node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	if len(node.Labels) != 3 {
		t.Fatalf("dedicated label was not changed oo node: 192.168.10.5")
	}

	// Update the edge Load Balancer - edge label should be removed
	expectedKeys = map[string]string{lbPublicVlanLabel: "1"}
	verifyLoadBalancerGatewayDeployment(t, c, clusterName, edgeService, "edge", expectedKeys)
}

func TestEnsureLoadBalancerGatewayEdge(t *testing.T) {
	var cc CloudConfig

	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		return cmd
	}
	_, _, cm, _, _, _ := createTestCloudProviderVlanIPConfigMaps()
	sa := createTestServiceAccount()
	cm2, sec1 := createTestCalicoCMandSecret()
	n1 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.5",
			Labels: map[string]string{lbPublicVlanLabel: "1", lbPrivateVlanLabel: "2", lbDedicatedLabel: lbGatewayNodeValue},
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "192.168.10.5",
				},
			},
		},
	}
	n2 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.6",
			Labels: map[string]string{lbPublicVlanLabel: "1", lbPrivateVlanLabel: "2", lbDedicatedLabel: lbGatewayNodeValue},
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "192.168.10.6",
				},
			},
		},
	}
	n3 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.7",
			Labels: map[string]string{lbPrivateVlanLabel: "5"},
		},
	}
	n4 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.8",
			Labels: map[string]string{lbPrivateVlanLabel: "5"},
		},
	}
	fakeKubeClient := fake.NewSimpleClientset(
		cm, cm2,
		sec1,
		sa,
		n1, n2, n3, n4,
	)
	fakeKubeClientV1 := fake.NewSimpleClientset()

	// Build test cloud.
	cc.Global.Version = "1.0.0"
	cc.Kubernetes.ConfigFilePaths = []string{"../test-fixtures/kubernetes/k8s-config"}
	cc.LBDeployment.Image = "registry.ng.bluemix.net/armada-master/keepalived:1328"
	cc.LBDeployment.Application = "keepalived"
	cc.LBDeployment.VlanIPConfigMap = "ibm-cloud-provider-vlan-ip-config"
	c := Cloud{
		Name:       "ibm",
		KubeClient: fakeKubeClient,
		Config:     &cc,
		Recorder:   NewCloudEventRecorderV1("ibm", fakeKubeClientV1.CoreV1().Events(lbDeploymentNamespace)),
		CloudTasks: map[string]*CloudTask{},
	}

	// run first test to ensure load balancer deployment will be scheduled to nodes with dedicated:gateway label
	service, _ := createTestLoadBalancerServiceIPVS("testGatewayLabel", "192.168.10.30", true, true)
	service.Spec.LoadBalancerIP = "192.168.10.30"
	nodes := []*v1.Node{
		n1, n2, n3, n4,
	}
	status, err := c.EnsureLoadBalancer(context.Background(), "test", service, nodes)

	if err != nil && status == nil {
		t.Fatalf("Expected EnsureLoadBalancer to succeed. Error: %v", err)
	}

	deployment, err := c.getLoadBalancerDeployment(getTestLoadBlancerName("testGatewayLabel"))
	if nil == deployment || nil != err {
		t.Fatalf("Unexpected error finding load balancer deployment '%v': %v, %v", "testGatewayLabel", deployment, err)
	}

	matchExpressions := deployment.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	if len(matchExpressions) != 2 {
		t.Fatalf("Unexpected matchExpression size: Expected: 2; Actual: %v", len(matchExpressions))
	}
	foundDedicatedGateway := false
	foundPublicVlan1 := false
	for _, matchExpression := range matchExpressions {
		if matchExpression.Key == "dedicated" {
			for _, value := range matchExpression.Values {
				if value == "gateway" {
					foundDedicatedGateway = true
				}
			}
		} else if matchExpression.Key == "publicVLAN" {
			for _, value := range matchExpression.Values {
				if value == "1" {
					foundPublicVlan1 = true
				}
			}
		}
	}

	if !foundDedicatedGateway || !foundPublicVlan1 {
		t.Fatalf("Node Affinity selector incorrect. foundDedicatedGateway = %v; foundPublicVlan1 = %v", foundDedicatedGateway, foundPublicVlan1)
	}

	// now, run a test to make sure load balancer deployments will be still scheduled to nodes with dedicated:gateway label
	// when also introducing the dedicated:edge label on other nodes
	n3.ObjectMeta.Labels = map[string]string{lbPublicVlanLabel: "1", lbPrivateVlanLabel: "5", lbDedicatedLabel: lbEdgeNodeValue}
	n4.ObjectMeta.Labels = map[string]string{lbPublicVlanLabel: "1", lbPrivateVlanLabel: "5", lbDedicatedLabel: lbEdgeNodeValue}
	n3, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), n3, metav1.UpdateOptions{})
	if len(n3.Labels) != 3 {
		t.Fatal("labels on node n3 weren't updated")
	}
	n4, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), n4, metav1.UpdateOptions{})
	if len(n4.Labels) != 3 {
		t.Fatal("labels on node n4 weren't updated")
	}

	service, _ = createTestLoadBalancerServiceIPVS("testGatewayLabel2", "192.168.10.31", true, true)
	service.Spec.LoadBalancerIP = "192.168.10.31"
	nodes = []*v1.Node{
		n1, n2, n3, n4,
	}
	status, err = c.EnsureLoadBalancer(context.Background(), "test", service, nodes)

	if err != nil && status == nil {
		t.Fatalf("Expected EnsureLoadBalancer to succeed. Error: %v", err)
	}

	deployment, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("testGatewayLabel2"))
	if nil == deployment || nil != err {
		t.Fatalf("Unexpected error finding load balancer deployment '%v': %v, %v", "testGatewayLabel2", deployment, err)
	}

	matchExpressions = deployment.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	if len(matchExpressions) != 2 {
		t.Fatalf("Unexpected matchExpression size: Expected: 2; Actual: %v", len(matchExpressions))
	}
	foundDedicatedGateway = false
	foundPublicVlan1 = false
	for _, matchExpression := range matchExpressions {
		if matchExpression.Key == "dedicated" {
			for _, value := range matchExpression.Values {
				if value == "gateway" {
					foundDedicatedGateway = true
				}
			}
		} else if matchExpression.Key == "publicVLAN" {
			for _, value := range matchExpression.Values {
				if value == "1" {
					foundPublicVlan1 = true
				}
			}
		}
	}

	if !foundDedicatedGateway || !foundPublicVlan1 {
		t.Fatalf("Node Affinity selector incorrect. foundDedicatedGateway = %v; foundPublicVlan1 = %v", foundDedicatedGateway, foundPublicVlan1)
	}

	// run EnsureLoadBalancer again on same service to ensure the update path doesn't move the load
	// balancer to other nodes
	status, err = c.EnsureLoadBalancer(context.Background(), "test", service, nodes)

	if err != nil && status == nil {
		t.Fatalf("Expected EnsureLoadBalancer to succeed. Error: %v", err)
	}

	deployment, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("testGatewayLabel2"))
	if nil == deployment || nil != err {
		t.Fatalf("Unexpected error finding load balancer deployment '%v': %v, %v", "testGatewayLabel2", deployment, err)
	}

	matchExpressions = deployment.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	if len(matchExpressions) != 2 {
		t.Fatalf("Unexpected matchExpression size: Expected: 2; Actual: %v", len(matchExpressions))
	}
	foundDedicatedGateway = false
	foundPublicVlan1 = false
	for _, matchExpression := range matchExpressions {
		if matchExpression.Key == "dedicated" {
			for _, value := range matchExpression.Values {
				if value == "gateway" {
					foundDedicatedGateway = true
				}
			}
		} else if matchExpression.Key == "publicVLAN" {
			for _, value := range matchExpression.Values {
				if value == "1" {
					foundPublicVlan1 = true
				}
			}
		}
	}

	if !foundDedicatedGateway || !foundPublicVlan1 {
		t.Fatalf("Node Affinity selector incorrect. foundDedicatedGateway = %v; foundPublicVlan1 = %v", foundDedicatedGateway, foundPublicVlan1)
	}

	// now, schedule to a private-only gateway node
	n1.ObjectMeta.Labels = map[string]string{lbPrivateVlanLabel: "2", lbDedicatedLabel: lbGatewayNodeValue}
	n2.ObjectMeta.Labels = map[string]string{lbPrivateVlanLabel: "2", lbDedicatedLabel: lbGatewayNodeValue}
	n3.ObjectMeta.Labels = map[string]string{lbPrivateVlanLabel: "2", lbDedicatedLabel: lbEdgeNodeValue}
	n4.ObjectMeta.Labels = map[string]string{lbPrivateVlanLabel: "2", lbDedicatedLabel: lbEdgeNodeValue}

	nodes = []*v1.Node{
		n1, n2, n3, n4,
	}
	for _, node := range nodes {
		node, _ = c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
		if len(node.Labels) != 2 {
			t.Fatal("labels on node weren't updated")
		}
	}

	service, _ = createTestLoadBalancerServiceIPVS("testGatewayLabelPrivate", "10.10.10.21", true, false)
	service.Spec.LoadBalancerIP = "10.10.10.21"
	status, err = c.EnsureLoadBalancer(context.Background(), "test", service, nodes)

	if err != nil && status == nil {
		t.Fatalf("Expected EnsureLoadBalancer to succeed. Error: %v", err)
	}

	deployment, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("testGatewayLabelPrivate"))
	if nil == deployment || nil != err {
		t.Fatalf("Unexpected error finding load balancer deployment '%v': %v, %v", "testGatewayLabelPrivate", deployment, err)
	}

	matchExpressions = deployment.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	if len(matchExpressions) != 2 {
		t.Fatalf("Unexpected matchExpression size: Expected: 2; Actual: %v", len(matchExpressions))
	}
	foundDedicatedGateway = false
	foundPrivateVlan2 := false
	for _, matchExpression := range matchExpressions {
		if matchExpression.Key == "dedicated" {
			for _, value := range matchExpression.Values {
				if value == "gateway" {
					foundDedicatedGateway = true
				}
			}
		} else if matchExpression.Key == "privateVLAN" {
			for _, value := range matchExpression.Values {
				if value == "2" {
					foundPrivateVlan2 = true
				}
			}
		}
	}

	if !foundDedicatedGateway || !foundPrivateVlan2 {
		t.Fatalf("Node Affinity selector incorrect. foundDedicatedGateway= %v; foundPublicVlan1 = %v", foundDedicatedGateway, foundPublicVlan1)
	}

	// now update the private service and make sure everything is still kosher
	status, err = c.EnsureLoadBalancer(context.Background(), "test", service, nodes)

	if err != nil && status == nil {
		t.Fatalf("Expected EnsureLoadBalancer to succeed. Error: %v", err)
	}

	deployment, err = c.getLoadBalancerDeployment(getTestLoadBlancerName("testGatewayLabelPrivate"))
	if nil == deployment || nil != err {
		t.Fatalf("Unexpected error finding load balancer deployment '%v': %v, %v", "testGatewayLabelPrivate", deployment, err)
	}

	matchExpressions = deployment.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
	if len(matchExpressions) != 2 {
		t.Fatalf("Unexpected matchExpression size: Expected: 2; Actual: %v", len(matchExpressions))
	}
	foundDedicatedGateway = false
	foundPrivateVlan2 = false
	for _, matchExpression := range matchExpressions {
		if matchExpression.Key == "dedicated" {
			for _, value := range matchExpression.Values {
				if value == "gateway" {
					foundDedicatedGateway = true
				}
			}
		} else if matchExpression.Key == "privateVLAN" {
			for _, value := range matchExpression.Values {
				if value == "2" {
					foundPrivateVlan2 = true
				}
			}
		}
	}

	if !foundDedicatedGateway || !foundPrivateVlan2 {
		t.Fatalf("Node Affinity selector incorrect. foundDedicatedGateway = %v; foundPublicVlan1 = %v", foundDedicatedGateway, foundPublicVlan1)
	}
}

func TestUpdateLoadBalancer(t *testing.T) {
	c, clusterName, _ := getTestCloud()

	calicoCMDCalled := 0
	// used to ensure the exec for calicoctl is redirected internally
	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		calicoCMDCalled++
		return cmd
	}

	servicePublic, cm := createTestLoadBalancerServiceIPVS("testIPVSDeleteCM", "192.168.10.50", true, true)
	n1, n2, _, _ := createTestCloudNodes()
	n3 := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "192.168.10.8",
			Labels: map[string]string{lbPublicVlanLabel: "1", lbPrivateVlanLabel: "2"},
		},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{
					Type:    v1.NodeInternalIP,
					Address: "192.168.10.8",
				},
			},
		},
	}
	nodes := []*v1.Node{n1, n2, n3}

	// Load balancer doesn't exist
	err := c.UpdateLoadBalancer(context.Background(), clusterName, getLoadBalancerService("doesntexist"), nodes)
	if nil != err {
		t.Fatalf("Unexpected error ensuring load balancer 'doesntexist' updated: %v", err)
	}

	// Duplicate load balancers exist
	err = c.UpdateLoadBalancer(context.Background(), clusterName, getLoadBalancerService("dup"), nodes)
	if nil == err {
		t.Fatalf("Unexpected load balancer 'dup' updated: %v", err)
	}

	err = c.UpdateLoadBalancer(context.Background(), clusterName, servicePublic, nodes)
	if nil != err {
		t.Fatalf("UpdateLoadBalancer failed: %v", err)
	}

	updatedCm, err := c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Get(context.TODO(), cm.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Unexpected error while attempting to retrieve LB configmap %v", cm.Name)
	}

	if cm.Data["nodes"] == updatedCm.Data["nodes"] {
		t.Fatalf("Unexpected error: configmap node contents unchanged %v", cm.Name)
	}
	if calicoCMDCalled != 1 {
		t.Fatalf("Expected to create the calico policy. Number of times Exec was called to create the Calico policy: %d", calicoCMDCalled)
	}

	// Verify we don't create the calico policy when creating a private LB
	servicePrivate, _ := createTestLoadBalancerServiceIPVS("testEdgeNodesWithEmptyToleration", "192.168.10.38", true, false)
	calicoCMDCalled = 0
	err = c.UpdateLoadBalancer(context.Background(), clusterName, servicePrivate, nodes)
	if nil != err {
		t.Fatalf("UpdateLoadBalancer failed: %v", err)
	}
	if calicoCMDCalled != 0 {
		t.Fatalf("Expected to not create the calico policy. Number of times Exec was called to create the Calico policy: %d", calicoCMDCalled)
	}
}

func TestEnsureLoadBalancerDeleted(t *testing.T) {
	var err error
	var status *v1.LoadBalancerStatus
	var exists bool

	// used to ensure the exec for calicoctl is redirected internally
	execCommand = func(command string, parms ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCalicoExecDummy", "--"}
		cs = append(cs, parms...)
		// #nosec G204 unit test code usage that wouldn't be exploited
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
		return cmd
	}

	c, clusterName, _ := getTestCloud()

	// Load balancer doesn't exist
	err = c.EnsureLoadBalancerDeleted(context.Background(), clusterName, getLoadBalancerService("doesntexist"))
	if nil != err {
		t.Fatalf("Unexpected error ensuring load balancer 'doesntexist' deleted: %v", err)
	}

	// Duplicate load balancers exist
	err = c.EnsureLoadBalancerDeleted(context.Background(), clusterName, getLoadBalancerService("dup"))
	if nil == err {
		t.Fatalf("Unexpected load balancer 'dup' deleted: %v", err)
	}

	// Ensure load balancer deleted
	err = c.EnsureLoadBalancerDeleted(context.Background(), clusterName, getLoadBalancerService("test"))
	if nil != err {
		t.Fatalf("Unexpected error ensuring load balancer 'test' deleted: %v", err)
	}
	status, exists, err = c.GetLoadBalancer(context.Background(), clusterName, getLoadBalancerService("test"))
	if nil != status || exists || nil != err {
		t.Fatalf("Unexpected load balancer 'test' found: %v, %v, %v", status, exists, err)
	}

	// Ensure load balancer without replicas deleted
	err = c.EnsureLoadBalancerDeleted(context.Background(), clusterName, getLoadBalancerService("noreplicas"))
	if nil != err {
		t.Fatalf("Unexpected error ensuring load balancer 'noreplicas' deleted: %v", err)
	}
	status, exists, err = c.GetLoadBalancer(context.Background(), clusterName, getLoadBalancerService("noreplicas"))
	if nil != status || exists || nil != err {
		t.Fatalf("Unexpected load balancer 'noreplicas' found: %v, %v, %v", status, exists, err)
	}

	// Ensure IPVS load balancer and configmap deleted
	testIPVSDeleteCMService := getLoadBalancerService("testIPVSDeleteCM")
	annotationMap := map[string]string{}
	annotationMap[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures] = lbFeatureIPVS
	testIPVSDeleteCMService.Annotations = annotationMap
	testIPVSDeleteCMService.Spec.LoadBalancerIP = "192.168.10.50"

	err = c.EnsureLoadBalancerDeleted(context.Background(), clusterName, testIPVSDeleteCMService)
	if nil != err {
		t.Fatalf("Unexpected error ensuring load balancer 'testIPVSDeleteCM' deleted: %v", err)
	}
	status, exists, err = c.GetLoadBalancer(context.Background(), clusterName, testIPVSDeleteCMService)
	if nil != status || exists || nil != err {
		t.Fatalf("Unexpected load balancer 'testIPVSDeleteCM' found: %v, %v, %v", status, exists, err)
	}
	cmName := lbDeploymentNamePrefix + getCloudProviderIPLabelValue("192.168.10.50")
	result, err := c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	if result != nil || err == nil {
		t.Fatalf("Unexpected error in ensuring deleted load balancer 'testIPVSDeleteCM'. Configmap found: %v", cmName)
	}

	// Test: Ensure IPVS private load balancer and configmap deleted
	testIPVSDeleteCMService = getLoadBalancerService("testIPVSDeleteCM")
	testIPVSDeleteCMService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	testIPVSDeleteCMService.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone] = "dal09"
	testIPVSDeleteCMService.Spec.LoadBalancerIP = "10.10.10.21"
	err = c.EnsureLoadBalancerDeleted(context.Background(), clusterName, testIPVSDeleteCMService)
	if nil != err {
		t.Fatalf("Unexpected error ensuring load balancer 'testIPVSDeleteCM' deleted: %v", err)
	}
	status, exists, err = c.GetLoadBalancer(context.Background(), clusterName, testIPVSDeleteCMService)
	if nil != status || exists || nil != err {
		t.Fatalf("Unexpected load balancer 'testIPVSDeleteCM' found: %v, %v, %v", status, exists, err)
	}
	cmName = lbDeploymentNamePrefix + getCloudProviderIPLabelValue("192.168.10.50")
	result, err = c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	if result != nil || err == nil {
		t.Fatalf("Unexpected error in ensuring deleted load balancer 'testIPVSDeleteCM'. Configmap found: %v", cmName)
	}
}

func TestFilterLoadBalancersFromServiceList(t *testing.T) {
	c, _, _ := getTestCloud()

	services, err := c.KubeClient.CoreV1().Services(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if nil != err {
		t.Fatalf("Failed to list load balancer services: %v", err)
	}

	num := len(services.Items)
	expectedNum := 12 // Count every service
	if expectedNum != num {
		t.Fatalf("The number of services: %v is not equal with the expected value: %v", num, expectedNum)
	}

	filterLoadBalancersFromServiceList(services)
	num = len(services.Items)
	expectedNum = 11 // Count of load balanacer services
	if expectedNum != num {
		t.Fatalf("The number of services: %v is not equal with the expected value: %v", num, expectedNum)
	}

	// Choose two "random" load balancers (4th and 9th) and set their load balancer class
	// Filter out these load balancer services
	services.Items[4].Spec.LoadBalancerClass = pointer.String("dummylb.io")
	services.Items[9].Spec.LoadBalancerClass = pointer.String("dummylb.io")
	filterLoadBalancersFromServiceList(services)
	num = len(services.Items)
	expectedNum = 9 // Count of load balancer services without load balancer class
	if expectedNum != num {
		t.Fatalf("The number of services: %v is not equal with the expected value: %v", num, expectedNum)
	}
}

func TestMonitorLoadBalancers(t *testing.T) {
	data := map[string]string{
		"atest":        "Fake error",
		"adoesntexist": "Fake error",
	}

	c, _, _ := getTestCloud()

	// Delete The service account to ensure we keep the same flow of the test
	err := c.KubeClient.CoreV1().Services(testNamespace).Delete(context.TODO(), "testIPVSDeleteCMCreate", metav1.DeleteOptions{})
	if nil != err {
		t.Fatalf("Unexpected error remove service account: %v, %v", lbDeploymentNamespace, "testIPVSDeleteCMCreate")
	}

	// Initial monitor should result in new errors.
	MonitorLoadBalancers(c, data)
	if 3 != len(data) || 0 != len(data["atest"]) || 0 == len(data["adup"]) || 0 == len(data["anoreplicas"]) {
		t.Fatalf("Unexpected load balancer monitor data: %v", data)
	}

	// Second monitor should result in warning events.
	MonitorLoadBalancers(c, data)
	if 3 != len(data) || 0 != len(data["atest"]) || 0 == len(data["adup"]) || 0 == len(data["anoreplicas"]) {
		t.Fatalf("Unexpected load balancer monitor data: %v", data)
	}

	data["testSourceIP"] = "Fake error"
	MonitorLoadBalancers(c, data)
	if 4 != len(data) || 0 != len(data["atest"]) || 0 == len(data["adup"]) || 0 == len(data["anoreplicas"]) || 0 == len(data["testSourceIP"]) {
		t.Fatalf("Unexpected load balancer monitor data: %v", data)
	}
}

func TestEnsureLoadBalancerSourceIp(t *testing.T) {
	c, clusterName, _ := getTestCloud()

	expectedServiceSelector := map[string]string{"tomIsCool": "true"}

	// Add dedicated=edge label to node2
	node, _ := c.KubeClient.CoreV1().Nodes().Get(context.TODO(), "192.168.10.6", metav1.GetOptions{})
	c.KubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
	nodeListOptions := metav1.ListOptions{LabelSelector: "privateVLAN=2"}
	nodes, _ := c.KubeClient.CoreV1().Nodes().List(context.TODO(), nodeListOptions)

	if len(nodes.Items) != 2 {
		t.Fatalf("Invalid number of nodes. Test needs 2 nodes to ensure 100 Percent MaxUnavailable pods is applied: %d", len(nodes.Items))
	}

	// Create a private Load Balancer - Enable local only traffic with ServiceExternalTrafficPolicyTypeLocal
	privateIPService := getLoadBalancerService("privateip")
	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	privateIPService.Spec.LoadBalancerIP = "10.10.10.21"
	privateIPService.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	privateIPService.Spec.Type = v1.ServiceTypeLoadBalancer
	privateIPService.Spec.Selector = expectedServiceSelector

	verifyLoadBalancerSourceIP(t, c, clusterName, privateIPService, "privateip", expectedServiceSelector)

	// Update a Service that has ServiceExternalTrafficPolicyTypeCluster and Deployment affinity set for pod affinity
	privateIPService.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeCluster
	verifyLoadBalancerSourceIPRemoved(t, c, clusterName, privateIPService, "privateip", map[string]string{}, len(nodes.Items))

	privateIPService.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	verifyLoadBalancerSourceIP(t, c, clusterName, privateIPService, "privateip", expectedServiceSelector)

	c.EnsureLoadBalancerDeleted(context.Background(), clusterName, privateIPService)

	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures] = lbFeatureIPVS
	verifyLoadBalancerSourceIPIPVS(t, c, clusterName, privateIPService, "privateip", len(nodes.Items))
}

func TestEnsureLoadBalancerSourceIpUpdate(t *testing.T) {

	c, clusterName, _ := getTestCloud()

	expectedServiceSelector := map[string]string{"tomIsCool": "true"}

	nodeListOptions := metav1.ListOptions{LabelSelector: "privateVLAN=2"}
	nodes, _ := c.KubeClient.CoreV1().Nodes().List(context.TODO(), nodeListOptions)

	if len(nodes.Items) != 2 {
		t.Fatalf("Invalid number of nodes. Test needs 2 nodes to ensure 100 Percent MaxUnavailable pods is applied: %d", len(nodes.Items))
	}

	// Create a private Load Balancer - Enable local only traffic with ServiceExternalTrafficPolicyTypeLocal
	privateIPService := getLoadBalancerService("testSourceIP")
	privateIPService.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	privateIPService.Spec.LoadBalancerIP = "192.168.10.36"
	privateIPService.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	privateIPService.Spec.Type = v1.ServiceTypeLoadBalancer
	privateIPService.Spec.Selector = expectedServiceSelector

	// Update a Service that has ServiceExternalTrafficPolicyTypeLocal and no affinity in deployment
	verifyLoadBalancerSourceIP(t, c, clusterName, privateIPService, "testSourceIP", expectedServiceSelector)

	// Update selector to verify Deployment is updated
	expectedServiceSelector = map[string]string{"tomIsReallyCool": "true", "tomIsNotCool": "false"}
	privateIPService.Spec.Selector = expectedServiceSelector
	verifyLoadBalancerSourceIP(t, c, clusterName, privateIPService, "testSourceIP", expectedServiceSelector)

	// Update selector to verify Deployment is updated
	privateIPService.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	expectedServiceSelector = map[string]string{"tomIsReallyCool": "true", "tomIsNotCool": "false"}
	privateIPService.Spec.Selector = expectedServiceSelector
	verifyLoadBalancerSourceIP(t, c, clusterName, privateIPService, "testSourceIP", expectedServiceSelector)

	// Update a Service that has ServiceExternalTrafficPolicyTypeCluster and Deployment affinity set for pod affinity
	privateIPService.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeCluster
	verifyLoadBalancerSourceIPRemoved(t, c, clusterName, privateIPService, "testSourceIP", map[string]string{}, len(nodes.Items))
	c.EnsureLoadBalancerDeleted(context.Background(), clusterName, privateIPService)

	// Create a private Load Balancer - Enable local only traffic with ServiceExternalTrafficPolicyTypeLocal
	privateIPService1 := getLoadBalancerService("privateipp")
	privateIPService1.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType] = "private"
	privateIPService1.Spec.LoadBalancerIP = "10.10.10.31"
	privateIPService1.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	privateIPService1.Spec.Type = v1.ServiceTypeLoadBalancer
	privateIPService1.Spec.Selector = expectedServiceSelector

	nodeListOptions1 := metav1.ListOptions{LabelSelector: "privateVLAN=5"}
	nodes1, _ := c.KubeClient.CoreV1().Nodes().List(context.TODO(), nodeListOptions1)

	if len(nodes1.Items) != 1 {
		t.Fatalf("Invalid number of nodes. Test needs 1 nodes to ensure 100 Percent MaxUnavailable pods is applied: %d", len(nodes1.Items))
	}
	// Update a Service that has ServiceExternalTrafficPolicyTypeLocal and no affinity in deployment
	verifyLoadBalancerSourceIP(t, c, clusterName, privateIPService1, "privateipp", expectedServiceSelector)

	// Update a Service that has ServiceExternalTrafficPolicyTypeCluster and Deployment affinity set for pod affinity
	privateIPService1.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeCluster
	verifyLoadBalancerSourceIPRemoved(t, c, clusterName, privateIPService1, "privateipp", map[string]string{}, len(nodes1.Items))
	c.EnsureLoadBalancerDeleted(context.Background(), clusterName, privateIPService1)

	// IPVS Tests
	privateIPService1.Annotations[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures] = lbFeatureIPVS
	privateIPService1.Spec.ExternalTrafficPolicy = v1.ServiceExternalTrafficPolicyTypeLocal
	verifyLoadBalancerSourceIPIPVS(t, c, clusterName, privateIPService1, "privateipp", len(nodes1.Items))

	// Update selector to verify Deployment is updated
	expectedServiceSelector = map[string]string{"tomIsReallyCool": "true", "tomIsNotCool": "false"}
	privateIPService1.Spec.Selector = expectedServiceSelector
	verifyLoadBalancerSourceIPIPVS(t, c, clusterName, privateIPService1, "privateipp", len(nodes1.Items))

}

func TestPodAffinityMatchLabelAndServiceSelectorEqual(t *testing.T) {
	checkRequestsOnlyLocalTraffic := func(requestsOnlyLocalTrafficBool bool, lbDeploymentPodAffinity *v1.PodAffinity, serviceSelector map[string]string) {
		res := podAffinityMatchLabelAndServiceSelectorEqual(lbDeploymentPodAffinity, serviceSelector)
		if res != requestsOnlyLocalTrafficBool {
			t.Errorf("Expected requests OnlyLocal traffic = %v, got %v",
				requestsOnlyLocalTrafficBool, res)
		}
	}

	// LB Deployment Match Labels is nil and serviceSelector is empty
	checkRequestsOnlyLocalTraffic(false, &v1.PodAffinity{}, map[string]string{})

	// LB Deployment Match Labels is set and serviceSelector is empty
	checkRequestsOnlyLocalTraffic(false, &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"tomsCool": "true",
					},
				},
				TopologyKey: v1.LabelHostname,
				Namespaces:  []string{},
			},
		},
	}, map[string]string{})

	// LB Deployment Match Labels is set and serviceSelector is set and they are equal
	checkRequestsOnlyLocalTraffic(true, &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"tomsCool": "true",
					},
				},
				TopologyKey: v1.LabelHostname,
				Namespaces:  []string{},
			},
		},
	}, map[string]string{
		"tomsCool": "true",
	})

	// LB Deployment Match Labels is set and serviceSelector is set and they are equal. But serviceSelector is in a different order
	checkRequestsOnlyLocalTraffic(true, &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"tomsCool1": "true",
						"tomsCool2": "true",
						"tomsCool3": "true",
						"tomsCool4": "true",
					},
				},
				TopologyKey: v1.LabelHostname,
				Namespaces:  []string{},
			},
		},
	}, map[string]string{
		"tomsCool3": "true",
		"tomsCool2": "true",
		"tomsCool4": "true",
		"tomsCool1": "true",
	})

	// LB Deployment Match Labels is set and serviceSelector is set and they are equal. But serviceSelector is in a different order
	checkRequestsOnlyLocalTraffic(false, &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"tomsCool5": "true",
						"tomsCool6": "true",
						"tomsCool7": "true",
						"tomsCool8": "true",
					},
				},
				TopologyKey: v1.LabelHostname,
				Namespaces:  []string{},
			}, {
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"tomsCool1": "true",
						"tomsCool2": "true",
						"tomsCool3": "true",
						"tomsCool4": "true",
					},
				},
				TopologyKey: v1.LabelHostname,
				Namespaces:  []string{},
			},
		},
	}, map[string]string{
		"tomsCool3": "true",
		"tomsCool2": "true",
		"tomsCool4": "true",
		"tomsCool1": "true",
	})

	// LB Deployment Match Labels is set and serviceSelector is set and the Match Labels are a subset of the serviceSelector
	checkRequestsOnlyLocalTraffic(false, &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"tomsCool1": "true",
						"tomsCool2": "true",
					},
				},
				TopologyKey: v1.LabelHostname,
				Namespaces:  []string{},
			},
		},
	}, map[string]string{
		"tomsCool3": "true",
		"tomsCool2": "true",
		"tomsCool4": "true",
		"tomsCool1": "true",
	})

	// LB Deployment Match Labels is set and serviceSelector is set and the serviceSelector are a subset of the Match Labels
	checkRequestsOnlyLocalTraffic(false, &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"tomsCool1": "true",
						"tomsCool2": "true",
						"tomsCool3": "true",
						"tomsCool4": "true",
					},
				},
				TopologyKey: v1.LabelHostname,
				Namespaces:  []string{},
			},
		},
	}, map[string]string{
		"tomsCool2": "true",
		"tomsCool1": "true",
	})

	// LB Deployment Match Labels and serviceSelector have the same keys but different values
	checkRequestsOnlyLocalTraffic(false, &v1.PodAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"tomsCool1": "true",
						"tomsCool2": "true",
					},
				},
				TopologyKey: v1.LabelHostname,
				Namespaces:  []string{},
			},
		},
	}, map[string]string{
		"tomsCool1": "true",
		"tomsCool2": "false",
	})
}
