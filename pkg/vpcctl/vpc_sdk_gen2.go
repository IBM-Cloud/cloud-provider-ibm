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

package vpcctl

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"cloud.ibm.com/cloud-provider-ibm/pkg/klog"
	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/resourcemanagerv2"
	sdk "github.com/IBM/vpc-go-sdk/vpcv1"
)

// VpcSdkGen2 SDK methods
type VpcSdkGen2 struct {
	Client *sdk.VpcV1
	Config *ConfigVpc
}

// NewVpcSdkGen2 - create new SDK client
func NewVpcSdkGen2(c *ConfigVpc) (CloudVpcSdk, error) {
	authenticator := &core.IamAuthenticator{ApiKey: c.APIKeySecret, URL: c.tokenExchangeURL}
	client, err := sdk.NewVpcV1(&sdk.VpcV1Options{
		Authenticator: authenticator,
		URL:           c.endpointURL})
	if err != nil {
		return nil, fmt.Errorf("Failed to create SDK client: %v", err)
	}
	// Convert the resource group name to an ID
	if c.resourceGroupID == "" && c.ResourceGroupName != "" {
		err = convertResourceGroupNameToID(c)
		if err != nil {
			return nil, err
		}
	}
	// Default VPC timeout is 30 seconds.  This is not long enough for some operations.
	// Change the default timeout for all VPC REST calls to be 90 seconds.
	client.Service.Client.Timeout = time.Second * 90
	v := &VpcSdkGen2{
		Client: client,
		Config: c,
	}
	return v, nil
}

// convertResourceGroupNameToID - convert the resource group name into an ID
func convertResourceGroupNameToID(c *ConfigVpc) error {
	// Determine if URL for the resource manager
	url := resourcemanagerv2.DefaultServiceURL
	if strings.Contains(c.endpointURL, "iaasdev.cloud.ibm.com") {
		url = "https://resource-controller.test.cloud.ibm.com"
	}
	// Create resource manager client
	authenticator := &core.IamAuthenticator{ApiKey: c.APIKeySecret, URL: c.tokenExchangeURL}
	client, err := resourcemanagerv2.NewResourceManagerV2(&resourcemanagerv2.ResourceManagerV2Options{URL: url, Authenticator: authenticator})
	if err != nil {
		return fmt.Errorf("Failed to create resource manager v2 client: %v", err)
	}
	// Retrieve the resource group
	listOptions := &resourcemanagerv2.ListResourceGroupsOptions{AccountID: &c.AccountID, Name: &c.ResourceGroupName}
	list, response, err := client.ListResourceGroups(listOptions)
	if err != nil {
		if response != nil {
			klog.Infof("Response (%d): %+v", response.StatusCode, response.Result)
		}
		return fmt.Errorf("Failed to ListResourceGroups: %v", err)
	}
	if len(list.Resources) != 1 {
		return fmt.Errorf("%d resource groups match name: %s", len(list.Resources), c.ResourceGroupName)
	}
	resourceGroup := list.Resources[0]
	if resourceGroup.ID != nil {
		c.resourceGroupID = *resourceGroup.ID
	}
	return nil
}

// CreateLoadBalancer - create a load balancer
func (v *VpcSdkGen2) CreateLoadBalancer(lbName string, nodeList, poolList, subnetList []string, options *ServiceOptions) (*VpcLoadBalancer, error) {
	// For each of the ports in the Kubernetes service
	listeners := []sdk.LoadBalancerListenerPrototypeLoadBalancerContext{}
	pools := []sdk.LoadBalancerPoolPrototype{}
	for _, poolName := range poolList {
		poolNameFields, err := extractFieldsFromPoolName(poolName)
		if err != nil {
			return nil, err
		}
		pool := sdk.LoadBalancerPoolPrototype{
			Algorithm:     core.StringPtr(sdk.LoadBalancerPoolPrototypeAlgorithmRoundRobinConst),
			HealthMonitor: v.genLoadBalancerHealthMonitor(poolNameFields.NodePort, options.getHealthCheckNodePort()),
			Members:       v.genLoadBalancerMembers(poolNameFields.NodePort, nodeList),
			Name:          core.StringPtr(poolName),
			Protocol:      core.StringPtr(poolNameFields.Protocol),
			ProxyProtocol: core.StringPtr(sdk.LoadBalancerPoolProxyProtocolDisabledConst),
		}
		// Set proxy protocol if it was requested on the service annotation (we don't support v2)
		if options.isProxyProtocol() {
			pool.ProxyProtocol = core.StringPtr(sdk.LoadBalancerPoolProxyProtocolV1Const)
		}
		pools = append(pools, pool)
		listener := sdk.LoadBalancerListenerPrototypeLoadBalancerContext{
			ConnectionLimit: core.Int64Ptr(15000),
			DefaultPool:     &sdk.LoadBalancerPoolIdentityByName{Name: core.StringPtr(poolName)},
			Port:            core.Int64Ptr(int64(poolNameFields.Port)),
			Protocol:        core.StringPtr(sdk.LoadBalancerListenerPrototypeLoadBalancerContextProtocolTCPConst),
		}
		listeners = append(listeners, listener)
	}

	// Fill out the subnets where the VPC LB will be placed
	subnetIds := []sdk.SubnetIdentityIntf{}
	for _, subnet := range subnetList {
		subnetIds = append(subnetIds, &sdk.SubnetIdentity{ID: core.StringPtr(subnet)})
	}

	// Initialize all of the create options
	createOptions := &sdk.CreateLoadBalancerOptions{
		IsPublic:      core.BoolPtr(options.isPublic()),
		Subnets:       subnetIds,
		Listeners:     listeners,
		Name:          core.StringPtr(lbName),
		Pools:         pools,
		ResourceGroup: &sdk.ResourceGroupIdentity{ID: core.StringPtr(v.Config.resourceGroupID)},
	}

	// Create the VPC LB
	lb, response, err := v.Client.CreateLoadBalancer(createOptions)
	if err != nil {
		v.logResponseError(response)
		return nil, err
	}

	// Map the generated object back to the common format
	return v.mapLoadBalancer(*lb), nil
}

// CreateLoadBalancerListener - create a load balancer listener
func (v *VpcSdkGen2) CreateLoadBalancerListener(lbID, poolName, poolID string) (*VpcLoadBalancerListener, error) {
	// Extract values from poolName
	poolNameFields, err := extractFieldsFromPoolName(poolName)
	if err != nil {
		return nil, err
	}
	// Initialize the create options
	createOptions := &sdk.CreateLoadBalancerListenerOptions{
		LoadBalancerID:  core.StringPtr(lbID),
		ConnectionLimit: core.Int64Ptr(15000),
		Port:            core.Int64Ptr(int64(poolNameFields.Port)),
		Protocol:        core.StringPtr(poolNameFields.Protocol),
		DefaultPool:     &sdk.LoadBalancerPoolIdentity{ID: core.StringPtr(poolID)},
	}
	// Create the VPC LB listener
	listener, response, err := v.Client.CreateLoadBalancerListener(createOptions)
	if err != nil {
		v.logResponseError(response)
		return nil, err
	}
	// Map the generated object back to the common format
	return v.mapLoadBalancerListener(*listener), nil
}

// CreateLoadBalancerPool - create a load balancer pool
func (v *VpcSdkGen2) CreateLoadBalancerPool(lbID, poolName string, nodeList []string, options *ServiceOptions) (*VpcLoadBalancerPool, error) {
	// Extract values from poolName
	poolNameFields, err := extractFieldsFromPoolName(poolName)
	if err != nil {
		return nil, err
	}
	// Initialize the create options
	createOptions := &sdk.CreateLoadBalancerPoolOptions{
		LoadBalancerID: core.StringPtr(lbID),
		Algorithm:      core.StringPtr(sdk.CreateLoadBalancerPoolOptionsAlgorithmRoundRobinConst),
		HealthMonitor:  v.genLoadBalancerHealthMonitor(poolNameFields.NodePort, options.getHealthCheckNodePort()),
		Members:        v.genLoadBalancerMembers(poolNameFields.NodePort, nodeList),
		Name:           core.StringPtr(poolName),
		Protocol:       core.StringPtr(poolNameFields.Protocol),
	}
	pool, response, err := v.Client.CreateLoadBalancerPool(createOptions)
	if err != nil {
		v.logResponseError(response)
		return nil, err
	}
	// Map the generated object back to the common format
	return v.mapLoadBalancerPool(*pool), nil
}

// CreateLoadBalancerPoolMember - create a load balancer pool member
func (v *VpcSdkGen2) CreateLoadBalancerPoolMember(lbID, poolName, poolID, nodeID string) (*VpcLoadBalancerPoolMember, error) {
	// Extract values from poolName
	poolNameFields, err := extractFieldsFromPoolName(poolName)
	if err != nil {
		return nil, err
	}
	// Initialize the create options
	createOptions := &sdk.CreateLoadBalancerPoolMemberOptions{
		LoadBalancerID: core.StringPtr(lbID),
		PoolID:         core.StringPtr(poolID),
		Port:           core.Int64Ptr(int64(poolNameFields.NodePort)),
		Target:         &sdk.LoadBalancerPoolMemberTargetPrototypeIP{Address: core.StringPtr(nodeID)},
	}
	// Create the VPC LB pool member
	member, response, err := v.Client.CreateLoadBalancerPoolMember(createOptions)
	if err != nil {
		v.logResponseError(response)
		return nil, err
	}
	// Map the generated object back to the common format
	return v.mapLoadBalancerPoolMember(*member), nil
}

// DeleteLoadBalancer - delete the specified VPC load balancer
func (v *VpcSdkGen2) DeleteLoadBalancer(lbID string) error {
	response, err := v.Client.DeleteLoadBalancer(&sdk.DeleteLoadBalancerOptions{ID: &lbID})
	if err != nil {
		v.logResponseError(response)
	}
	return err
}

// DeleteLoadBalancerListener - delete the specified VPC load balancer listener
func (v *VpcSdkGen2) DeleteLoadBalancerListener(lbID, listenerID string) error {
	response, err := v.Client.DeleteLoadBalancerListener(&sdk.DeleteLoadBalancerListenerOptions{LoadBalancerID: &lbID, ID: &listenerID})
	if err != nil {
		v.logResponseError(response)
	}
	return err
}

// DeleteLoadBalancerPool - delete the specified VPC load balancer pool
func (v *VpcSdkGen2) DeleteLoadBalancerPool(lbID, poolID string) error {
	response, err := v.Client.DeleteLoadBalancerPool(&sdk.DeleteLoadBalancerPoolOptions{LoadBalancerID: &lbID, ID: &poolID})
	if err != nil {
		v.logResponseError(response)
	}
	return err
}

// DeleteLoadBalancerPoolMember - delete the specified VPC load balancer pool
func (v *VpcSdkGen2) DeleteLoadBalancerPoolMember(lbID, poolID, memberID string) error {
	response, err := v.Client.DeleteLoadBalancerPoolMember(&sdk.DeleteLoadBalancerPoolMemberOptions{LoadBalancerID: &lbID, PoolID: &poolID, ID: &memberID})
	if err != nil {
		v.logResponseError(response)
	}
	return err
}

// genLoadBalancerHealthMonitor - generate the VPC health monitor template for load balancer
func (v *VpcSdkGen2) genLoadBalancerHealthMonitor(nodePort, healthCheckPort int) *sdk.LoadBalancerPoolHealthMonitorPrototype {
	// Define health monitor for load balancer.
	//
	// The Delay, MaxRetries, and Timeout values listed below are the default values that are selected when
	// a load balancer is created in the VPC UI.  These values may need to be adjusted for IKS clusters.
	healthMonitor := &sdk.LoadBalancerPoolHealthMonitorPrototype{
		Delay:      core.Int64Ptr(5),
		MaxRetries: core.Int64Ptr(2),
		Port:       core.Int64Ptr(int64(nodePort)),
		Timeout:    core.Int64Ptr(2),
		Type:       core.StringPtr(sdk.LoadBalancerPoolHealthMonitorPrototypeTypeTCPConst),
	}

	// If the service has: "externalTrafficPolicy: local", then set the health check to be HTTP
	if healthCheckPort > 0 {
		healthMonitor.Port = core.Int64Ptr(int64(healthCheckPort))
		healthMonitor.Type = core.StringPtr(sdk.LoadBalancerPoolHealthMonitorPrototypeTypeHTTPConst)
		healthMonitor.URLPath = core.StringPtr("/")
	}
	return healthMonitor
}

// genLoadBalancerHealthMonitorUpdate - generate the VPC health monitor update template for load balancer pool
func (v *VpcSdkGen2) genLoadBalancerHealthMonitorUpdate(nodePort, healthCheckPort int) *sdk.LoadBalancerPoolHealthMonitorPatch {
	// Define health monitor for load balancer.
	//
	// The Delay, MaxRetries, and Timeout values listed below are the default values that are selected when
	// a load balancer is created in the VPC UI.  These values may need to be adjusted for IKS clusters.
	healthMonitor := &sdk.LoadBalancerPoolHealthMonitorPatch{
		Delay:      core.Int64Ptr(5),
		MaxRetries: core.Int64Ptr(2),
		Port:       core.Int64Ptr(int64(nodePort)),
		Timeout:    core.Int64Ptr(2),
		Type:       core.StringPtr(sdk.LoadBalancerPoolHealthMonitorPrototypeTypeTCPConst),
	}

	// If the service has: "externalTrafficPolicy: local", then set the health check to be HTTP
	if healthCheckPort > 0 {
		healthMonitor.Port = core.Int64Ptr(int64(healthCheckPort))
		healthMonitor.Type = core.StringPtr(sdk.LoadBalancerPoolHealthMonitorPrototypeTypeHTTPConst)
		healthMonitor.URLPath = core.StringPtr("/")
	}
	return healthMonitor
}

// genLoadBalancerMembers - generate the VPC member template for load balancer
func (v *VpcSdkGen2) genLoadBalancerMembers(nodePort int, nodeList []string) []sdk.LoadBalancerPoolMemberPrototype {
	// Create list of backend nodePorts on each of the nodes
	members := []sdk.LoadBalancerPoolMemberPrototype{}
	for _, node := range nodeList {
		member := sdk.LoadBalancerPoolMemberPrototype{Port: core.Int64Ptr(int64(nodePort))}
		member.Target = &sdk.LoadBalancerPoolMemberTargetPrototypeIP{Address: core.StringPtr(node)}
		members = append(members, member)
	}
	return members
}

// GetLoadBalancer - get a specific load balancer
func (v *VpcSdkGen2) GetLoadBalancer(lbID string) (*VpcLoadBalancer, error) {
	lb, response, err := v.Client.GetLoadBalancer(&sdk.GetLoadBalancerOptions{ID: &lbID})
	if err != nil {
		v.logResponseError(response)
		return nil, err
	}
	return v.mapLoadBalancer(*lb), nil
}

// GetSubnet - get a specific subnet
func (v *VpcSdkGen2) GetSubnet(subnetID string) (*VpcSubnet, error) {
	subnet, response, err := v.Client.GetSubnet(&sdk.GetSubnetOptions{ID: &subnetID})
	if err != nil {
		v.logResponseError(response)
		return nil, err
	}
	return v.mapSubnet(*subnet), nil
}

// ListLoadBalancers - return list of load balancers
func (v *VpcSdkGen2) ListLoadBalancers() ([]*VpcLoadBalancer, error) {
	lbs := []*VpcLoadBalancer{}
	var start *string
	for {
		list, response, err := v.Client.ListLoadBalancers(&sdk.ListLoadBalancersOptions{Start: start})
		if err != nil {
			v.logResponseError(response)
			return lbs, err
		}
		for _, item := range list.LoadBalancers {
			lbs = append(lbs, v.mapLoadBalancer(item))
		}
		// Check to see if more subnets need to be retrieved
		if list.Next == nil || list.Next.Href == nil {
			break
		}
		// We need to pull out the "start" query value and re-issue the call to RIaaS to get the next block of objects
		u, err := url.Parse(*list.Next.Href)
		if err != nil {
			return lbs, err
		}
		qryArgs := u.Query()
		start = core.StringPtr(qryArgs.Get("start"))
	}
	return lbs, nil
}

// ListLoadBalancerListeners - return list of load balancer listeners
func (v *VpcSdkGen2) ListLoadBalancerListeners(lbID string) ([]*VpcLoadBalancerListener, error) {
	listeners := []*VpcLoadBalancerListener{}
	list, response, err := v.Client.ListLoadBalancerListeners(&sdk.ListLoadBalancerListenersOptions{LoadBalancerID: &lbID})
	if err != nil {
		v.logResponseError(response)
		return listeners, err
	}
	for _, item := range list.Listeners {
		listeners = append(listeners, v.mapLoadBalancerListener(item))
	}
	return listeners, nil
}

// ListLoadBalancerPools - return list of load balancer pools
func (v *VpcSdkGen2) ListLoadBalancerPools(lbID string) ([]*VpcLoadBalancerPool, error) {
	pools := []*VpcLoadBalancerPool{}
	list, response, err := v.Client.ListLoadBalancerPools(&sdk.ListLoadBalancerPoolsOptions{LoadBalancerID: &lbID})
	if err != nil {
		v.logResponseError(response)
		return pools, err
	}
	for _, item := range list.Pools {
		pool := v.mapLoadBalancerPool(item)
		members, err := v.ListLoadBalancerPoolMembers(lbID, pool.ID)
		if err != nil {
			return pools, err
		}
		pool.Members = members
		pools = append(pools, pool)
	}
	return pools, nil
}

// ListLoadBalancerPoolMembers - return list of load balancer pool members
func (v *VpcSdkGen2) ListLoadBalancerPoolMembers(lbID, poolID string) ([]*VpcLoadBalancerPoolMember, error) {
	members := []*VpcLoadBalancerPoolMember{}
	list, response, err := v.Client.ListLoadBalancerPoolMembers(&sdk.ListLoadBalancerPoolMembersOptions{LoadBalancerID: &lbID, PoolID: &poolID})
	if err != nil {
		v.logResponseError(response)
		return members, err
	}
	for _, item := range list.Members {
		members = append(members, v.mapLoadBalancerPoolMember(item))
	}
	return members, nil
}

// ListSubnets - return list of subnets
func (v *VpcSdkGen2) ListSubnets() ([]*VpcSubnet, error) {
	subnets := []*VpcSubnet{}
	// Default quota limitation on account:
	//   - VPCs / region: 10
	//   - Subnets / VPC: 15
	// Without ever altering the quotas on the account, a single region can have: 10 x 15 = 150 subnets
	// Default limit on subnets returned from VPC in a single call: 50  (maximum = 100)
	// Since there is no way to filter the subnet results, pagination will need to be used.
	var start *string
	for {
		list, response, err := v.Client.ListSubnets(&sdk.ListSubnetsOptions{Start: start})
		if err != nil {
			v.logResponseError(response)
			return subnets, err
		}
		for _, item := range list.Subnets {
			subnets = append(subnets, v.mapSubnet(item))
		}
		// Check to see if more subnets need to be retrieved
		if list.Next == nil || list.Next.Href == nil {
			break
		}
		// The list.Next.Href value will be set to something like:
		//   "https://us-south.iaas.cloud.ibm.com/v1/subnets?limit=50&start=0717-52e6fbc8-e4e3-4699-87aa-aa33a8e841a7"
		// We need to pull out the "start" query value and re-issue the call to RIaaS to get the next block of objects
		u, err := url.Parse(*list.Next.Href)
		if err != nil {
			return subnets, err
		}
		qryArgs := u.Query()
		start = core.StringPtr(qryArgs.Get("start"))
	}
	return subnets, nil
}

// logResponseError - write the response details to stdout so it will appear in logs
func (v *VpcSdkGen2) logResponseError(response *core.DetailedResponse) {
	if response != nil {
		klog.Infof("Response (%d): %+v", response.StatusCode, response.Result)
	}
}

// mapLoadBalancer - map the LoadBalancer to generic format
func (v *VpcSdkGen2) mapLoadBalancer(item sdk.LoadBalancer) *VpcLoadBalancer {
	lb := &VpcLoadBalancer{
		SdkObject:          item,
		CreatedAt:          SafePointerDate(item.CreatedAt),
		Hostname:           SafePointerString(item.Hostname),
		ID:                 SafePointerString(item.ID),
		IsPublic:           SafePointerBool(item.IsPublic),
		Name:               SafePointerString(item.Name),
		OperatingStatus:    SafePointerString(item.OperatingStatus),
		ProvisioningStatus: SafePointerString(item.ProvisioningStatus),
	}
	// Listener IDs
	for _, listenerRef := range item.Listeners {
		lb.ListenerIDs = append(lb.ListenerIDs, SafePointerString(listenerRef.ID))
	}
	// Pools
	for _, poolRef := range item.Pools {
		lb.Pools = append(lb.Pools, VpcObjectReference{ID: SafePointerString(poolRef.ID), Name: SafePointerString(poolRef.Name)})
	}
	// Private IPs
	for _, item := range item.PrivateIps {
		lb.PrivateIps = append(lb.PrivateIps, SafePointerString(item.Address))
	}
	sort.Strings(lb.PrivateIps)
	// Profile
	if item.Profile != nil {
		lb.ProfileFamily = SafePointerString(item.Profile.Family)
	}
	// Public IPs
	for _, item := range item.PublicIps {
		lb.PublicIps = append(lb.PublicIps, SafePointerString(item.Address))
	}
	sort.Strings(lb.PublicIps)
	// Resource Group
	if item.ResourceGroup != nil {
		lb.ResourceGroup = VpcObjectReference{ID: SafePointerString(item.ResourceGroup.ID), Name: SafePointerString(item.ResourceGroup.Name)}
	}
	// Subnets
	for _, subnetRef := range item.Subnets {
		lb.Subnets = append(lb.Subnets, VpcObjectReference{ID: SafePointerString(subnetRef.ID), Name: SafePointerString(subnetRef.Name)})
	}
	return lb
}

// mapLoadBalancerListener - map the LoadBalancerListener to generic format
func (v *VpcSdkGen2) mapLoadBalancerListener(item sdk.LoadBalancerListener) *VpcLoadBalancerListener {
	listener := &VpcLoadBalancerListener{
		ConnectionLimit:    SafePointerInt64(item.ConnectionLimit),
		ID:                 SafePointerString(item.ID),
		Port:               SafePointerInt64(item.Port),
		Protocol:           SafePointerString(item.Protocol),
		ProvisioningStatus: SafePointerString(item.ProvisioningStatus),
	}
	if item.DefaultPool != nil {
		listener.DefaultPool = VpcObjectReference{ID: SafePointerString(item.DefaultPool.ID), Name: SafePointerString(item.DefaultPool.Name)}
	}
	return listener
}

// mapLoadBalancerPool - map the LoadBalancerPool to generic format
func (v *VpcSdkGen2) mapLoadBalancerPool(item sdk.LoadBalancerPool) *VpcLoadBalancerPool {
	pool := &VpcLoadBalancerPool{
		Algorithm:          SafePointerString(item.Algorithm),
		ID:                 SafePointerString(item.ID),
		Name:               SafePointerString(item.Name),
		Protocol:           SafePointerString(item.Protocol),
		ProvisioningStatus: SafePointerString(item.ProvisioningStatus),
		ProxyProtocol:      SafePointerString(item.ProxyProtocol),
		SessionPersistence: "None",
	}
	if item.HealthMonitor != nil {
		pool.HealthMonitor = v.mapLoadBalancerPoolHealthMonitor(*item.HealthMonitor)
	}
	for _, memberRef := range item.Members {
		pool.Members = append(pool.Members, &VpcLoadBalancerPoolMember{ID: SafePointerString(memberRef.ID)})
	}
	if item.SessionPersistence != nil {
		pool.SessionPersistence = SafePointerString(item.SessionPersistence.Type)
	}
	return pool
}

// mapLoadBalancerPoolHealthMonitor - map the LoadBalancerPoolHealthMonitor to generic format
func (v *VpcSdkGen2) mapLoadBalancerPoolHealthMonitor(item sdk.LoadBalancerPoolHealthMonitor) VpcLoadBalancerPoolHealthMonitor {
	healthMonitor := VpcLoadBalancerPoolHealthMonitor{
		Delay:      SafePointerInt64(item.Delay),
		MaxRetries: SafePointerInt64(item.MaxRetries),
		Port:       SafePointerInt64(item.Port),
		Timeout:    SafePointerInt64(item.Timeout),
		Type:       SafePointerString(item.Type),
		URLPath:    SafePointerString(item.URLPath),
	}
	return healthMonitor
}

// mapLoadBalancerPoolMember - map the LoadBalancerPoolMember to generic format
func (v *VpcSdkGen2) mapLoadBalancerPoolMember(item sdk.LoadBalancerPoolMember) *VpcLoadBalancerPoolMember {
	member := &VpcLoadBalancerPoolMember{
		Health:             SafePointerString(item.Health),
		ID:                 SafePointerString(item.ID),
		Port:               SafePointerInt64(item.Port),
		ProvisioningStatus: SafePointerString(item.ProvisioningStatus),
		Weight:             SafePointerInt64(item.Weight),
	}
	if item.Target != nil {
		member.TargetIPAddress = SafePointerString(item.Target.(*sdk.LoadBalancerPoolMemberTarget).Address)
		member.TargetInstanceID = SafePointerString(item.Target.(*sdk.LoadBalancerPoolMemberTarget).ID)
	}
	return member
}

// mapSubnet - map the Subnet to generic format
func (v *VpcSdkGen2) mapSubnet(item sdk.Subnet) *VpcSubnet {
	subnet := &VpcSubnet{
		SdkObject:                 item,
		AvailableIpv4AddressCount: SafePointerInt64(item.AvailableIpv4AddressCount),
		CreatedAt:                 SafePointerDate(item.CreatedAt),
		ID:                        SafePointerString(item.ID),
		IPVersion:                 SafePointerString(item.IPVersion),
		Ipv4CidrBlock:             SafePointerString(item.Ipv4CIDRBlock),
		Name:                      SafePointerString(item.Name),
		Status:                    SafePointerString(item.Status),
		TotalIpv4AddressCount:     SafePointerInt64(item.TotalIpv4AddressCount),
	}
	// NetworkACL
	if item.NetworkACL != nil {
		subnet.NetworkACL = VpcObjectReference{ID: SafePointerString(item.NetworkACL.ID), Name: SafePointerString(item.NetworkACL.Name)}
	}
	// PublicGateway
	if item.PublicGateway != nil {
		subnet.PublicGateway = VpcObjectReference{ID: SafePointerString(item.PublicGateway.ID), Name: SafePointerString(item.PublicGateway.Name)}
	}
	// Resource Group
	if item.ResourceGroup != nil {
		subnet.ResourceGroup = VpcObjectReference{ID: SafePointerString(item.ResourceGroup.ID), Name: SafePointerString(item.ResourceGroup.Name)}
	}
	// VPC
	if item.VPC != nil {
		subnet.Vpc = VpcObjectReference{ID: SafePointerString(item.VPC.ID), Name: SafePointerString(item.VPC.Name)}
	}
	// Zone
	if item.Zone != nil {
		subnet.Zone = SafePointerString(item.Zone.Name)
	}
	return subnet
}

// ReplaceLoadBalancerPoolMembers - update a load balancer pool members
func (v *VpcSdkGen2) ReplaceLoadBalancerPoolMembers(lbID, poolName, poolID string, nodeList []string) ([]*VpcLoadBalancerPoolMember, error) {
	// Extract values from poolName
	poolNameFields, err := extractFieldsFromPoolName(poolName)
	if err != nil {
		return nil, err
	}
	// Initialize the create options
	replaceOptions := &sdk.ReplaceLoadBalancerPoolMembersOptions{
		LoadBalancerID: core.StringPtr(lbID),
		PoolID:         core.StringPtr(poolID),
		Members:        v.genLoadBalancerMembers(poolNameFields.NodePort, nodeList),
	}
	// Update the VPC LB pool member
	list, response, err := v.Client.ReplaceLoadBalancerPoolMembers(replaceOptions)
	if err != nil {
		v.logResponseError(response)
		return nil, err
	}
	// Map the generated object back to the common format
	members := []*VpcLoadBalancerPoolMember{}
	for _, item := range list.Members {
		members = append(members, v.mapLoadBalancerPoolMember(item))
	}
	return members, nil
}

// UpdateLoadBalancerPool - update a load balancer pool
func (v *VpcSdkGen2) UpdateLoadBalancerPool(lbID, newPoolName string, existingPool *VpcLoadBalancerPool, options *ServiceOptions) (*VpcLoadBalancerPool, error) {
	// Extract values from poolName
	poolNameFields, err := extractFieldsFromPoolName(newPoolName)
	if err != nil {
		return nil, err
	}
	proxyProtocolRequested := options.isProxyProtocol()
	updatePool := &sdk.LoadBalancerPoolPatch{
		// Algorithm:  core.StringPtr(algorithm),
		HealthMonitor: v.genLoadBalancerHealthMonitorUpdate(poolNameFields.NodePort, options.getHealthCheckNodePort()),
		Name:          core.StringPtr(newPoolName),
	}
	if proxyProtocolRequested && existingPool.ProxyProtocol != sdk.LoadBalancerPoolProxyProtocolV1Const {
		updatePool.ProxyProtocol = core.StringPtr(sdk.LoadBalancerPoolProxyProtocolV1Const)
	}
	if !proxyProtocolRequested && existingPool.ProxyProtocol != sdk.LoadBalancerPoolProxyProtocolDisabledConst {
		updatePool.ProxyProtocol = core.StringPtr(sdk.LoadBalancerPoolProxyProtocolDisabledConst)
	}
	updatePatch, err := updatePool.AsPatch()
	if err != nil {
		return nil, err
	}
	// Initialize the update pool options
	updateOptions := &sdk.UpdateLoadBalancerPoolOptions{
		LoadBalancerID:        core.StringPtr(lbID),
		ID:                    core.StringPtr(existingPool.ID),
		LoadBalancerPoolPatch: updatePatch,
	}
	// Update the VPC LB pool
	pool, response, err := v.Client.UpdateLoadBalancerPool(updateOptions)
	if err != nil {
		v.logResponseError(response)
		return nil, err
	}

	// Map the generated object back to the common format
	return v.mapLoadBalancerPool(*pool), nil
}
