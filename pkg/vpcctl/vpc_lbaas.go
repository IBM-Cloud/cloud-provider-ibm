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
	"strings"
	"time"

	"cloud.ibm.com/cloud-provider-ibm/pkg/klog"
	v1 "k8s.io/api/core/v1"
)

const (
	actionCreateListener     = "CREATE-LISTENER"
	actionCreatePool         = "CREATE-POOL"
	actionCreatePoolMember   = "CREATE-POOL-MEMBER"
	actionDeleteListener     = "DELETE-LISTENER"
	actionDeletePool         = "DELETE-POOL"
	actionDeletePoolMember   = "DELETE-POOL-MEMBER"
	actionReplacePoolMembers = "REPLACE-POOL-MEMBERS"
	actionUpdatePool         = "UPDATE-POOL"

	poolToBeDeleted = "POOL-TO-BE-DELETED"
)

// checkForMultiplePoolMemberUpdates - replace multiple CREATE-POOL-MEMBER / DELETE-POOL-MEMBER actions with a single REPLACE-POOL-MEMBERS
//
// Each time that a CREATE-POOL-MEMBER or DELETE-POOL-MEMBER operation needs to be done against an existing LB it takes 30 seconds.
// If there are multiple of these operations queued up for a given LB pool, it is more efficient to do a single REPLACE-POOL-MEMBERS.
// In the general case, nodes being added/removed from the cluster, the scenario of multiple create/delete operations on a single pool
// will not occur that often. The case in which multiple create/delete pool members will most occur is when the service annotations are
// updated on the LB such that the pool members needs to get adjusted.
//
// Example: 9 node cluster, spread across 3 zones (A, B, and C), with 3 nodes in each zone. The service is updated with the "zone" annotation
// which states only Zone-A should be allowed. The 6 pool members in the other zones need to be deleted from the pool. The 6 delete
// operations (30 sec/each) would take roughly 3 minutes. A single REPLACE-POOL-MEMBERS would only take 30 seconds.
func (c *CloudVpc) checkForMultiplePoolMemberUpdates(updatesRequired []string) []string {
	// Determine how many pool member updates are being done to each of the load balancer pools
	poolUpdates := map[string]int{}
	for _, update := range updatesRequired {
		updateArgs := strings.Fields(update)
		cmd := updateArgs[0]
		poolName := updateArgs[1]
		if cmd == actionCreatePoolMember || cmd == actionDeletePoolMember {
			poolUpdates[poolName]++
		}
	}

	// Check to see if there are any pools with multiple pool member updates needed
	filterNeeded := false
	for _, count := range poolUpdates {
		if count > 1 {
			filterNeeded = true
		}
	}
	// No filtering needs to be done.  Return original list
	if !filterNeeded {
		return updatesRequired
	}

	// We need to regenerate the updatesRequired array and replace the first pool member update with
	// an REPLACE-POOL-MEMBERS and then delete the rest of the pool member updates for that specific pool.
	// All other update operations for the LB need to be kept.
	filteredUpdates := []string{}
	for _, update := range updatesRequired {
		updateArgs := strings.Fields(update)
		cmd := updateArgs[0]
		if cmd != actionCreatePoolMember && cmd != actionDeletePoolMember {
			// Keep all non-pool member update operations
			filteredUpdates = append(filteredUpdates, update)
			continue
		}
		poolName := updateArgs[1]
		poolID := updateArgs[2]
		switch {
		case poolUpdates[poolName] == 1:
			// If this is the only pool member update to this pool, then there is no need to change the update
			filteredUpdates = append(filteredUpdates, update)
		case poolUpdates[poolName] > 1:
			// If there are multiple pool member updates to this pool and this is the first one that we have found,
			// replace this update with an REPLACE-POOL-MEMBERS and set the count to 0 so that all other pool member
			// updates for this pool will be ignored.
			filteredUpdates = append(filteredUpdates, fmt.Sprintf("%s %s %s", actionReplacePoolMembers, poolName, poolID))
			poolUpdates[poolName] = 0
		}
	}
	return filteredUpdates
}

// checkListenersForExtPortAddedToService - check to see if we have existing listener for the specified Kube service
func (c *CloudVpc) checkListenersForExtPortAddedToService(updatesRequired []string, listeners []*VpcLoadBalancerListener, servicePort v1.ServicePort) []string {
	for _, listener := range listeners {
		if c.isServicePortEqualListener(servicePort, listener) {
			// Found an existing listener for the external port, no additional update needed
			return updatesRequired
		}
	}
	// Listener for this service port was not found. Create the listener
	poolName := genLoadBalancerPoolName(servicePort)
	updatesRequired = append(updatesRequired, fmt.Sprintf("%s %s", actionCreateListener, poolName))
	return updatesRequired
}

// checkListenerForExtPortDeletedFromService - check if there is a Kube service for the specified listener
func (c *CloudVpc) checkListenerForExtPortDeletedFromService(updatesRequired []string, listener *VpcLoadBalancerListener, ports []v1.ServicePort) []string {
	// Search for a matching port
	for _, kubePort := range ports {
		if c.isServicePortEqualListener(kubePort, listener) {
			// A service was found for the listener.  No updated needed.
			return updatesRequired
		}
	}
	// Port for this listener must have been deleted. Delete the listener
	updatesRequired = append(updatesRequired, fmt.Sprintf("%s %s %s", actionDeleteListener, listener.DefaultPool.Name, listener.ID))
	return updatesRequired
}

// checkPoolsForExtPortAddedToService - check to see if we have existing pool for the specified Kube service
func (c *CloudVpc) checkPoolsForExtPortAddedToService(updatesRequired []string, pools []*VpcLoadBalancerPool, servicePort v1.ServicePort) ([]string, error) {
	poolName := genLoadBalancerPoolName(servicePort)
	for _, pool := range pools {
		if pool.Name == poolName {
			// Found an existing pool for the pool name, no additional update needed
			return updatesRequired, nil
		}
		// If the pool was marked for deletion, move on to the next one
		if pool.Name == poolToBeDeleted {
			continue
		}
		poolNameFields, err := extractFieldsFromPoolName(pool.Name)
		if err != nil {
			return updatesRequired, err
		}
		// If we already have a pool for the external port, no need to create a new pool
		if c.isServicePortEqualPoolName(servicePort, poolNameFields) {
			return updatesRequired, nil
		}
	}
	updatesRequired = append(updatesRequired, fmt.Sprintf("%s %s", actionCreatePool, poolName))
	return updatesRequired, nil
}

// checkPoolForExtPortDeletedFromService - check to see if we have a Kube service for the specific pool
func (c *CloudVpc) checkPoolForExtPortDeletedFromService(updatesRequired []string, pool *VpcLoadBalancerPool, ports []v1.ServicePort) ([]string, error) {
	// Search through the service ports to find a matching external port
	poolNameFields, err := extractFieldsFromPoolName(pool.Name)
	if err != nil {
		return updatesRequired, err
	}
	for _, kubePort := range ports {
		if c.isServicePortEqualPoolName(kubePort, poolNameFields) {
			// Found a service for the pool, no additional update needed
			return updatesRequired, nil
		}
	}
	// Update the pool name indicating that it is being deleted.  This will prevent pool members from being created/deleted
	updatesRequired = append(updatesRequired, fmt.Sprintf("%s %s %s", actionDeletePool, pool.Name, pool.ID))
	pool.Name = poolToBeDeleted

	// External port for this pool must have been deleted. Delete the pool
	return updatesRequired, nil
}

// checkPoolForNodesToAdd - check to see if any of the existing members of a VPC pool need to be deleted
func (c *CloudVpc) checkPoolForNodesToAdd(updatesRequired []string, pool *VpcLoadBalancerPool, ports []v1.ServicePort, nodeList []string) ([]string, error) {
	// If the pool was marked for deletion, don't bother checking the members
	if pool.Name == poolToBeDeleted {
		return updatesRequired, nil
	}
	// Extract the fields from the pool name
	poolNameFields, err := extractFieldsFromPoolName(pool.Name)
	if err != nil {
		return updatesRequired, err
	}
	// Make sure that the node port of the pool is correct, i.e. generated poolName for Kube service must match actual pool name
	for _, kubePort := range ports {
		if c.isServicePortEqualPoolName(kubePort, poolNameFields) {
			// Found the correct kube service
			if poolNameFields.NodePort != int(kubePort.NodePort) {
				// Node port for the pool has changed.  All members (nodes) will be refreshed
				return updatesRequired, nil
			}
		}
	}
	// Verify that we have a pool member for each of the nodes AND the node port in the member is correct
	for _, nodeID := range nodeList {
		foundMember := false
		for _, member := range pool.Members {
			memberTarget := member.TargetIPAddress
			if nodeID == memberTarget && poolNameFields.NodePort == int(member.Port) {
				// There is a pool member for this node.  Move on to the next node
				foundMember = true
				break
			}
		}
		// If we failed to find member for the node, then we need to create one
		if !foundMember {
			updatesRequired = append(updatesRequired, fmt.Sprintf("%s %s %s %s", actionCreatePoolMember, pool.Name, pool.ID, nodeID))
		}
	}
	return updatesRequired, nil
}

// checkPoolForNodesToDelete - check to see if any of the existing members of a VPC pool need to be deleted
func (c *CloudVpc) checkPoolForNodesToDelete(updatesRequired []string, pool *VpcLoadBalancerPool, ports []v1.ServicePort, nodeList []string) ([]string, error) {
	// If the pool was marked for deletion, don't bother checking the members
	if pool.Name == poolToBeDeleted {
		return updatesRequired, nil
	}
	// Extract the fields from the pool name
	poolNameFields, err := extractFieldsFromPoolName(pool.Name)
	if err != nil {
		return updatesRequired, err
	}
	// Make sure that the node port of the pool is correct, i.e. generated poolName for Kube service must match actual pool name
	for _, kubePort := range ports {
		if c.isServicePortEqualPoolName(kubePort, poolNameFields) {
			// Found the correct kube service port for the specified pool
			if poolNameFields.NodePort != int(kubePort.NodePort) {
				// Node port for the pool has changed.
				// All members (nodes) will be refreshed by a REPLACE-POOL-MEMBERS update when checkPoolForServiceChanges() runs
				return updatesRequired, nil
			}
		}
	}
	// Verify that each pool member refers to a node AND the node port in the member is correct
	nodeString := " " + strings.Join(nodeList, " ") + " "
	for _, member := range pool.Members {
		memberTarget := member.TargetIPAddress
		if !strings.Contains(nodeString, " "+memberTarget+" ") || poolNameFields.NodePort != int(member.Port) {
			updatesRequired = append(updatesRequired, fmt.Sprintf("%s %s %s %s %s", actionDeletePoolMember, pool.Name, pool.ID, member.ID, memberTarget))
		}
	}
	return updatesRequired, nil
}

// checkPoolForServiceChanges - check to see if we have a Kube service for the specific pool
func (c *CloudVpc) checkPoolForServiceChanges(updatesRequired []string, pool *VpcLoadBalancerPool, service *v1.Service) ([]string, error) {
	// If the pool was marked for deletion, don't bother checking to see if needs to get updated
	if pool.Name == poolToBeDeleted {
		return updatesRequired, nil
	}
	// Extract the fields from the pool name
	poolNameFields, err := extractFieldsFromPoolName(pool.Name)
	if err != nil {
		return updatesRequired, err
	}
	// Search through the service ports to find a matching external port
	for _, kubePort := range service.Spec.Ports {
		if !c.isServicePortEqualPoolName(kubePort, poolNameFields) {
			// If this is not the correct Kube service port, move on to the next one
			continue
		}
		poolName := genLoadBalancerPoolName(kubePort)
		updatePool := false
		replacePoolMembers := false
		switch {
		case poolName != pool.Name:
			updatePool = true
			replacePoolMembers = true

		// case pool.SessionPersistence != desiredPersistence:
		// 	updatePool = true

		// case pool.Algorithm != desiredScheduler:
		// 	updatePool = true

		case service.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal && service.Spec.HealthCheckNodePort > 0 &&
			(pool.HealthMonitor.Type != LoadBalancerProtocolHTTP || pool.HealthMonitor.Port != int64(service.Spec.HealthCheckNodePort)):
			updatePool = true

		case service.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeCluster &&
			(pool.HealthMonitor.Type != LoadBalancerProtocolTCP || pool.HealthMonitor.Port != int64(kubePort.NodePort)):
			updatePool = true
		}

		if updatePool {
			updatesRequired = append(updatesRequired, fmt.Sprintf("%s %s %s", actionUpdatePool, poolName, pool.ID))
		}
		if replacePoolMembers {
			updatesRequired = append(updatesRequired, fmt.Sprintf("%s %s %s", actionReplacePoolMembers, poolName, pool.ID))
		}
		break
	}
	return updatesRequired, nil
}

// CreateLoadBalancer - create a VPC load balancer
func (c *CloudVpc) CreateLoadBalancer(lbName string, service *v1.Service, nodes []*v1.Node) (*VpcLoadBalancer, error) {
	if lbName == "" || service == nil || nodes == nil {
		return nil, fmt.Errorf("Required argument is missing")
	}

	// Validate the service tht was passed in and extract the advanced options requested
	options, err := c.validateService(service)
	if err != nil {
		return nil, err
	}

	// Determine what VPC subnets to associate with this load balancer
	allSubnets, err := c.Sdk.ListSubnets()
	if err != nil {
		return nil, err
	}
	vpcSubnets := c.filterSubnetsByVpcName(allSubnets, c.Config.VpcName)
	clusterSubnets := c.filterSubnetsByName(vpcSubnets, c.Config.SubnetNames)
	if len(clusterSubnets) == 0 {
		return nil, fmt.Errorf("None of the configured VPC subnets (%s) were found", c.Config.SubnetNames)
	}
	subnetList := c.getSubnetIDs(clusterSubnets)
	serviceSubnets := c.getServiceSubnets(service)
	serviceZone := c.getServiceZone(service)
	if serviceSubnets != "" {
		vpcID := clusterSubnets[0].Vpc.ID
		subnetList, err = c.validateServiceSubnets(service, serviceSubnets, vpcID, vpcSubnets)
	} else if serviceZone != "" {
		subnetList, err = c.validateServiceZone(service, serviceZone, clusterSubnets)
	}
	if err != nil {
		return nil, err
	}
	klog.Infof("Subnets: %+v", subnetList)

	// Filter node list by the service annotations (if specified) and node edge label (if set)
	filterLabel, filterValue := c.getServiceNodeSelectorFilter(service)
	if filterLabel != "" {
		nodes = c.findNodesMatchingLabelValue(nodes, filterLabel, filterValue)
	} else {
		nodes = c.filterNodesByServiceZone(nodes, service)
		nodes = c.filterNodesByEdgeLabel(nodes)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("There are no available nodes for this service")
	}

	// Determine the IP address for each of the nodes
	nodeList := c.getNodeIDs(nodes)
	klog.Infof("Nodes: %v", nodeList)

	// Determine what ports are associated with the service
	klog.Infof("Ports: %+v", service.Spec.Ports)
	poolList, err := c.getServicePoolNames(service)
	if err != nil {
		return nil, err
	}
	klog.Infof("Pools: %+v", poolList)

	// Determine if we are creating public or private load balancer
	publicLB := c.isServicePublic(service)

	// Determine if service has externalTrafficPolicy:Local
	//   0  = externalTrafficPolicy: Cluster
	//  >0  = externalTrafficPolicy: Local
	healthCheckNodePort := c.getServiceHealthCheckNodePort(service)

	// Create the load balancer
	lb, err := c.Sdk.CreateLoadBalancer(lbName, publicLB, nodeList, poolList, subnetList, healthCheckNodePort, options)
	if err != nil {
		return nil, err
	}
	return lb, nil
}

// createLoadBalancerListener - create a VPC load balancer listener
func (c *CloudVpc) createLoadBalancerListener(lb *VpcLoadBalancer, poolName, options string) error {
	poolID := ""
	for _, pool := range lb.Pools {
		if poolName == pool.Name {
			poolID = pool.ID
			break
		}
	}
	if poolID == "" {
		return fmt.Errorf("Unable to create listener. Pool %s not found", poolName)
	}
	_, err := c.Sdk.CreateLoadBalancerListener(lb.ID, poolName, poolID, options)
	return err
}

// createLoadBalancerPool - create a VPC load balancer pool
func (c *CloudVpc) createLoadBalancerPool(lb *VpcLoadBalancer, poolName string, nodeList []string, healthCheckPort int, options string) error {
	_, err := c.Sdk.CreateLoadBalancerPool(lb.ID, poolName, nodeList, healthCheckPort, options)
	return err
}

// createLoadBalancerPoolMember - create a VPC load balancer pool member
func (c *CloudVpc) createLoadBalancerPoolMember(lb *VpcLoadBalancer, args, options string) error {
	argsArray := strings.Fields(args)
	if lb == nil || len(argsArray) != 3 {
		return fmt.Errorf("Required argument is missing")
	}
	poolName := argsArray[0]
	poolID := argsArray[1]
	nodeID := argsArray[2]
	_, err := c.Sdk.CreateLoadBalancerPoolMember(lb.ID, poolName, poolID, nodeID, options)
	return err
}

// DeleteLoadBalancer - delete a VPC load balancer
func (c *CloudVpc) DeleteLoadBalancer(lb *VpcLoadBalancer, service *v1.Service) error {
	if lb == nil {
		return fmt.Errorf("Required argument is missing")
	}
	return c.Sdk.DeleteLoadBalancer(lb.ID)
}

// deleteLoadBalancerListener - delete a VPC load balancer listener
func (c *CloudVpc) deleteLoadBalancerListener(lb *VpcLoadBalancer, args string) error {
	argsArray := strings.Fields(args)
	if lb == nil || len(argsArray) != 2 {
		return fmt.Errorf("Required argument is missing")
	}
	// poolName := argsArray[0]
	listenerID := argsArray[1]
	return c.Sdk.DeleteLoadBalancerListener(lb.ID, listenerID)
}

// deleteLoadBalancerPool - delete a VPC load balancer pool
func (c *CloudVpc) deleteLoadBalancerPool(lb *VpcLoadBalancer, args string) error {
	argsArray := strings.Fields(args)
	if lb == nil || len(argsArray) != 2 {
		return fmt.Errorf("Required argument is missing")
	}
	// poolName := argsArray[0]
	poolID := argsArray[1]
	return c.Sdk.DeleteLoadBalancerPool(lb.ID, poolID)
}

// deleteLoadBalancerPoolMember - delete a VPC load balancer pool member
func (c *CloudVpc) deleteLoadBalancerPoolMember(lb *VpcLoadBalancer, args string) error {
	argsArray := strings.Fields(args)
	if lb == nil || len(argsArray) != 4 {
		return fmt.Errorf("Required argument is missing")
	}
	// poolName := argsArray[0]
	poolID := argsArray[1]
	memberID := argsArray[2]
	// nodeID := argsArray[3]
	return c.Sdk.DeleteLoadBalancerPoolMember(lb.ID, poolID, memberID)
}

// FindLoadBalancer - locate a VPC load balancer based on the Name, ID, or hostname
func (c *CloudVpc) FindLoadBalancer(nameID string, service *v1.Service) (*VpcLoadBalancer, error) {
	if nameID == "" {
		return nil, fmt.Errorf("Required argument is missing")
	}
	lbs, err := c.Sdk.ListLoadBalancers()
	if err != nil {
		return nil, err
	}
	for _, lb := range lbs {
		if nameID == lb.ID || nameID == lb.Name || nameID == lb.Hostname {
			return lb, nil
		}
	}
	return nil, nil
}

// GetLoadBalancerStatus returns the load balancer status for a given VPC host name
func (c *CloudVpc) GetLoadBalancerStatus(service *v1.Service, lb *VpcLoadBalancer) *v1.LoadBalancerStatus {
	lbStatus := &v1.LoadBalancerStatus{}
	lbStatus.Ingress = []v1.LoadBalancerIngress{{Hostname: lb.Hostname}}
	return lbStatus
}

// replaceLoadBalancerPoolMembers - replace the load balancer pool members
func (c *CloudVpc) replaceLoadBalancerPoolMembers(lb *VpcLoadBalancer, args string, nodeList []string, options string) error {
	argsArray := strings.Fields(args)
	if lb == nil || len(argsArray) != 2 {
		return fmt.Errorf("Required argument is missing")
	}
	poolName := argsArray[0]
	poolID := argsArray[1]
	_, err := c.Sdk.ReplaceLoadBalancerPoolMembers(lb.ID, poolName, poolID, nodeList, options)
	return err
}

// UpdateLoadBalancer - update a VPC load balancer
func (c *CloudVpc) UpdateLoadBalancer(lb *VpcLoadBalancer, service *v1.Service, nodes []*v1.Node) (*VpcLoadBalancer, error) {
	if lb == nil || service == nil || nodes == nil {
		return nil, fmt.Errorf("Required argument is missing")
	}

	// Verify that the load balancer is in the correct state
	if !lb.IsReady() {
		return nil, fmt.Errorf("Update can not be performed, load balancer is not ready: %v", lb.GetStatus())
	}

	// Validate the service tht was passed in and extract the advanced options requested
	options, err := c.validateService(service)
	if err != nil {
		return nil, err
	}

	// If the service has been changed from public to private (or vice-versa)
	// If the service has been changed to a network load balancer (or vice versa)
	err = c.validateServiceTypeNotUpdated(service, lb)
	if err != nil {
		return nil, err
	}

	// Retrieve list of all VPC subnets
	vpcSubnets, err := c.Sdk.ListSubnets()
	if err != nil {
		return nil, err
	}

	// If the VPC subnets annotation on the service has been changed, detect this case and return error
	err = c.validateServiceSubnetsNotUpdated(service, lb, vpcSubnets)
	if err != nil {
		return nil, err
	}

	// Determine if service has externalTrafficPolicy:Local
	//   0  = externalTrafficPolicy: Cluster
	//  >0  = externalTrafficPolicy: Local
	healthCheckNodePort := c.getServiceHealthCheckNodePort(service)

	// Verify that there are nodes available to associate with this load balancer
	filterLabel, filterValue := c.getServiceNodeSelectorFilter(service)
	if filterLabel != "" {
		nodes = c.findNodesMatchingLabelValue(nodes, filterLabel, filterValue)
	} else {
		nodes = c.filterNodesByServiceZone(nodes, service)
		nodes = c.filterNodesByEdgeLabel(nodes)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("There are no available nodes for this load balancer")
	}

	// Retrieve list of listeners for the current load balancer
	listeners, err := c.Sdk.ListLoadBalancerListeners(lb.ID)
	if err != nil {
		return nil, err
	}

	// Retrieve list of pools for the current load balancer
	pools, err := c.Sdk.ListLoadBalancerPools(lb.ID)
	if err != nil {
		return nil, err
	}

	// Determine the node list
	nodeList := c.getNodeIDs(nodes)

	// The following array is going to be used to keep track of ALL of the updates that need to be done
	// There will be 1 line of text for each update that needs to be done.
	// The first word will indicate what type of operation needs to be done
	// The rest of the line will contain all of the necessary options needed to perform that update (space separated)
	// The arguments for each operation will be different
	// Care must be taken to ensure that the arg string is consistently ordered/handle by both the caller and the called function
	// Update operations must be performed in a specific order.  Rules concering the supported operations:
	//   1. DELETE-LISTENER must be done before the pool can be cleaned up with DELETE-POOL
	//   2. CREATE-POOL must be done before the pool can be referenced by an CREATE-LISTENER
	//   3. CREATE-LISTENER can not be done for an external port that is being used by an existing listener
	//   4. Since any CREATE operations cause cause us to hit the account quota, all CREATE operations will be done last
	//   5. No need to CREATE-POOL-MEMBER or DELETE-POOL-MEMBER if the entire pool was tagged to be deleted by a DELETE-POOL
	//   6. UPDATE-POOL handles updating the health check settings on the pool and/or changing the name of pool (node port change)
	//   7. REPLACE-POOL-MEMBERS handles updating the node port of all the pool members
	//   8. The listener is never updated. The listener will always points to the same pool once it has been created
	//   9. The load balancer object is never updated or modified.  All update processing is done on the listeners, pools, and members
	updatesRequired := []string{}

	// Step 1: Delete the VPC LB listener if the Kube service external port was deleted
	for _, listener := range listeners {
		updatesRequired = c.checkListenerForExtPortDeletedFromService(updatesRequired, listener, service.Spec.Ports)
	}

	// Step 2: Delete the VPC LB pool if the Kube service external port was deleted
	for _, pool := range pools {
		updatesRequired, err = c.checkPoolForExtPortDeletedFromService(updatesRequired, pool, service.Spec.Ports)
		if err != nil {
			return nil, err
		}
	}

	// Step 3: Delete VPC LB pool members for any nodes that are no longer in the cluster
	for _, pool := range pools {
		updatesRequired, err = c.checkPoolForNodesToDelete(updatesRequired, pool, service.Spec.Ports, nodeList)
		if err != nil {
			return nil, err
		}
	}

	// Step 4: Update the existing pools and pool members if the Kube service node port was changed -OR- if the externalTrafficPolicy was changed
	for _, pool := range pools {
		updatesRequired, err = c.checkPoolForServiceChanges(updatesRequired, pool, service)
		if err != nil {
			return nil, err
		}
	}

	// Step 5: Create new VPC LB pool members if new nodes were added to the cluster
	for _, pool := range pools {
		updatesRequired, err = c.checkPoolForNodesToAdd(updatesRequired, pool, service.Spec.Ports, nodeList)
		if err != nil {
			return nil, err
		}
	}

	// Step 6: Create a new VPC LB pool if a new external port was added to the Kube service
	for _, servicePort := range service.Spec.Ports {
		updatesRequired, err = c.checkPoolsForExtPortAddedToService(updatesRequired, pools, servicePort)
		if err != nil {
			return nil, err
		}

		// Step 7: Create a new VPC LB listener if a new external port was added to the Kube service
		updatesRequired = c.checkListenersForExtPortAddedToService(updatesRequired, listeners, servicePort)
	}

	// Step 8: Replace multiple CREATE-POOL-MEMBER / DELETE-POOL-MEMBER actions with a single REPLACE-POOL-MEMBERS
	updatesRequired = c.checkForMultiplePoolMemberUpdates(updatesRequired)

	// If no updates are required, then return
	if len(updatesRequired) == 0 {
		klog.Infof("No updates needed")
		return lb, nil
	}

	// Display list of all required updates
	for i, update := range updatesRequired {
		klog.Infof("Updates required [%d]: %s", i+1, update)
	}

	// Set sleep and max wait times.  Increase times if NLB
	maxWaitTime := 2 * 60
	minSleepTime := 8

	// Process all of the updates that are needed
	for i, update := range updatesRequired {
		// Get the updated load balancer object (if not first time through this loop)
		if i > 0 {
			lb, err = c.Sdk.GetLoadBalancer(lb.ID)
			if err != nil {
				return nil, err
			}
			// Wait for the LB to be "ready" before performing the actual update
			if !lb.IsReady() {
				lb, err = c.WaitLoadBalancerReady(lb, minSleepTime, maxWaitTime)
				if err != nil {
					return nil, err
				}
			}
		}

		// Process the current update
		klog.Infof("Processing update [%d]: %s", i+1, update)
		action := strings.Fields(update)[0]
		args := strings.TrimSpace(strings.TrimPrefix(update, action))
		switch action {
		case actionCreateListener:
			err = c.createLoadBalancerListener(lb, args, options)
		case actionCreatePool:
			err = c.createLoadBalancerPool(lb, args, nodeList, healthCheckNodePort, options)
		case actionCreatePoolMember:
			err = c.createLoadBalancerPoolMember(lb, args, options)
		case actionDeleteListener:
			err = c.deleteLoadBalancerListener(lb, args)
		case actionDeletePool:
			err = c.deleteLoadBalancerPool(lb, args)
		case actionDeletePoolMember:
			err = c.deleteLoadBalancerPoolMember(lb, args)
		case actionUpdatePool:
			err = c.updateLoadBalancerPool(lb, args, pools, healthCheckNodePort, options)
		case actionReplacePoolMembers:
			err = c.replaceLoadBalancerPoolMembers(lb, args, nodeList, options)
		default:
			err = fmt.Errorf("Unsupported update operation: %s", update)
		}
		// If update operation failed, return err
		if err != nil {
			return nil, err
		}
	}

	// Return the updated load balancer
	klog.Infof("Done with updates")
	return lb, nil
}

// updateLoadBalancerPool - create a VPC load balancer pool
func (c *CloudVpc) updateLoadBalancerPool(lb *VpcLoadBalancer, args string, pools []*VpcLoadBalancerPool, healthCheckPort int, options string) error {
	argsArray := strings.Fields(args)
	if lb == nil || len(argsArray) != 2 {
		return fmt.Errorf("Required argument is missing")
	}
	poolName := argsArray[0]
	poolID := argsArray[1]
	var existingPool *VpcLoadBalancerPool
	for _, pool := range pools {
		if pool.ID == poolID {
			existingPool = pool
			break
		}
	}
	if existingPool == nil {
		return fmt.Errorf("Existing pool nof found for pool ID: %s", poolID)
	}
	_, err := c.Sdk.UpdateLoadBalancerPool(lb.ID, poolName, existingPool, healthCheckPort, options)
	return err
}

// WaitLoadBalancerReady will call the Get() operation on the load balancer every minSleep seconds until the state
// of the load balancer goes to Online/Active -OR- until the maxWait timeout occurs
func (c *CloudVpc) WaitLoadBalancerReady(lb *VpcLoadBalancer, minSleep, maxWait int) (*VpcLoadBalancer, error) {
	// Wait for the load balancer to Online/Active
	var err error
	lbID := lb.ID
	startTime := time.Now()
	for i := 0; i < (maxWait / minSleep); i++ {
		suffix := ""
		if lb.ProvisioningStatus == LoadBalancerProvisioningStatusCreatePending {
			suffix = fmt.Sprintf(" Public:%s Private:%s", strings.Join(lb.PublicIps, ","), strings.Join(lb.PrivateIps, ","))
		}
		klog.Infof(" %3d) %9s %s%s", i+1, time.Since(startTime).Round(time.Millisecond), lb.GetStatus(), suffix)
		if lb.IsReady() {
			return lb, nil
		}
		if time.Since(startTime).Seconds() > float64(maxWait) {
			break
		}
		time.Sleep(time.Second * time.Duration(minSleep))
		lb, err = c.Sdk.GetLoadBalancer(lbID)
		if err != nil {
			klog.Errorf("Failed to get load balancer %v: %v", lbID, err)
			return nil, err
		}
	}
	return lb, fmt.Errorf("load balancer not ready: %s", lb.GetStatus())
}
