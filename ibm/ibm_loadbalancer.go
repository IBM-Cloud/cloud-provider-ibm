/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2025 All Rights Reserved.
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
	"time"

	"cloud.ibm.com/cloud-provider-ibm/pkg/classic"
	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cloudprovider "k8s.io/cloud-provider"
)

const (
	lbVpcNextGenProvider = "g2"
)

// GetCloudProviderLoadBalancerName is a copy of the original Kubernetes function
// for generating a load balancer name. The original function is now deprecated
// so we are providing our own implementation here to continue generating load
// balancer names as we always have.
func GetCloudProviderLoadBalancerName(service *v1.Service) string {
	ret := "a" + string(service.UID)
	ret = strings.ReplaceAll(ret, "-", "")
	if len(ret) > 32 {
		ret = ret[:32]
	}
	return ret
}

// LoadBalancer returns a balancer interface. Also returns true if the interface is supported, false otherwise.
func (c *Cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	// Ensure that the monitor task is started.
	c.StartTask(MonitorLoadBalancers, time.Minute*5)
	return c, true
}

// GetLoadBalancerName returns the name of the load balancer. Implementations must treat the
// *v1.Service parameter as read-only and not modify it.
func (c *Cloud) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	// For a VPC cluster, we use a slightly different load balancer name
	if c.isProviderVpc() {
		return c.vpcGetLoadBalancerName(service)
	}
	return classic.GetCloudProviderLoadBalancerName(service)
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (*v1.LoadBalancerStatus, bool, error) {
	// Invoke VPC specific logic if this is a VPC cluster
	if c.isProviderVpc() {
		return c.VpcGetLoadBalancer(ctx, clusterName, service)
	}
	return c.ClassicCloud.GetLoadBalancer(ctx, clusterName, service)
}

// EnsureLoadBalancer creates a new load balancer 'name', or updates the existing one. Returns the status of the balancer
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {

	// Verify that the load balancer service configuration is supported.
	err := c.isServiceConfigurationSupported(service)
	if err != nil {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			fmt.Sprintf("Service configuration is not supported: %v", err),
		)
	}

	// Invoke VPC specific logic if this is a VPC cluster
	if c.isProviderVpc() {
		return c.VpcEnsureLoadBalancer(ctx, clusterName, service, nodes)
	}
	return c.ClassicCloud.EnsureLoadBalancer(ctx, clusterName, service, nodes)
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	// Invoke VPC specific logic if this is a VPC cluster
	if c.isProviderVpc() {
		return c.VpcUpdateLoadBalancer(ctx, clusterName, service, nodes)
	}
	return c.ClassicCloud.UpdateLoadBalancer(ctx, clusterName, service, nodes)
}

// EnsureLoadBalancerDeleted deletes the specified load balancer if it
// exists, returning nil if the load balancer specified either didn't exist or
// was successfully deleted.
// This construction is useful because many cloud providers' load balancers
// have multiple underlying components, meaning a Get could say that the LB
// doesn't exist even if some part of it is still laying around.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	// Invoke VPC specific logic if this is a VPC cluster
	if c.isProviderVpc() {
		return c.VpcEnsureLoadBalancerDeleted(ctx, clusterName, service)
	}
	return c.ClassicCloud.EnsureLoadBalancerDeleted(ctx, clusterName, service)
}

// Filter the services list to just contain the load balancers without defined load balancer class and nothing else.
func (c *Cloud) filterLoadBalancersFromServiceList(services *v1.ServiceList) {
	var lbItems []v1.Service
	for i := range services.Items {
		err := c.isServiceConfigurationSupported(&services.Items[i])
		if services.Items[i].Spec.Type == v1.ServiceTypeLoadBalancer &&
			services.Items[i].Spec.LoadBalancerClass == nil && err == nil {
			lbItems = append(lbItems, services.Items[i])
		}
	}
	services.Items = lbItems
}

// MonitorLoadBalancers monitors load balancer services to ensure that they
// are working properly. This is a cloud task run via ticker.
func MonitorLoadBalancers(c *Cloud, data map[string]string) {
	klog.Infof("Monitoring load balancers ...")

	// Prevent panic in unit test; occurs if the monitor thread interval is dropped to 15 sec
	if c.KubeClient == nil {
		return
	}

	// Monitor all load balancer services and generate a warning event for
	// each service that fails at least two consecutive monitors. A warning event
	// will also be generated to note that a service is restored after a failure.
	services, err := c.KubeClient.CoreV1().Services(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if nil != err {
		klog.Warningf("Failed to list load balancer services: %v", err)
		return
	}

	// Filtering out the services which type is not load balancer and also filtering out
	// the load balancer services which has got defined load balancer class.
	// The ServiceList struct was modified in place so there is no returning value
	c.filterLoadBalancersFromServiceList(services)

	// Invoke VPC specific logic if this is a VPC cluster
	if c.isProviderVpc() {
		c.VpcMonitorLoadBalancers(services, data)
	} else {
		c.ClassicCloud.MonitorLoadBalancers(services, data)
	}
}

func isProviderVpc(provider string) bool {
	return provider == lbVpcNextGenProvider
}

func (c *Cloud) isServiceConfigurationSupported(service *v1.Service) error {

	if c.isProviderVpc() && service.Spec.AllocateLoadBalancerNodePorts != nil && !*service.Spec.AllocateLoadBalancerNodePorts {
		return fmt.Errorf("NodePort allocation is required")
	}

	for _, port := range service.Spec.Ports {
		switch port.Protocol {
		case v1.ProtocolTCP, v1.ProtocolUDP:
		default:
			return fmt.Errorf("%s protocol", port.Protocol)
		}

	}
	return nil
}
