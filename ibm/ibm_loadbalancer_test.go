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
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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

func TestFilterLoadBalancersFromServiceList(t *testing.T) {
	c := &Cloud{Config: &CloudConfig{Prov: Provider{}}}
	s := v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "tcp-service"},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeNodePort,
			Ports: []v1.ServicePort{{
				Port:     80,
				Protocol: v1.ProtocolTCP,
			}},
		},
	}
	services := &v1.ServiceList{Items: []v1.Service{s}}

	// Filter should not remove node port service
	c.filterLoadBalancersFromServiceList(services)
	if len(services.Items) != 0 {
		t.Fatalf("Node port service was not filtered when it should have been")
	}

	// Filter should keep TCP load balancer services
	s.Spec.Type = v1.ServiceTypeLoadBalancer
	services = &v1.ServiceList{Items: []v1.Service{s}}
	c.filterLoadBalancersFromServiceList(services)
	if len(services.Items) != 1 {
		t.Fatalf("The TCP service was filtered when it should not have been")
	}

	// Filter out TCP load balancer when loadBalancerClass is specified
	class := "dummy"
	s.Spec.LoadBalancerClass = &class
	services = &v1.ServiceList{Items: []v1.Service{s}}
	c.filterLoadBalancersFromServiceList(services)
	if len(services.Items) != 0 {
		t.Fatalf("Load balancer class was not filtered when it should have been")
	}

	// Filter out non-TCP/UDP load balancer
	s.Spec.LoadBalancerClass = nil
	s.Spec.Ports[0].Protocol = v1.ProtocolSCTP
	services = &v1.ServiceList{Items: []v1.Service{s}}
	c.filterLoadBalancersFromServiceList(services)
	if len(services.Items) != 0 {
		t.Fatalf("SCTP load balancer was not filtered when it should have been")
	}

	// Allocate node port == "false" should NOT be filtered if classic
	AllocateLoadBalancerNodePorts := false
	s.Spec.LoadBalancerClass = nil
	s.Spec.AllocateLoadBalancerNodePorts = &AllocateLoadBalancerNodePorts
	s.Spec.Ports[0].Protocol = v1.ProtocolTCP
	services = &v1.ServiceList{Items: []v1.Service{s}}
	c.filterLoadBalancersFromServiceList(services)
	if len(services.Items) != 1 {
		t.Fatalf("Load balancer with (allocate node ports == false) was filtered when it should not have been")
	}

	// Allocate node port == "false" should be filtered if VPC
	s.Spec.AllocateLoadBalancerNodePorts = &AllocateLoadBalancerNodePorts
	services = &v1.ServiceList{Items: []v1.Service{s}}
	c.Config.Prov.ProviderType = lbVpcNextGenProvider
	c.filterLoadBalancersFromServiceList(services)
	if len(services.Items) != 0 {
		t.Fatalf("Load balancer with (allocate node ports == false) was not filtered when it should not have been")
	}
}

func TestIsProviderVpc(t *testing.T) {
	if isProviderVpc("") == true {
		t.Fatalf("isProviderVpc did not return false for empty string")
	}

	if isProviderVpc(lbVpcNextGenProvider) == false {
		t.Fatalf("isProviderVpc did not return true for 'g2'")
	}
}
