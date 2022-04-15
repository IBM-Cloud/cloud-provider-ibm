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
	"errors"
	"fmt"
	"strings"

	"cloud.ibm.com/cloud-provider-ibm/pkg/klog"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
)

const (
	creatingCloudLoadBalancerFailed  = "CreatingCloudLoadBalancerFailed"
	deletingCloudLoadBalancerFailed  = "DeletingCloudLoadBalancerFailed"
	gettingCloudLoadBalancerFailed   = "GettingCloudLoadBalancerFailed"
	updatingCloudLoadBalancerFailed  = "UpdatingCloudLoadBalancerFailed"
	verifyingCloudLoadBalancerFailed = "VerifyingCloudLoadBalancerFailed"

	vpcLbStatusOnlineActive              = LoadBalancerOperatingStatusOnline + "/" + LoadBalancerProvisioningStatusActive
	vpcLbStatusOfflineCreatePending      = LoadBalancerOperatingStatusOffline + "/" + LoadBalancerProvisioningStatusCreatePending
	vpcLbStatusOfflineMaintenancePending = LoadBalancerOperatingStatusOffline + "/" + LoadBalancerProvisioningStatusMaintenancePending
	vpcLbStatusOfflineFailed             = LoadBalancerOperatingStatusOffline + "/" + LoadBalancerProvisioningStatusFailed
	vpcLbStatusOfflineNotFound           = LoadBalancerOperatingStatusOffline + "/not_found"
)

// CloudVpc is the main VPC cloud provider implementation.
type CloudVpc struct {
	KubeClient kubernetes.Interface
	Config     *ConfigVpc
	Sdk        CloudVpcSdk
	Recorder   record.EventRecorder
}

// Global variables
var (
	// Persistent storage for CloudVpc object
	persistentCloudVpc *CloudVpc

	// VpcLbNamePrefix - Prefix to be used for VPC load balancer
	VpcLbNamePrefix = "kube"
)

// GetCloudVpc - Retrieve the global VPC cloud object.  Return nil if not initialized.
func GetCloudVpc() *CloudVpc {
	return persistentCloudVpc
}

// ResetCloudVpc - Resetthe global VPC cloud object
func ResetCloudVpc() {
	persistentCloudVpc = nil
}

// SetCloudVpc - Set the global VPC cloud object.  Specify nil to clear value
func SetCloudVpc(vpc *CloudVpc) {
	persistentCloudVpc = vpc
}

func NewCloudVpc(kubeClient kubernetes.Interface, config *ConfigVpc, recorder record.EventRecorder) (*CloudVpc, error) {
	if config == nil {
		return nil, fmt.Errorf("Missing cloud configuration")
	}
	c := &CloudVpc{KubeClient: kubeClient, Config: config, Recorder: recorder}
	err := c.initialize()
	if err != nil {
		return nil, err
	}
	c.Sdk, err = NewCloudVpcSdk(c.Config)
	if err != nil {
		return nil, err
	}
	SetCloudVpc(c)
	return c, nil
}

// EnsureLoadBalancer - called by cloud provider to create/update the load balancer
func (c *CloudVpc) EnsureLoadBalancer(lbName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	// Check to see if the VPC load balancer exists
	lb, err := c.FindLoadBalancer(lbName, service)
	if err != nil {
		errString := fmt.Sprintf("Failed getting LoadBalancer: %v", err)
		klog.Errorf(errString)
		return nil, c.recordServiceWarningEvent(service, creatingCloudLoadBalancerFailed, lbName, errString)
	}

	// If the specified VPC load balancer was not found, create it
	if lb == nil {
		lb, err = c.CreateLoadBalancer(lbName, service, nodes)
		if err != nil {
			errString := fmt.Sprintf("Failed ensuring LoadBalancer: %v", err)
			klog.Errorf(errString)
			return nil, c.recordServiceWarningEvent(service, creatingCloudLoadBalancerFailed, lbName, errString)
		}
		// Log basic stats about the load balancer and return success (if the LB is READY or not NLB)
		// - return SUCCESS for non-NLB to remain backward compatibility, no additional operations need to be done
		// - don't return SUCCESS for NLB, because we can't do the DNS lookup of static IP if the LB is still pending
		if lb.IsReady() || !lb.IsNLB() {
			klog.Infof(lb.GetSummary())
			klog.Infof("Load balancer %v created.", lbName)
			return c.GetLoadBalancerStatus(service, lb), nil
		}
	}

	// Log basic stats about the load balancer
	klog.Infof(lb.GetSummary())

	// If we get to this point, it means that EnsureLoadBalancer was called against a Load Balancer
	// that already exists. This is most likely due to a change is the Kubernetes service.
	// If the load balancer is not "Online/Active", then no additional operations that can be performed.
	if !lb.IsReady() {
		errString := fmt.Sprintf("LoadBalancer is busy: %v", lb.GetStatus())
		klog.Warningf(errString)
		return nil, c.recordServiceWarningEvent(service, creatingCloudLoadBalancerFailed, lbName, errString)
	}

	// The load balancer state is Online/Active.  This means that additional operations can be done.
	// Update the existing LB with any service or node changes that may have occurred.
	lb, err = c.UpdateLoadBalancer(lb, service, nodes)
	if err != nil {
		errString := fmt.Sprintf("Failed ensuring LoadBalancer: %v", err)
		klog.Errorf(errString)
		return nil, c.recordServiceWarningEvent(service, creatingCloudLoadBalancerFailed, lbName, errString)
	}

	// Return success
	klog.Infof("Load balancer %v created.", lbName)
	return c.GetLoadBalancerStatus(service, lb), nil
}

// EnsureLoadBalancerDeleted - called by cloud provider to delete the load balancer
func (c *CloudVpc) EnsureLoadBalancerDeleted(lbName string, service *v1.Service) error {
	// Check to see if the VPC load balancer exists
	lb, err := c.FindLoadBalancer(lbName, service)
	if err != nil {
		errString := fmt.Sprintf("Failed getting LoadBalancer: %v", err)
		klog.Errorf(errString)
		return c.recordServiceWarningEvent(service, deletingCloudLoadBalancerFailed, lbName, errString)
	}

	// If the load balancer does not exist, return
	if lb == nil {
		klog.Infof("Load balancer %v not found", lbName)
		return nil
	}

	// Log basic stats about the load balancer
	klog.Infof(lb.GetSummary())

	// The load balancer state is Online/Active.  Attempt to delete the load balancer
	err = c.DeleteLoadBalancer(lb, service)
	if err != nil {
		errString := fmt.Sprintf("Failed deleting LoadBalancer: %v", err)
		klog.Errorf(errString)
		return c.recordServiceWarningEvent(service, deletingCloudLoadBalancerFailed, lbName, errString)
	}

	// Return success
	klog.Infof("Load balancer %v deleted", lbName)
	return nil
}

// EnsureLoadBalancerUpdated - updates the hosts under the specified load balancer
func (c *CloudVpc) EnsureLoadBalancerUpdated(lbName string, service *v1.Service, nodes []*v1.Node) error {
	// Check to see if the VPC load balancer exists
	lb, err := c.FindLoadBalancer(lbName, service)
	if err != nil {
		errString := fmt.Sprintf("Failed getting LoadBalancer: %v", err)
		klog.Errorf(errString)
		return c.recordServiceWarningEvent(service, updatingCloudLoadBalancerFailed, lbName, errString)
	}
	if lb == nil {
		errString := fmt.Sprintf("Load balancer not found: %v", lbName)
		klog.Warningf(errString)
		return nil
	}
	// Log basic stats about the load balancer
	klog.Infof(lb.GetSummary())

	// Check the state of the load balancer to determine if the update operation can even be attempted
	if !lb.IsReady() {
		errString := fmt.Sprintf("LoadBalancer is busy: %v", lb.GetStatus())
		klog.Warningf(errString)
		return c.recordServiceWarningEvent(service, updatingCloudLoadBalancerFailed, lbName, errString)
	}

	// The load balancer state is Online/Active.  This means that additional operations can be done.
	// Update the existing LB with any service or node changes that may have occurred.
	_, err = c.UpdateLoadBalancer(lb, service, nodes)
	if err != nil {
		errString := fmt.Sprintf("Failed updating LoadBalancer: %v", err)
		klog.Errorf(errString)
		return c.recordServiceWarningEvent(service, updatingCloudLoadBalancerFailed, lbName, errString)
	}

	// Return success
	klog.Infof("Load balancer %v updated.", lbName)
	return nil
}

// GatherLoadBalancers - returns status of all VPC load balancers associated with Kube LBs in this cluster
func (c *CloudVpc) GatherLoadBalancers(services *v1.ServiceList) (map[string]*v1.Service, map[string]*VpcLoadBalancer, error) {
	// Verify we were passed a list of Kube services
	if services == nil {
		klog.Errorf("Required argument is missing")
		return nil, nil, errors.New("Required argument is missing")
	}
	// Retrieve list of all load balancers
	lbs, err := c.Sdk.ListLoadBalancers()
	if err != nil {
		return nil, nil, err
	}
	// Create map of VPC LBs. Do not include LBs that are in different cluster
	vpcMap := map[string]*VpcLoadBalancer{}
	lbPrefix := VpcLbNamePrefix + "-" + c.Config.ClusterID + "-"
	for _, lb := range lbs {
		if strings.HasPrefix(lb.Name, lbPrefix) {
			lbPtr := lb
			vpcMap[lb.Name] = lbPtr
		}
	}
	// Create map of Kube node port and LB services
	lbMap := map[string]*v1.Service{}
	npMap := map[string]*v1.Service{}
	for _, service := range services.Items {
		// Keep track of all load balancer -AND- node port services.
		//
		// The cloud provider will only ever create VPC LB for a Load Balancer service,
		// but the vpcctl binary can create a VPC LB for a Node Port service.  This allows testing
		// of create/delete VPC LB functionality outside of the cloud provider.
		//
		// This means that it is possible to have VPC LB point to either a Kube LB or Kube node port
		kubeService := service
		switch kubeService.Spec.Type {
		case v1.ServiceTypeLoadBalancer:
			lbName := c.GenerateLoadBalancerName(&kubeService)
			lbMap[lbName] = &kubeService
		case v1.ServiceTypeNodePort:
			lbName := c.GenerateLoadBalancerName(&kubeService)
			npMap[lbName] = &kubeService
		}
	}

	// Clean up any VPC LBs that are in READY state that do not have Kube LB or node port service
	for _, lb := range vpcMap {
		if !lb.IsReady() {
			continue
		}
		// If we have a VPC LB and there is no Kube LB or node port service associated with it,
		// go ahead and schedule the deletion of the VPC LB. The fact that we are deleting the
		// VPC LB will be displayed as an "INFO:"" statement in the vpcctl stdout and will be added
		// to the cloud provider controller manager log. Since there is no "ServiceUID:" on this
		// "INFO:" statement, it will just be logged.
		if lbMap[lb.Name] == nil && npMap[lb.Name] == nil {
			klog.Infof("Deleting stale VPC LB: %s", lb.GetSummary())
			err := c.DeleteLoadBalancer(lb, nil)
			if err != nil {
				// Add an error message to log, but don't fail the entire MONITOR operation
				klog.Errorf("Failed to delete stale VPC LB: %s", lb.Name)
			}
		}
	}

	// Return the LB and VPC maps to the caller
	return lbMap, vpcMap, nil
}

// GenerateLoadBalancerName - generate the VPC load balancer name from the cluster ID and Kube service
func (c *CloudVpc) GenerateLoadBalancerName(service *v1.Service) string {
	serviceID := strings.ReplaceAll(string(service.ObjectMeta.UID), "-", "")
	lbName := VpcLbNamePrefix + "-" + c.Config.ClusterID + "-" + serviceID
	// Limit the LB name to 63 characters
	if len(lbName) > 63 {
		lbName = lbName[:63]
	}
	return lbName
}

// getEventMessage based on the status that was passed in
func (c *CloudVpc) getEventMessage(status string) string {
	switch status {
	case vpcLbStatusOfflineFailed:
		return "The VPC load balancer that routes requests to this Kubernetes LoadBalancer service is offline. For troubleshooting steps, see <https://ibm.biz/vpc-lb-ts>"
	case vpcLbStatusOfflineMaintenancePending:
		return "The VPC load balancer that routes requests to this Kubernetes LoadBalancer service is under maintenance."
	case vpcLbStatusOfflineNotFound:
		return "The VPC load balancer that routes requests to this Kubernetes LoadBalancer service was not found. To recreate the VPC load balancer, restart the Kubernetes master by running 'ibmcloud ks cluster master refresh --cluster <cluster_name_or_id>'."
	default:
		return fmt.Sprintf("The VPC load balancer that routes requests to this Kubernetes LoadBalancer service is currently %s.", status)
	}
}

// GetLoadBalancer - called by cloud provider to retrieve status of the load balancer
func (c *CloudVpc) GetLoadBalancer(lbName string, service *v1.Service) (*v1.LoadBalancerStatus, bool, error) {
	// Check to see if the VPC load balancer exists
	lb, err := c.FindLoadBalancer(lbName, service)
	if err != nil {
		errString := fmt.Sprintf("Failed getting LoadBalancer: %v", err)
		klog.Errorf(errString)
		return nil, false, c.recordServiceWarningEvent(service, gettingCloudLoadBalancerFailed, lbName, errString)
	}

	// The load balancer was not found
	if lb == nil {
		klog.Infof("Load balancer %v not found", lbName)
		return nil, false, nil
	}

	// Write details of the load balancer to the log
	klog.Infof(lb.GetSummary())

	// If the VPC load balancer is not Ready, return the hostname from the service or blank
	if !lb.IsReady() {
		klog.Warningf("Load balancer %s is busy: %v", lbName, lb.GetStatus())
		var lbStatus *v1.LoadBalancerStatus
		if service.Status.LoadBalancer.Ingress != nil {
			lbStatus = c.GetLoadBalancerStatus(service, lb)
		} else {
			lbStatus = &v1.LoadBalancerStatus{}
		}
		return lbStatus, true, nil
	}

	// Return success
	klog.Infof("Load balancer %v found.", lbName)
	return c.GetLoadBalancerStatus(service, lb), true, nil
}

// MonitorLoadBalancers - accepts a list of services (of all types), verifies that each Kubernetes load balancer service
// has a corresponding VPC load balancer object, and creates Kubernetes events based on the load balancer's status.
// `status` is a map from a load balancer's unique Service ID to its status.
// This persists load balancer status between consecutive monitor calls.
func (c *CloudVpc) MonitorLoadBalancers(services *v1.ServiceList, status map[string]string) {
	// Verify we were passed a list of Kube services
	if services == nil {
		klog.Infof("No Load Balancers to monitor, returning")
		return
	}
	// Retrieve list of VPC LBs for the current cluster
	lbMap, vpcMap, err := c.GatherLoadBalancers(services)
	if err != nil {
		klog.Errorf("Failed retrieving VPC LBs: %v", err)
		return
	}

	// Verify that we have a VPC LB for each of the Kube LB services
	for lbName, service := range lbMap {
		serviceID := string(service.ObjectMeta.UID)
		oldStatus := status[serviceID]
		vpcLB, exists := vpcMap[lbName]
		if exists {
			if vpcLB.IsReady() {
				klog.Infof("VPC LB: %s Service:%s/%s", vpcLB.GetSummary(), service.ObjectMeta.Namespace, service.ObjectMeta.Name)
			} else {
				klog.Warningf("VPC LB: %s Service:%s/%s", vpcLB.GetSummary(), service.ObjectMeta.Namespace, service.ObjectMeta.Name)
			}
			// Store the new status so its available to the next call to VpcMonitorLoadBalancers()
			newStatus := vpcLB.GetStatus()
			status[serviceID] = newStatus

			// If the current state of the LB is online/active
			if newStatus == vpcLbStatusOnlineActive {
				if oldStatus != vpcLbStatusOnlineActive {
					// If the status of the VPC load balancer transitioned to 'online/active' --> NORMAL EVENT.
					c.recordServiceNormalEvent(service, lbName, c.getEventMessage(newStatus))
				}
				// Move on to the next LB service
				continue
			}

			// If the status of the VPC load balancer is not 'online/active', record warning event if status has not changed since last we checked
			if oldStatus == newStatus {
				_ = c.recordServiceWarningEvent(
					service, verifyingCloudLoadBalancerFailed, lbName, c.getEventMessage(newStatus)) // #nosec G104 error is always returned
			}

			// Move on to the next LB service
			continue
		}

		// There is no VPC LB for the current Kubernetes load balancer.  Update the status to: "offline/not_found"
		klog.Warningf("VPC LB not found for service %s/%s %s", service.ObjectMeta.Namespace, service.ObjectMeta.Name, serviceID)
		newStatus := vpcLbStatusOfflineNotFound
		status[serviceID] = newStatus
		if oldStatus == newStatus {
			_ = c.recordServiceWarningEvent(
				service, verifyingCloudLoadBalancerFailed, lbName, c.getEventMessage(newStatus)) // #nosec G104 error is always returned
		}
	}
}

// recordServiceNormalEvent logs a VPC load balancer service event
func (c *CloudVpc) recordServiceNormalEvent(lbService *v1.Service, lbName, eventMessage string) {
	if c.Recorder != nil {
		message := fmt.Sprintf("Event on cloud load balancer %v for service %v with UID %v: %v",
			lbName, types.NamespacedName{Namespace: lbService.ObjectMeta.Namespace, Name: lbService.ObjectMeta.Name}, lbService.ObjectMeta.UID, eventMessage)
		c.Recorder.Event(lbService, v1.EventTypeNormal, "CloudVPCLoadBalancerNormalEvent", message)
	}
}

// recordServiceWarningEvent logs a VPC load balancer service warning
// event and returns an error representing the event.
func (c *CloudVpc) recordServiceWarningEvent(lbService *v1.Service, reason, lbName, errorMessage string) error {
	message := fmt.Sprintf("Error on cloud load balancer %v for service %v with UID %v: %v",
		lbName, types.NamespacedName{Namespace: lbService.ObjectMeta.Namespace, Name: lbService.ObjectMeta.Name}, lbService.ObjectMeta.UID, errorMessage)
	if c.Recorder != nil {
		c.Recorder.Event(lbService, v1.EventTypeWarning, reason, message)
	}
	return errors.New(message)
}
