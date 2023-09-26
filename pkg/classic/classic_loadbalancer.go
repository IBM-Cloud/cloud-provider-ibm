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

package classic

import (
	"context"
	"errors"
	"strings"

	v1 "k8s.io/api/core/v1"
)

// GetCloudProviderLoadBalancerName is a copy of the original Kubernetes function
// for generating a load balancer name.
func GetCloudProviderLoadBalancerName(service *v1.Service) string {
	ret := "a" + string(service.UID)
	ret = strings.ReplaceAll(ret, "-", "")
	if len(ret) > 32 {
		ret = ret[:32]
	}
	return ret
}

// GetLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
func (c *Cloud) GetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (*v1.LoadBalancerStatus, bool, error) {
	return nil, false, errors.New("cloud provider does not support GetLoadBalancer interface")
}

// EnsureLoadBalancer creates a new load balancer 'name', or updates the existing one. Returns the status of the balancer
func (c *Cloud) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	return nil, errors.New("cloud provider does not support EnsureLoadBalancer interface")
}

// UpdateLoadBalancer updates hosts under the specified load balancer.
func (c *Cloud) UpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	return errors.New("cloud provider does not support UpdateLoadBalancer interface")
}

// EnsureLoadBalancerDeleted deletes the specified load balancer if it
// exists, returning nil if the load balancer specified either didn't exist or
// was successfully deleted.
func (c *Cloud) EnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	return errors.New("cloud provider does not support EnsureLoadBalancerDeleted interface")
}

// MonitorLoadBalancers monitors load balancer services to ensure that they
// are working properly.
func (c *Cloud) MonitorLoadBalancers(services *v1.ServiceList, data map[string]string) {
}
