/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2025 All Rights Reserved.
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
	"net/url"
	"strings"

	"github.com/IBM-Cloud/power-go-client/clients/instance"
	"github.com/IBM-Cloud/power-go-client/ibmpisession"
	"github.com/IBM-Cloud/power-go-client/power/models"
	"github.com/IBM/go-sdk-core/v5/core"
	rc "github.com/IBM/platform-services-go-sdk/resourcecontrollerv2"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

const (
	// powerVSResourceID is Power VS power-iaas service id, can be retrieved using ibmcloud cli
	// ibmcloud catalog service power-iaas
	powerVSResourceID = "abd259f0-9990-11e8-acc8-b9f54a8f1661"

	// powerVSResourcePlanID is Power VS power-iaas plan id, can be retrieved using ibmcloud cli
	// ibmcloud catalog service power-iaas
	powerVSResourcePlanID = "f165dd34-3a40-423b-9d95-e90a23f724dd"
)

// dhcpCacheStore is a cache store to hold the Power VS VM DHCP IP.
var dhcpCacheStore cache.Store

func init() {
	dhcpCacheStore = initialiseDHCPCacheStore()
}

// Client is a wrapper object for actual PowerVS SDK clients to allow for easier testing.
type Client interface {
	GetInstances() (*models.PVMInstances, error)
	GetInstanceByName(name string) (*models.PVMInstance, error)
	GetDHCPServers() (models.DHCPServers, error)
	GetDHCPServerByID(string) (*models.DHCPServerDetail, error)
	GetCloudServiceInstanceByName(string) (*rc.ResourceInstance, error)
}

// ibmPowerVSClient makes call to IBM Cloud Power VS APIs
type ibmPowerVSClient struct {
	provider        Provider
	sdk             Client
	cloudInstanceID string
}

// powerVSClient helps in initializing ibmPowerVSClient Client and implements Client interface
type powerVSClient struct {
	cloudInstanceID string
	instanceClient  *instance.IBMPIInstanceClient
	dhcpClient      *instance.IBMPIDhcpClient
	resourceClient  *rc.ResourceControllerV2
}

// newPowerVSSdkClient initializes a new sdk client and can be overridden by testing
var newPowerVSSdkClient = func(provider *Provider) (Client, error) {
	if provider.PowerVSCloudInstanceName == "" && provider.PowerVSCloudInstanceID == "" {
		return nil, fmt.Errorf("both service instance id and name cannot be empty")
	}
	credential, err := readCredential(*provider)
	if err != nil {
		klog.Errorf("Failed to read the credentials, Error: %v", err)
		return nil, err
	}

	// Create the authenticator
	authenticator := &core.IamAuthenticator{
		ApiKey: credential,
	}

	// If the IAM endpoint override was specified in the config, update the URL
	if provider.IamEndpointOverride != "" {
		authenticator.URL = provider.IamEndpointOverride
	}

	// Create the session options struct
	options := &ibmpisession.IBMPIOptions{
		Authenticator: authenticator,
		UserAccount:   provider.AccountID,
		Zone:          provider.PowerVSZone,
	}

	// If the PowerVS endpoint override was specified in the config, update the URL
	if provider.PowerVSEndpointOverride != "" {
		options.URL = provider.PowerVSEndpointOverride
	}

	// Construct the session service instance
	session, err := ibmpisession.NewIBMPISession(options)
	if err != nil {
		klog.Errorf("Failed to create new IBMPISession, Error: %v", err)
		return nil, err
	}
	client := &powerVSClient{}
	if provider.PowerVSCloudInstanceID == "" {
		rcOptions := &rc.ResourceControllerV2Options{
			Authenticator: authenticator,
		}
		// If the resource controller endpoint override was specified in the config, update the URL
		if provider.RcEndpointOverride != "" {
			rcOptions.URL = provider.RcEndpointOverride
		}

		rcv2, err := rc.NewResourceControllerV2(rcOptions)
		if err != nil {
			klog.Errorf("failed to create resource controller to fetch service instance id, error: %v", err)
			return nil, err
		}
		if rcv2 == nil {
			return nil, fmt.Errorf("unable to get resource controller")
		}
		client.resourceClient = rcv2
		cloudInstance, err := client.GetCloudServiceInstanceByName(provider.PowerVSCloudInstanceName)
		if err != nil {
			klog.Errorf("failed to get serviceInstance with name %s, err: %v", err, provider.PowerVSCloudInstanceName)
			return nil, err
		}
		if cloudInstance == nil {
			return nil, fmt.Errorf("cannot fetch service instance with name %s, service instance is nil", provider.PowerVSCloudInstanceName)
		}
		// Update the cloud instance id to provider.PowerVSCloudInstanceID so that we can refer it while setting provider id
		provider.PowerVSCloudInstanceID = *cloudInstance.GUID
	}
	client.cloudInstanceID = provider.PowerVSCloudInstanceID
	ctx := context.Background()
	client.instanceClient = instance.NewIBMPIInstanceClient(ctx, session, client.cloudInstanceID)
	client.dhcpClient = instance.NewIBMPIDhcpClient(ctx, session, client.cloudInstanceID)
	return client, nil
}

// newPowerVSClient initializes a new validated powerVSClient
func newPowerVSClient(provider *Provider) (*ibmPowerVSClient, error) {
	// create Power VS sdk client
	sdk, err := newPowerVSSdkClient(provider)
	if err != nil {
		klog.Errorf("Failed to create newPowerVSSdkClient, Error: %v", err)
		return nil, err
	}

	return &ibmPowerVSClient{
		provider: *provider,
		sdk:      sdk,
	}, nil
}

// isProviderPowerVS returns true when PowerVS specific parameters are set in Provider
func isProviderPowerVS(provider Provider) bool {
	if (provider.PowerVSCloudInstanceID != "" || provider.PowerVSCloudInstanceName != "") && provider.PowerVSRegion != "" && provider.PowerVSZone != "" {
		return true
	}
	return false
}

// GetInstanceByName return the Power VS instance corresponding to the provided name
func (p *powerVSClient) GetInstanceByName(name string) (*models.PVMInstance, error) {
	instances, err := p.GetInstances()
	if err != nil {
		klog.Errorf("Failed to get instance list, Error: %v", err)
		return nil, fmt.Errorf("failed to get the instance list, Error: %v", err)
	}
	for _, i := range instances.PvmInstances {
		if *i.ServerName == name {
			klog.Infof("instance name: %s id %s", name, *i.PvmInstanceID)
			return p.instanceClient.Get(*i.PvmInstanceID)
		}
	}
	return nil, fmt.Errorf("instance %s not found in %s cloud instance ", name, p.cloudInstanceID)
}

// GetInstances returns all the Power VS instances
func (p *powerVSClient) GetInstances() (*models.PVMInstances, error) {
	return p.instanceClient.GetAll()
}

func (p *powerVSClient) GetDHCPServers() (models.DHCPServers, error) {
	return p.dhcpClient.GetAll()
}

func (p *powerVSClient) GetDHCPServerByID(id string) (*models.DHCPServerDetail, error) {
	return p.dhcpClient.Get(id)
}

func (p *powerVSClient) GetCloudServiceInstanceByName(name string) (*rc.ResourceInstance, error) {
	if name == "" {
		return nil, fmt.Errorf("cannot fetch service instance as service instance name is empty")
	}
	var serviceInstancesList []rc.ResourceInstance
	f := func(start string) (bool, string, error) {
		listServiceInstanceOptions := &rc.ListResourceInstancesOptions{
			Name:           &name,
			ResourceID:     ptr.To(powerVSResourceID),
			ResourcePlanID: ptr.To(powerVSResourcePlanID),
		}
		if start != "" {
			listServiceInstanceOptions.Start = &start
		}

		serviceInstances, _, err := p.resourceClient.ListResourceInstances(listServiceInstanceOptions)
		if err != nil {
			return false, "", err
		}
		if serviceInstances != nil {
			serviceInstancesList = append(serviceInstancesList, serviceInstances.Resources...)
			nextURL, err := serviceInstances.GetNextStart()
			if err != nil {
				return false, "", err
			}
			if nextURL == nil {
				return true, "", nil
			}
			return false, *nextURL, nil
		}
		return true, "", nil
	}

	if err := PagingHelper(f); err != nil {
		return nil, fmt.Errorf("error listing service instances %v", err)
	}
	// log useful error message
	switch len(serviceInstancesList) {
	case 0:
		errStr := fmt.Errorf("does exist any cloud service instance with name %s", name)
		klog.Error(errStr)
		return nil, fmt.Errorf("does exist any cloud service instance with name %s", name)
	case 1:
		klog.Infof("serviceInstance %s found with ID: %v", name, serviceInstancesList[0].GUID)
		return &serviceInstancesList[0], nil
	default:
		errStr := fmt.Errorf("there exist more than one service instance ID with with same name %s, Try setting serviceInstance.ID", name)
		klog.Error(errStr)
		return nil, errStr
	}
}

// populateNodeMetadata forms the node metadata from instance details
func (p *ibmPowerVSClient) populateNodeMetadata(nodeName string, node *NodeMetadata) error {

	// Try to fetch the nodeMetadata from cache
	obj, exists, err := dhcpCacheStore.GetByKey(nodeName)
	if err != nil {
		klog.Errorf("Node %s failed to fetch the node metadata from cache, error: %v", nodeName, err)
	}
	if exists {
		node = obj.(nodeMetadataCache).Metadata
		klog.Infof("Node %s found metadata %+v from DHCP cache", nodeName, node)
		return nil
	}

	// Get Power VS Instance.
	pvsInstance, err := p.sdk.GetInstanceByName(nodeName)
	if err != nil {
		return err
	}
	// Check if instance is not nil.
	if pvsInstance == nil {
		return errors.New("Could not retrieve a Power instance: name=" + nodeName)
	}
	if pvsInstance.PvmInstanceID == nil || *pvsInstance.PvmInstanceID == "" {
		return fmt.Errorf("could not retrieve valid instance id for Power instance name: %s instance id: %v", nodeName, pvsInstance.PvmInstanceID)
	}
	node.WorkerID = *pvsInstance.PvmInstanceID
	klog.Infof("Node %s worker id is %s", nodeName, node.WorkerID)

	node.InstanceType = pvsInstance.SysType
	klog.Infof("Node %s instance type is %s", nodeName, node.InstanceType)

	node.Region = p.provider.PowerVSRegion
	klog.Infof("Node %s region is %s", nodeName, node.Region)

	node.FailureDomain = p.provider.PowerVSZone
	klog.Infof("Node %s failureDomain is %s", nodeName, node.FailureDomain)

	for _, network := range pvsInstance.Networks {
		if strings.TrimSpace(network.ExternalIP) != "" {
			node.ExternalIP = strings.TrimSpace(network.ExternalIP)
		}
		if strings.TrimSpace(network.IPAddress) != "" {
			node.InternalIP = strings.TrimSpace(network.IPAddress)
		}
	}

	if node.ExternalIP == "" && node.InternalIP == "" {
		// If node ExternalIP and InternalIP is empty, try to fetch the IP from dhcp server.
		klog.Infof("Node %s fetching IP from DHCP server", nodeName)
		// Fetch the Network attached to instance.
		network, err := getPowerVSNetwork(pvsInstance)
		if err != nil {
			klog.Errorf("failed to fetch Power VS Network name error: %v", err)
			return err
		}
		// for DHCP network type will be "dynamic" for other networks type will be "fixed"
		if network.Type != "dynamic" {
			errStr := fmt.Errorf("node %s attached with network %s of type %s expecting Network Type to be dynamic to fetch IP from DHCP server ", nodeName, network.NetworkName, network.Type)
			klog.Error(errStr.Error())
			return errStr
		}
		// Fetch the DHCP server ID.
		dhcpServerID, err := p.getDHCPServerID(network.NetworkName)
		if err != nil {
			klog.Errorf("failed to fetch dhcp server id error: %v", err)
			return err
		}
		dhcpServerDetails, err := p.sdk.GetDHCPServerByID(dhcpServerID)
		if err != nil {
			klog.Errorf("failed to fetch dhcp server details with id: %s error: %v", dhcpServerID, err)
			return err
		}
		for _, lease := range dhcpServerDetails.Leases {
			if network.MacAddress == *lease.InstanceMacAddress {
				node.InternalIP = *lease.InstanceIP
				klog.Infof("Node %s found internal ip %s from dhcp lease", nodeName, node.InternalIP)
			}
		}
		if len(node.InternalIP) == 0 {
			errStr := fmt.Errorf("not able to find internal ip for pvm instance: %s with attached network name: %s", nodeName, network.NetworkName)
			klog.Error(errStr)
			return errStr
		}
	}

	// Update the cache with the node metadata
	err = dhcpCacheStore.Add(nodeMetadataCache{
		Name:     nodeName,
		Metadata: node,
	})
	if err != nil {
		klog.Errorf("Node %s failed to add node metadata to cache, Error %v", nodeName, err)
	}
	klog.Infof("Node %s internal IP is %s", nodeName, node.InternalIP)
	klog.Infof("Node %s external IP is %s", nodeName, node.ExternalIP)
	return nil
}

// getDHCPServerID fetches and returns the DHCP server ID.
func (p *ibmPowerVSClient) getDHCPServerID(networkName string) (string, error) {
	// If the DHCP server ID is not provided try to fetch it.
	// Get all the DHCP servers.
	dhcpServer, err := p.sdk.GetDHCPServers()
	if err != nil {
		klog.Errorf("failed to get DHCP server error: %v", err)
		return "", err
	}
	// Get the DHCP server ID.
	for _, server := range dhcpServer {
		if *server.Network.Name == networkName {
			klog.Infof("Found DHCP server with ID %s for network %s", *server.ID, networkName)
			return *server.ID, nil
		}
	}
	return "", fmt.Errorf("not able to get DHCP server ID for network %s", networkName)
}

// getPowerVSNetwork fetches and returns the Power VS Network
func getPowerVSNetwork(instance *models.PVMInstance) (*models.PVMInstanceNetwork, error) {
	// currently its assumed that there will be only 1 network attached to instance
	if len(instance.Networks) != 1 {
		errStr := fmt.Errorf("expecting only one network to be attached to vm %s but got %d", *instance.ServerName, len(instance.Networks))
		klog.Error(errStr.Error())
		return nil, errStr
	}
	return instance.Networks[0], nil
}

// getStartToken parses the given url string and gets the 'start' query param.
func getStartToken(nextURLS string) (string, error) {
	nextURL, err := url.Parse(nextURLS)
	if err != nil || nextURL == nil {
		return "", fmt.Errorf("could not parse next url for getting next resources %w", err)
	}

	start := nextURL.Query().Get("start")
	return start, nil
}

// PagingHelper while listing resources, can use this to get the start token for getting the next set of resources for processing
// start token will get fetched from nextURL returned by f and passed to the func f.
// f should take start as param and return three values isDone bool, nextURL string, e error.
// isDone  - represents no need to iterate for getting next set of resources.
// nextURL - if nextURL is present, will try to get the start token and pass it to f for next set of resource processing.
// e       - if e is not nil, will break and return the error.
func PagingHelper(f func(string) (bool, string, error)) error {
	start := ""
	var err error
	for {
		isDone, nextURL, e := f(start)

		if e != nil {
			err = e
			break
		}

		if isDone {
			break
		}

		// for paging over next set of resources getting the start token
		if nextURL != "" {
			start, err = getStartToken(nextURL)
			if err != nil {
				break
			}
		} else {
			break
		}
	}
	return err
}
