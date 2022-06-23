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
	"fmt"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"

	"cloud.ibm.com/cloud-provider-ibm/pkg/klog"
	"cloud.ibm.com/cloud-provider-ibm/pkg/vpcctl"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
)

// envVarPublicEndPoint is an environmental variable used to select public service endpoint
// Accepted value is "true", if any other value is set, it will be ignored.
const envVarPublicEndPoint = "ENABLE_VPC_PUBLIC_ENDPOINT"

// shouldPrivateEndpointBeEnabled - Determine if private service endpoint should be enabled
func shouldPrivateEndpointBeEnabled() bool {
	// If ENABLE_VPC_PUBLIC_ENDPOINT env variable is set to true, do not use private endpoints so return false
	return strings.ToLower(os.Getenv(envVarPublicEndPoint)) != "true"
}

// GetCloudVpc - Retrieve the VPC cloud object.  Return nil if not initialized.
func (c *Cloud) GetCloudVpc() *vpcctl.CloudVpc {
	return vpcctl.GetCloudVpc()
}

// InitCloudVpc - Initialize the VPC cloud logic
func (c *Cloud) InitCloudVpc(enablePrivateEndpoint bool) (*vpcctl.CloudVpc, error) {
	// Extract the VPC cloud object. If set, return it
	cloudVpc := c.GetCloudVpc()
	if cloudVpc != nil {
		return cloudVpc, nil
	}
	// Initialize config based on values in the cloud provider
	config, err := c.NewConfigVpc(enablePrivateEndpoint)
	if err != nil {
		return nil, err
	}
	// Allocate a new VPC Cloud object and save it if successful
	var recorder record.EventRecorder
	if c.Recorder != nil {
		recorder = c.Recorder.Recorder
	}
	return vpcctl.NewCloudVpc(c.KubeClient, config, recorder)
}

// isProviderVpc - Is the current cloud provider running in VPC environment?
func (c *Cloud) isProviderVpc() bool {
	provider := c.Config.Prov.ProviderType
	return provider == vpcctl.VpcProviderTypeGen2
}

// NewConfigVpc - Create the ConfigVpc from the current Cloud object
func (c *Cloud) NewConfigVpc(enablePrivateEndpoint bool) (*vpcctl.ConfigVpc, error) {
	// Make sure Cloud config has been initialized
	if c.Config == nil {
		return nil, fmt.Errorf("Cloud config not initialized")
	}
	// Initialize config based on values in the cloud provider
	config := &vpcctl.ConfigVpc{
		AccountID:         c.Config.Prov.AccountID,
		ClusterID:         c.Config.Prov.ClusterID,
		EnablePrivate:     enablePrivateEndpoint,
		ProviderType:      c.Config.Prov.ProviderType,
		Region:            c.Config.Prov.Region,
		ResourceGroupName: c.Config.Prov.G2ResourceGroupName,
		SubnetNames:       c.Config.Prov.G2VpcSubnetNames,
		WorkerAccountID:   c.Config.Prov.G2WorkerServiceAccountID,
		VpcName:           c.Config.Prov.G2VpcName,
	}
	// If the G2Credentials is set, then look up the API key
	if c.Config.Prov.G2Credentials != "" {
		klog.Infof("Reading cloud credential from: %v", c.Config.Prov.G2Credentials)
		fileData, err := os.ReadFile(c.Config.Prov.G2Credentials)
		if err != nil {
			return nil, fmt.Errorf("Failed to read credentials from %s: %v", c.Config.Prov.G2Credentials, err)
		}
		config.APIKeySecret = strings.TrimSpace(string(fileData))
		// If there are spaces in the API key, then reset it
		if strings.Contains(config.APIKeySecret, " ") {
			klog.Infof("API key read from file is not valid: [%v]", config.APIKeySecret)
			config.APIKeySecret = ""
		}
	}
	return config, nil
}

// VpcEnsureLoadBalancer - Creates a new VPC load balancer or updates the existing one. Returns the status of the balancer
func (c *Cloud) VpcEnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {
	lbName := c.vpcGetLoadBalancerName(service)
	klog.Infof("EnsureLoadBalancer(lbName:%v, Service:{%v}, NodeCount:%v)", lbName, c.vpcGetServiceDetails(service), len(nodes))
	if len(nodes) == 0 {
		errString := "There are no available nodes for LoadBalancer"
		klog.Errorf(errString)
		return nil, fmt.Errorf(errString)
	}
	vpc, err := c.InitCloudVpc(shouldPrivateEndpointBeEnabled())
	if err != nil {
		errString := fmt.Sprintf("Failed initializing VPC: %v", err)
		klog.Errorf(errString)
		return nil, c.Recorder.VpcLoadBalancerServiceWarningEvent(service, CreatingCloudLoadBalancerFailed, lbName, errString)
	}
	// Attempt to create/update the VPC load balancer for this service
	return vpc.EnsureLoadBalancer(lbName, service, nodes)
}

// VpcEnsureLoadBalancerDeleted - Deletes the specified load balancer if it exists,
// returning nil if the load balancer specified either didn't exist or was successfully deleted.
func (c *Cloud) VpcEnsureLoadBalancerDeleted(ctx context.Context, clusterName string, service *v1.Service) error {
	lbName := c.vpcGetLoadBalancerName(service)
	klog.Infof("EnsureLoadBalancerDeleted(lbName:%v, Service:{%v})", lbName, c.vpcGetServiceDetails(service))
	vpc, err := c.InitCloudVpc(shouldPrivateEndpointBeEnabled())
	if err != nil {
		errString := fmt.Sprintf("Failed initializing VPC: %v", err)
		klog.Errorf(errString)
		return c.Recorder.VpcLoadBalancerServiceWarningEvent(service, DeletingCloudLoadBalancerFailed, lbName, errString)
	}
	// Attempt to delete the VPC load balancer
	return vpc.EnsureLoadBalancerDeleted(lbName, service)
}

// VpcGetLoadBalancer - Returns whether the specified load balancer exists, and
// if so, what its status is.
func (c *Cloud) VpcGetLoadBalancer(ctx context.Context, clusterName string, service *v1.Service) (*v1.LoadBalancerStatus, bool, error) {
	lbName := c.vpcGetLoadBalancerName(service)
	klog.Infof("GetLoadBalancer(lbName:%v, Service:{%v})", lbName, c.vpcGetServiceDetails(service))
	vpc, err := c.InitCloudVpc(shouldPrivateEndpointBeEnabled())
	if err != nil {
		errString := fmt.Sprintf("Failed initializing VPC: %v", err)
		klog.Errorf(errString)
		return nil, false, c.Recorder.VpcLoadBalancerServiceWarningEvent(service, GettingCloudLoadBalancerFailed, lbName, errString)
	}
	// Retrieve the status of the VPC load balancer
	return vpc.GetLoadBalancer(lbName, service)
}

// vpcGetLoadBalancerName - Returns the name of the load balancer
func (c *Cloud) vpcGetLoadBalancerName(service *v1.Service) string {
	clusterID := c.Config.Prov.ClusterID
	serviceID := strings.ReplaceAll(string(service.UID), "-", "")
	ret := vpcctl.VpcLbNamePrefix + "-" + clusterID + "-" + serviceID
	// Limit the LB name to 63 characters
	if len(ret) > 63 {
		ret = ret[:63]
	}
	return ret
}

// vpcGetServiceDetails - Returns the string of the Kube LB service key fields
func (c *Cloud) vpcGetServiceDetails(service *v1.Service) string {
	if service == nil {
		return "<nil>"
	}
	// Only include the service annotations that we care about in the log
	annotations := map[string]string{}
	for k, v := range service.ObjectMeta.Annotations {
		if strings.Contains(k, "ibm-load-balancer-cloud-provider") {
			annotations[k] = v
		}
	}
	// Only include the port information that we care about: protocol, ext port, node port
	ports := []string{}
	for _, port := range service.Spec.Ports {
		portString := fmt.Sprintf("%v-%v-%v", port.Protocol, port.Port, port.NodePort)
		ports = append(ports, strings.ToLower(portString))
	}
	return fmt.Sprintf("Name:%v NameSpace:%v UID:%v Annotations:%v Ports:%v ExternalTrafficPolicy:%v HealthCheckNodePort:%v Status:%+v",
		service.ObjectMeta.Name,
		service.ObjectMeta.Namespace,
		service.ObjectMeta.UID,
		annotations,
		ports,
		service.Spec.ExternalTrafficPolicy,
		service.Spec.HealthCheckNodePort,
		service.Status)
}

// VpcMonitorLoadBalancers accepts a list of services (of all types), verifies that each Kubernetes load balancer service has a
// corresponding VPC load balancer object, and creates Kubernetes events based on the load balancer's status.
// `status` is a map from a load balancer's unique Service ID to its status.
// This persists load balancer status between consecutive monitor calls.
func (c *Cloud) VpcMonitorLoadBalancers(services *v1.ServiceList, status map[string]string) {
	// If there are no load balancer services to monitor, don't even initCloudVpc, just return.
	if services == nil || len(services.Items) == 0 {
		klog.Infof("No Load Balancers to monitor, returning")
		return
	}
	vpc, err := c.InitCloudVpc(shouldPrivateEndpointBeEnabled())
	if err != nil {
		klog.Errorf("Failed initializing VPC: %v", err)
		return
	}
	vpc.MonitorLoadBalancers(services, status)
}

// VpcUpdateLoadBalancer updates hosts under the specified load balancer
func (c *Cloud) VpcUpdateLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) error {
	lbName := c.vpcGetLoadBalancerName(service)
	klog.Infof("UpdateLoadBalancer(lbName:%v, Service:{%v}, NodeCount:%v)", lbName, c.vpcGetServiceDetails(service), len(nodes))
	if len(nodes) == 0 {
		errString := "There are no available nodes for LoadBalancer"
		klog.Errorf(errString)
		return fmt.Errorf(errString)
	}
	vpc, err := c.InitCloudVpc(shouldPrivateEndpointBeEnabled())
	if err != nil {
		errString := fmt.Sprintf("Failed initializing VPC: %v", err)
		klog.Errorf(errString)
		return c.Recorder.VpcLoadBalancerServiceWarningEvent(service, UpdatingCloudLoadBalancerFailed, lbName, errString)
	}
	// Update the VPC load balancer
	return vpc.EnsureLoadBalancerUpdated(lbName, service, nodes)
}

// WatchCloudCredential watches for changes to the cloud credentials and resets the VPC settings
func (c *Cloud) WatchCloudCredential() error {
	if c.Config.Prov.G2Credentials == "" {
		return fmt.Errorf("No cloud credential file to watch")
	}
	klog.Infof("Watch the cloud credential file: %v", c.Config.Prov.G2Credentials)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("Failed to create watcher for cloud credential file: %v", err)
	}
	// Go function to handle updates
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				klog.Infof("Credential watch event: %v", event)
				// Write event == file was updated. Don't care about other events
				if event.Op&fsnotify.Write == fsnotify.Write {
					fileData, err := os.ReadFile(c.Config.Prov.G2Credentials)
					if err != nil {
						klog.Warningf("Failed to read credentials from %s: %v", c.Config.Prov.G2Credentials, err)
					} else if cred := strings.TrimSpace(string(fileData)); strings.Contains(cred, " ") {
						klog.Infof("Cloud credential is not valid: [%s]", cred)
					} else {
						klog.Infof("Reset the cloud credentials")
						vpcctl.ResetCloudVpc()
						if c.Metadata != nil && c.Metadata.vpcClient != nil {
							c.Metadata.vpcClient = nil
						}
					}
				}
			case err := <-watcher.Errors:
				klog.Infof("Credential watch error: %v", err)
			}
		}
	}()
	err = watcher.Add(c.Config.Prov.G2Credentials)
	if err != nil {
		return fmt.Errorf("Failed to add credential file to watch: %v", err)
	}
	return nil
}
