/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2019, 2021 All Rights Reserved.
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
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// VPC Load Balancer Monitor Constants
const vpcLBStatusPrefix = "Status"
const vpcLBServiceIDPrefix = "ServiceUID"
const vpcStatusOnlineActive = "online/active"
const vpcStatusOfflineCreatePending = "offline/create_pending"
const vpcStatusOfflineMaintenancePending = "offline/maintenance_pending"
const vpcStatusOfflineFailed = "offline/failed"
const vpcStatusOfflineNotFound = "offline/not_found"
const networkLoadBalancerFeature = "nlb"

// execVpcCommand - Run a VPC command and return the output to the caller
// switched from func to var so method can be spoofed
var execVpcCommand = func(args string, envvars []string) ([]string, error) {
	cmd := exec.Command("vpcctl", strings.Fields(args)...) // #nosec G204 external access to this function is not possible
	env := os.Environ()
	env = append(env, envvars...)
	cmd.Env = env
	// Run vpcctl and get the output
	outBytes, err := cmd.CombinedOutput()

	if err != nil {
		return nil, err
	}
	return strings.Split(string(outBytes), "\n"), nil
}

// getVpcLoadBalancerName returns the name of the load balancer. Implementations must treat the
// *v1.Service parameter as read-only and not modify it.
func (c *Cloud) getVpcLoadBalancerName(service *v1.Service) string {
	clusterID := c.Config.Prov.ClusterID
	serviceID := strings.ReplaceAll(string(service.UID), "-", "")
	ret := "kube-" + clusterID + "-" + serviceID
	// Limit the LB name to 63 characters
	if len(ret) > 63 {
		ret = ret[:63]
	}
	return ret
}

// getVpcLoadBalancerStatus returns the load balancer status for a given VPC host name
func getVpcLoadBalancerStatus(service *v1.Service, hostname string) *v1.LoadBalancerStatus {
	lbStatus := &v1.LoadBalancerStatus{}
	if strings.Contains(hostname, ",") {
		ipArray := strings.Split(hostname, ",")
		for _, ipArrayItem := range ipArray {
			ipArrayItem = strings.TrimSpace(ipArrayItem)
			ingressObject := v1.LoadBalancerIngress{IP: ipArrayItem}
			lbStatus.Ingress = append(lbStatus.Ingress, ingressObject)
		}
		return lbStatus
	}
	lbStatus.Ingress = []v1.LoadBalancerIngress{{Hostname: hostname}}
	if isFeatureEnabled(service, networkLoadBalancerFeature) {
		// IF the hostname and static IP address are already stored in the service, then don't
		// repeat the overhead of the DNS hostname resolution again
		if service.Status.LoadBalancer.Ingress != nil &&
			len(service.Status.LoadBalancer.Ingress) == 1 &&
			service.Status.LoadBalancer.Ingress[0].Hostname == hostname &&
			service.Status.LoadBalancer.Ingress[0].IP != "" {
			lbStatus.Ingress[0].IP = service.Status.LoadBalancer.Ingress[0].IP
		} else {
			ipAddrs, err := net.LookupIP(hostname)
			if err == nil && len(ipAddrs) > 0 && len(ipAddrs[0]) == net.IPv4len {
				lbStatus.Ingress[0].IP = ipAddrs[0].String()
			}
		}
	}
	return lbStatus
}

// getVpcLoadBalancer returns whether the specified load balancer exists, and
// if so, what its status is.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) getVpcLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (*v1.LoadBalancerStatus, bool, error) {
	lbName := c.getVpcLoadBalancerName(service)
	klog.Infof("GetLoadBalancer(%v, %v)", lbName, clusterName)

	command := c.determineCommandArgs("STATUS-LB", lbName, service)
	outArray, err := execVpcCommand(command, c.determineVpcEnvSettings(nil))
	if err != nil {
		return nil, false, c.Recorder.VpcLoadBalancerServiceWarningEvent(
			service, GettingCloudLoadBalancerFailed, lbName,
			fmt.Sprintf("Failed executing command [%s]: %v", command, err),
		)
	}
	for _, line := range outArray {
		if len(line) < 2 || !strings.Contains(line, ": ") {
			continue
		}
		lineType := strings.Split(line, ":")[0]             // Grab first part of the output line
		lineData := strings.TrimPrefix(line, lineType+": ") // Remainder of the output line
		switch lineType {
		case "ERROR":
			klog.Error(lineData)
			return nil, false, c.Recorder.VpcLoadBalancerServiceWarningEvent(
				service, GettingCloudLoadBalancerFailed, lbName,
				fmt.Sprintf("Failed getting LoadBalancer: %v", lineData))
		case "INFO":
			klog.Info(lineData)
		case "NOT_FOUND":
			klog.Infof("Load balancer %v not found", lbName)
			return nil, false, nil
		case "PENDING":
			klog.Warningf("Load balancer %s is busy: %v", lbName, lineData)
			var lbStatus *v1.LoadBalancerStatus
			if service.Status.LoadBalancer.Ingress != nil {
				lbStatus = getVpcLoadBalancerStatus(service, service.Status.LoadBalancer.Ingress[0].Hostname)
			} else {
				lbStatus = &v1.LoadBalancerStatus{}
			}
			return lbStatus, true, nil
		case "SUCCESS":
			klog.Infof("Load balancer %v found.  Hostname: %v", lbName, lineData)
			return getVpcLoadBalancerStatus(service, lineData), true, nil
		default:
			klog.Warning(line)
		}
	}
	return nil, false, c.Recorder.VpcLoadBalancerServiceWarningEvent(
		service, GettingCloudLoadBalancerFailed, lbName,
		"Invalid response from command")
}

func (c *Cloud) determineCommandArgs(command, lbName string, service *v1.Service) string {
	return command + " " + lbName + " " + service.Namespace + "/" + service.Name
}

func (c *Cloud) determineVpcEnvSettings(nodes []*v1.Node) []string {
	// Set the default environment to be just the KUBECONFIG
	env := []string{"KUBECONFIG=" + c.Config.Kubernetes.ConfigFilePaths[0]}

	// If this is a Gen2 cluster then add the worker service account ID to the environment settings
	if c.Config.Prov.ProviderType == lbVpcNextGenProvider {
		env = append(env, "G2_WORKER_SERVICE_ACCOUNT_ID="+c.Config.Prov.G2WorkerServiceAccountID)
	}

	// If we have a cluster ID, then add it as an environment variable
	if c.Config.Prov.ClusterID != "" {
		env = append(env, "VPCCTL_CLUSTER_ID="+c.Config.Prov.ClusterID)
	}

	// If a list of nodes was specified, then add the node names as an environment variable
	if len(nodes) > 0 {
		nodeNames := []string{}
		for _, node := range nodes {
			nodeNames = append(nodeNames, node.Name)
		}
		nodeList := strings.Join(nodeNames, ",")
		// If the generated string is > 16K, don't add it to the env
		if len(nodeList) < 16*1024 {
			env = append(env, "VPCCTL_NODE_LIST="+nodeList)
		}
	}

	return env
}

// ensureVpcLoadBalancer creates a new load balancer 'name', or updates the existing one. Returns the status of the balancer
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) ensureVpcLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	lbName := c.getVpcLoadBalancerName(service)
	klog.Infof(
		"EnsureLoadBalancer(%v, %v, %v) - Service Name: %v - Selector: %v",
		lbName,
		clusterName,
		service.Annotations,
		service.Name,
		service.Spec.Selector,
	)

	command := c.determineCommandArgs("CREATE-LB", lbName, service)
	outArray, err := execVpcCommand(command, c.determineVpcEnvSettings(nodes))
	if err != nil {
		return nil, c.Recorder.VpcLoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed, lbName,
			fmt.Sprintf("Failed executing command [%s]: %v", command, err),
		)
	}
	for _, line := range outArray {
		if len(line) < 2 || !strings.Contains(line, ": ") {
			continue
		}
		lineType := strings.Split(line, ":")[0]             // Grab first part of the output line
		lineData := strings.TrimPrefix(line, lineType+": ") // Remainder of the output line
		switch lineType {
		case "ERROR":
			klog.Error(lineData)
			return nil, c.Recorder.VpcLoadBalancerServiceWarningEvent(
				service, CreatingCloudLoadBalancerFailed, lbName,
				fmt.Sprintf("Failed ensuring LoadBalancer: %v", lineData))
		case "INFO":
			klog.Info(lineData)
		case "PENDING":
			klog.Warningf("Load balancer %v is busy: %v", lbName, lineData) // Not sure what to return in this case
			if isFeatureEnabled(service, networkLoadBalancerFeature) {
				// For NLB, we are going to return PENDING until the VPC LB goes to online/active state.
				// Don't generate a WARNING event for this case since this is part of the normal Create NLB code path
				//
				// Note: A warning event IS still be generated by Kubernetes because we are returning an error back on this EnsureLoadBalancer function
				message := fmt.Sprintf("%v for service %v is busy: %v",
					lbName, types.NamespacedName{Namespace: service.ObjectMeta.Namespace, Name: service.ObjectMeta.Name}, lineData)
				return nil, errors.New(message)
			}
			return nil, c.Recorder.VpcLoadBalancerServiceWarningEvent(
				service, CreatingCloudLoadBalancerFailed, lbName,
				fmt.Sprintf("LoadBalancer is busy: %v", lineData))
		case "SUCCESS":
			klog.Infof("Load balancer %v created.  Hostname: %v", lbName, lineData)
			return getVpcLoadBalancerStatus(service, lineData), nil
		default:
			klog.Warning(line)
		}
	}
	return nil, c.Recorder.VpcLoadBalancerServiceWarningEvent(
		service, CreatingCloudLoadBalancerFailed, lbName,
		"Invalid response from command")
}

// updateVpcLoadBalancer updates hosts under the specified load balancer.
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) updateVpcLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	lbName := c.getVpcLoadBalancerName(service)
	klog.Infof("UpdateLoadBalancer(%v, %v, %v, %v)", lbName, clusterName, service, len(nodes))

	command := c.determineCommandArgs("UPDATE-LB", lbName, service)
	outArray, err := execVpcCommand(command, c.determineVpcEnvSettings(nodes))
	if err != nil {
		return c.Recorder.VpcLoadBalancerServiceWarningEvent(
			service, UpdatingCloudLoadBalancerFailed, lbName,
			fmt.Sprintf("Failed executing command [%s]: %v", command, err),
		)
	}
	for _, line := range outArray {
		if len(line) < 2 || !strings.Contains(line, ": ") {
			continue
		}
		lineType := strings.Split(line, ":")[0]             // Grab first part of the output line
		lineData := strings.TrimPrefix(line, lineType+": ") // Remainder of the output line
		switch lineType {
		case "ERROR":
			klog.Error(lineData)
			return c.Recorder.VpcLoadBalancerServiceWarningEvent(
				service, UpdatingCloudLoadBalancerFailed, lbName,
				fmt.Sprintf("Failed updating LoadBalancer: %v", lineData))
		case "INFO":
			klog.Info(lineData)
		case "PENDING":
			klog.Warningf("Load balancer %v is busy: %v", lbName, lineData) // Not sure what to return in this case
			return c.Recorder.VpcLoadBalancerServiceWarningEvent(
				service, UpdatingCloudLoadBalancerFailed, lbName,
				fmt.Sprintf("LoadBalancer is busy: %v", lineData))
		case "SUCCESS":
			klog.Infof("Load balancer %v updated", lbName)
			return nil
		default:
			klog.Warning(line)
		}
	}
	return c.Recorder.VpcLoadBalancerServiceWarningEvent(
		service, UpdatingCloudLoadBalancerFailed, lbName,
		"Invalid response from command")
}

// ensureVpcLoadBalancerDeleted deletes the specified load balancer if it
// exists, returning nil if the load balancer specified either didn't exist or
// was successfully deleted.
// This construction is useful because many cloud providers' load balancers
// have multiple underlying components, meaning a Get could say that the LB
// doesn't exist even if some part of it is still laying around.
// Implementations must treat the *v1.Service parameter as read-only and not modify it.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) ensureVpcLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	lbName := c.getVpcLoadBalancerName(service)
	klog.Infof("EnsureLoadBalancerDeleted(%v, %v, %v)", lbName, clusterName, service)

	command := c.determineCommandArgs("DELETE-LB", lbName, service)
	outArray, err := execVpcCommand(command, c.determineVpcEnvSettings(nil))
	if err != nil {
		return c.Recorder.VpcLoadBalancerServiceWarningEvent(
			service, DeletingCloudLoadBalancerFailed, lbName,
			fmt.Sprintf("Failed executing command [%s]: %v", command, err),
		)
	}
	for _, line := range outArray {
		if len(line) < 2 || !strings.Contains(line, ": ") {
			continue
		}
		lineType := strings.Split(line, ":")[0]             // Grab first part of the output line
		lineData := strings.TrimPrefix(line, lineType+": ") // Remainder of the output line
		switch lineType {
		case "ERROR":
			klog.Error(lineData)
			return c.Recorder.VpcLoadBalancerServiceWarningEvent(
				service, DeletingCloudLoadBalancerFailed, lbName,
				fmt.Sprintf("Failed deleting LoadBalancer: %v", lineData))
		case "INFO":
			klog.Info(lineData)
		case "NOT_FOUND":
			klog.Infof("Load balancer %v not found", lbName)
			return nil
		case "PENDING":
			klog.Warningf("Load balancer %v is busy: %v", lbName, lineData) // Not sure what to return in this case
			return c.Recorder.VpcLoadBalancerServiceWarningEvent(
				service, DeletingCloudLoadBalancerFailed, lbName,
				fmt.Sprintf("LoadBalancer is busy: %v", lineData))
		case "SUCCESS":
			klog.Infof("Load balancer %v deleted", lbName)
			return nil
		default:
			klog.Warning(line)
		}
	}
	return c.Recorder.VpcLoadBalancerServiceWarningEvent(
		service, DeletingCloudLoadBalancerFailed, lbName,
		"Invalid response from command")
}

// findField accepts a line of data from the vpcctl binary and attempts
// to retrieve the value/data associated with the specified prefix.
// Data passed from the Binary is of the following form:
// <DATA TYPE>: <DATA> where <DATA> itself can contain a space delineated collection 'key:value' pairs
func findField(lineData, prefix string) string {
	fields := strings.Fields(lineData)

	for _, field := range fields {
		key := strings.Split(field, ":")[0]
		if key == prefix && key != field {
			return strings.Split(field, ":")[1]
		}
	}

	return ""
}

// isNewLoadBalancer indicates whether the Kubernetes load balancer
// service object has been created in the last 24 hours. This is useful
// for avoiding event generation in the edge case where CCM restarts during VPC load balancer creation
func isNewLoadBalancer(lb *v1.Service) bool {
	currentTime := time.Now().Unix()
	serviceCreationTime := lb.ObjectMeta.CreationTimestamp.Unix()

	// Has the load balancer object been created in the last 24 hours?
	return currentTime-serviceCreationTime <= 86400
}

// monitorVpcLoadBalancers accepts a list of services (of all types),
// verifies that each Kubernetes load balancer service has a
// corresponding VPC load balancer object in RIaaS, and creates Kubernetes
// events based on the load balancer's status. `data` is a map from a load balancer's unique Service ID
// to its status. This persists load balancer status between consecutive monitor calls.
func monitorVpcLoadBalancers(c *Cloud, services *v1.ServiceList, status map[string]string, triggerEvent EventRecorder) {
	klog.Info("Monitoring VPC Load Balancers...")

	// Build a map of all Kubernetes load balancer service objects
	serviceMap := map[string]*v1.Service{}
	for _, svc := range services.Items {
		if svc.Spec.Type == v1.ServiceTypeLoadBalancer {
			lbSvc := svc
			serviceMap[string(svc.UID)] = &lbSvc
		}
	}

	// Return if there are no load balancer services to monitor
	if len(serviceMap) == 0 {
		klog.Info("No Load Balancers to monitor, exiting...")
		return
	}

	command := "MONITOR"
	outArray, err := execVpcCommand(command, c.determineVpcEnvSettings(nil))
	if err != nil {
		klog.Errorf("Error calling vpcctl binary: %s", err)
		return
	}

	// Generate events based on response from 'vpcctl' binary
	for _, line := range outArray {
		klog.Info("Processing line: ", line)
		if len(line) < 2 || !strings.Contains(line, ": ") {
			continue
		}
		lineType := strings.Split(line, ":")[0]             // Grab first part of the output line
		lineData := strings.TrimPrefix(line, lineType+": ") // Remainder of the output line

		switch lineType {
		case "INFO":
			// A corresponding VPC load balancer exists in RIaaS. We need to further parse binary response and generate
			// events based on load balancer status

			// Obtain information necessary for event creation
			serviceID := findField(lineData, vpcLBServiceIDPrefix)
			service, exists := serviceMap[serviceID]
			if !exists {
				// We do not have a load balancer service associated with this service UID returned from the binary
				// OR this is a line without data (ex: "INFO: Entering monitor")
				continue
			}

			newStatus := findField(lineData, vpcLBStatusPrefix) // Looking for Status:<status-data>
			oldStatus, oldStatusExists := status[serviceID]

			if oldStatusExists {
				// We have prior state for this load balancer from a previous call to monitorVpcLoadBalancer()
				// Compare current VPC LB status with the previous VPC LB status and trigger events for a variety of cases
				klog.Infof("Previous State: %s    Current State: %s", oldStatus, newStatus)

				// If the status of the VPC load balancer is transitioning from any
				// non active state to 'online/active' --> NORMAL EVENT.
				if newStatus == vpcStatusOnlineActive {
					if oldStatus != vpcStatusOnlineActive {
						// If this is a network load balancer, we don't want to signal the NORMAL EVENT
						// (and potentially wake up some application that is waiting for this normal even to appear)
						// unless EnsureLoadBalancer has set the hostname and static IP address in the service spec
						if isFeatureEnabled(service, networkLoadBalancerFeature) {
							if service.Status.LoadBalancer.Ingress == nil || service.Status.LoadBalancer.Ingress[0].Hostname == "" {
								// Ignore this new status and wait for EnsureLoadBalancer to set the hostname
								newStatus = oldStatus
							} else {
								triggerEvent(c.Recorder, service, newStatus)
							}
						} else {
							triggerEvent(c.Recorder, service, newStatus)
						}
					}
				} else {
					// If the status of the VPC load balancer is not 'online/active'
					// on consecutive calls to Monitor --> EVENT (Normal OR Warning)
					if oldStatus == newStatus {
						triggerEvent(c.Recorder, service, newStatus)
					}
				}
			} else if newStatus == vpcStatusOnlineActive && isNewLoadBalancer(service) {
				// We do not have prior state for this load balancer, either because it was recently created or
				// the CCM was restarted due to failure/rolling update

				// Handle the case when the VPC load balancer is fully created in between successive calls to Monitor
				// 'offline/create_pending` state could be lost so trigger event to ensure
				// Ingress does not miss notification of newly created LB
				klog.Info("New VPC load balancer has no prior state. Triggering event in case load balancer creation began and completed in between monitor")
				triggerEvent(c.Recorder, service, newStatus)
			}

			// Store status in data map so its available to the next call to monitorVpcLoadBalancers()
			status[serviceID] = newStatus

		case "NOT_FOUND":
			// Unable to find VPC load balancer object in RIaaS which corresponds to this Kubernetes service object.
			// Obtain information necessary for event creation
			serviceID := findField(lineData, vpcLBServiceIDPrefix)
			service, exists := serviceMap[serviceID]
			if !exists {
				// We do not have a load balancer service associated with this service UID returned from the binary
				continue
			}

			newStatus := vpcStatusOfflineNotFound
			oldStatus, oldStatusExists := status[serviceID]

			// Avoid Not Found event generation while waiting for cluster creation.
			// This requires that a load balancer be assigned a non-empty status
			// before the monitor will keep track of not_found status
			if oldStatusExists {
				// If the status of the VPC load balancer is found in state 'offline/not_found'
				// on consecutive calls to Monitor() --> NOT FOUND EVENT
				if newStatus == oldStatus {
					triggerEvent(c.Recorder, service, newStatus)
				}

				status[serviceID] = newStatus
			}

			klog.Infof("Corresponding VPC load balancer not found for service: %s", serviceID)
		default:
			// Log unexpected information from binary
			klog.Error(line)
		}
	}
}

// EventRecorder creates a type for 'triggerEvent' function and makes for a cleaner 'monitorVpcLoadBalancer' function signature
type EventRecorder func(*CloudEventRecorder, *v1.Service, string)

// triggerEvent generates different types of cloud events for a given service
// NOTE(czachman): Should "Normal" event be the default?
func triggerEvent(eventRecorder *CloudEventRecorder, service *v1.Service, newStatus string) {
	switch newStatus {
	case vpcStatusOfflineCreatePending: // Ignore long VPC LB creates for now
	case vpcStatusOfflineFailed: // Failed Event
		eventRecorder.VpcLoadBalancerServiceWarningEvent(
			service, CloudVPCLoadBalancerFailed, service.Name,
			"The VPC load balancer that routes requests to this Kubernetes LoadBalancer service is offline. For troubleshooting steps, see <https://ibm.biz/vpc-lb-ts>",
		)
	case vpcStatusOfflineNotFound: // Not Found Warning Event
		eventRecorder.VpcLoadBalancerServiceWarningEvent(
			service, CloudVPCLoadBalancerNotFound, service.Name,
			"The VPC load balancer that routes requests to this Kubernetes LoadBalancer service was deleted from your VPC account. To recreate the VPC load balancer, restart the Kubernetes master by running 'ibmcloud ks cluster master refresh --cluster <cluster_name_or_id>'.",
		)
	case vpcStatusOfflineMaintenancePending: // Maintenance Warning Event
		eventRecorder.VpcLoadBalancerServiceWarningEvent(
			service, CloudVPCLoadBalancerMaintenance, service.Name,
			"The VPC load balancer that routes requests to this Kubernetes LoadBalancer service is under maintenance.",
		)
	default: // Normal Event
		eventRecorder.VpcLoadBalancerServiceNormalEvent(
			service, CloudVPCLoadBalancerNormalEvent, service.Name,
			fmt.Sprintf("The VPC load balancer that routes requests to this Kubernetes LoadBalancer service is currently %s.", newStatus),
		)
	}
}
