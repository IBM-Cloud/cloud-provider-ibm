/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2021, 2025 All Rights Reserved.
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
	"sort"
	"strconv"
	"strings"

	"github.com/go-openapi/strfmt"
	v1 "k8s.io/api/core/v1"
)

// CloudVpcSdk interface for SDK operations
type CloudVpcSdk interface {
	CreateLoadBalancer(lbName string, nodeList, poolList, subnetList []string, options *ServiceOptions) (*VpcLoadBalancer, error)
	CreateLoadBalancerListener(lbID, poolName, poolID string) (*VpcLoadBalancerListener, error)
	CreateLoadBalancerPool(lbID, poolName string, nodeList []string, options *ServiceOptions) (*VpcLoadBalancerPool, error)
	CreateLoadBalancerPoolMember(lbID, poolName, poolID, nodeID string) (*VpcLoadBalancerPoolMember, error)
	DeleteLoadBalancer(lbID string) error
	DeleteLoadBalancerListener(lbID, listenerID string) error
	DeleteLoadBalancerPool(lbID, poolID string) error
	DeleteLoadBalancerPoolMember(lbID, poolID, memberID string) error
	GetLoadBalancer(lbID string) (*VpcLoadBalancer, error)
	GetSubnet(subnetID string) (*VpcSubnet, error)
	ListLoadBalancers() ([]*VpcLoadBalancer, error)
	ListLoadBalancerListeners(lbID string) ([]*VpcLoadBalancerListener, error)
	ListLoadBalancerPools(lbID string) ([]*VpcLoadBalancerPool, error)
	ListLoadBalancerPoolMembers(lbID, poolID string) ([]*VpcLoadBalancerPoolMember, error)
	ListSubnets() ([]*VpcSubnet, error)
	ReplaceLoadBalancerPoolMembers(lbID, poolName, poolID string, nodeList []string) ([]*VpcLoadBalancerPoolMember, error)
	UpdateLoadBalancerPool(lbID, newPoolName string, existingPool *VpcLoadBalancerPool, options *ServiceOptions) (*VpcLoadBalancerPool, error)
}

// NewVpcSdkProvider - name of SDK interface
var NewVpcSdkProvider = NewVpcSdkGen2

// NewCloudVpcSdk - return the correct set of SDK library routines
func NewCloudVpcSdk(c *ConfigVpc) (CloudVpcSdk, error) {
	switch c.ProviderType {
	case VpcProviderTypeGen2:
		return NewVpcSdkProvider(c)
	case VpcProviderTypeFake:
		return NewVpcSdkFake()
	default:
		return nil, fmt.Errorf("invalid VPC ProviderType: %s", c.ProviderType)
	}
}

// VpcPoolNameFields - Structure for dealing with parts of the VPC pool name
type VpcPoolNameFields struct {
	Protocol string
	Port     int
	NodePort int
}

// extractFieldsFromPoolName - pool name has format of <protocol>-<port>-<nodePort>
func extractFieldsFromPoolName(poolName string) (*VpcPoolNameFields, error) {
	protocol, port, nodePort, err := extractProtocolPortsFromPoolName(poolName)
	return &VpcPoolNameFields{protocol, port, nodePort}, err
}

// extractProtocolPortsFromPoolName - pool name has format of <protocol>-<port>-<nodePort>
func extractProtocolPortsFromPoolName(poolName string) (string, int, int, error) {
	pool := strings.Split(poolName, "-")
	if len(pool) != 3 {
		return "", -1, -1, fmt.Errorf("invalid pool name, format not <protocol>-<port>-<nodePort>: [%s]", poolName)
	}
	protocol := pool[0]
	portString := pool[1]
	nodePortString := pool[2]
	if protocol != "tcp" && protocol != "udp" {
		return "", -1, -1, fmt.Errorf("invalid protocol in pool name [%s], only tcp and udp supported", poolName)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		return "", -1, -1, err
	}
	nodePort, err := strconv.Atoi(nodePortString)
	if err != nil {
		return "", -1, -1, err
	}
	return protocol, port, nodePort, nil
}

// genLoadBalancerPoolName - generate the VPC pool name for a specific Kubernetes service port
func genLoadBalancerPoolName(kubePort v1.ServicePort) string {
	return fmt.Sprintf("%s-%d-%d", strings.ToLower(string(kubePort.Protocol)), kubePort.Port, kubePort.NodePort)
}

// ServiceOptions - options from Kubernetes Load Balancer service and methods to access those fields
type ServiceOptions struct {
	annotations         map[string]string
	enabledFeatures     string
	healthCheckNodePort int
}

// newServiceOptions - return blank/empty service option
func newServiceOptions() *ServiceOptions {
	return &ServiceOptions{annotations: map[string]string{}}
}

// getServiceOptions - create service options from the Kubernetes service
func (c *CloudVpc) getServiceOptions(service *v1.Service) *ServiceOptions {
	if service == nil {
		return newServiceOptions()
	}
	return &ServiceOptions{
		annotations:         service.Annotations,
		enabledFeatures:     c.getServiceEnabledFeatures(service),
		healthCheckNodePort: c.getServiceHealthCheckNodePort(service),
	}
}

// getHealthCheckNodePort - retrieve the health check node port value
//
//	 0  = externalTrafficPolicy: Cluster
//	>0  = externalTrafficPolicy: Local
func (options *ServiceOptions) getHealthCheckNodePort() int {
	return options.healthCheckNodePort
}

// getServiceSubnets - retrieve the vpc-subnets annotation
func (options *ServiceOptions) getServiceSubnets() string {
	return strings.ReplaceAll(options.annotations[serviceAnnotationSubnets], " ", "")
}

// getServiceZone - retrieve the zone annotation
func (options *ServiceOptions) getServiceZone() string {
	return strings.ReplaceAll(options.annotations[serviceAnnotationZone], " ", "")
}

// isProxyProtocol - return true if service has proxy-protocol enabled
func (options *ServiceOptions) isProxyProtocol() bool {
	return isVpcOptionEnabled(options.enabledFeatures, LoadBalancerOptionProxyProtocol)
}

// isPublic - return true if service is public LB
func (options *ServiceOptions) isPublic() bool {
	value := options.annotations[serviceAnnotationIPType]
	return value == "" || value == servicePublicLB
}

// isVpcOptionEnabled - check to see if item string is in the comma separated list
func isVpcOptionEnabled(list, item string) bool {
	if list == "" {
		return false
	}
	for _, option := range strings.Split(list, ",") {
		if option == item {
			return true
		}
	}
	return false
}

// SafePointerBool - safely de-ref pointer to an bool
func SafePointerBool(ptr *bool) bool {
	if ptr == nil {
		return false
	}
	return *ptr
}

// SafePointerDate - safely de-ref pointer to an date object
func SafePointerDate(ptr *strfmt.DateTime) string {
	if ptr == nil {
		return "nil"
	}
	return fmt.Sprintf("%v", *ptr)
}

// SafePointerInt64 - safely de-ref pointer to an int64
func SafePointerInt64(ptr *int64) int64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

// SafePointerString - safely de-ref pointer to an string
func SafePointerString(ptr *string) string {
	if ptr == nil {
		return "nil"
	}
	return *ptr
}

// Constants associated with the LoadBalancer*.OperatingStatus property.
// The operating status of this load balancer.
const (
	LoadBalancerOperatingStatusOffline = "offline"
	LoadBalancerOperatingStatusOnline  = "online"
)

// Constants associated with the LoadBalancer*.ProvisioningStatus property.
// The provisioning status of this load balancer.
const (
	LoadBalancerProvisioningStatusActive             = "active"
	LoadBalancerProvisioningStatusCreatePending      = "create_pending"
	LoadBalancerProvisioningStatusDeletePending      = "delete_pending"
	LoadBalancerProvisioningStatusFailed             = "failed"
	LoadBalancerProvisioningStatusMaintenancePending = "maintenance_pending"
	LoadBalancerProvisioningStatusUpdatePending      = "update_pending"
)

// Constants associated with the LoadBalancer*.Protocol property.
// The listener protocol.
const (
	LoadBalancerProtocolHTTP  = "http"
	LoadBalancerProtocolHTTPS = "https"
	LoadBalancerProtocolTCP   = "tcp"
)

// Constants associated with the LoadBalancerPool.Algorithm property.
// The load balancing algorithm.
const (
	LoadBalancerAlgorithmLeastConnections   = "least_connections"
	LoadBalancerAlgorithmRoundRobin         = "round_robin"
	LoadBalancerAlgorithmWeightedRoundRobin = "weighted_round_robin"
)

// Constants associated with the LoadBalancerPool.ProxyProtocol property.
// The PROXY protocol setting for this pool:
// - `v1`: Enabled with version 1 (human-readable header format)
// - `v2`: Enabled with version 2 (binary header format)
// - `disabled`: Disabled
//
// Supported by load balancers in the `application` family (otherwise always `disabled`).
const (
	LoadBalancerProxyProtocolDisabled = "disabled"
	LoadBalancerProxyProtocolV1       = "v1"
	LoadBalancerProxyProtocolV2       = "v2"
)

// Constants associated with the LoadBalancerPoolSessionPersistence.Type property.
// The session persistence type.
const (
	LoadBalancerSessionPersistenceSourceIP = "source_ip"
)

// Constants that can control the behavior of the VPC LoadBalancer
const (
	LoadBalancerOptionProxyProtocol = "proxy-protocol"
)

// VpcObjectReference ...
type VpcObjectReference struct {
	// The unique identifier
	ID string

	// The unique user-defined name
	Name string
}

// VpcLoadBalancer ...
type VpcLoadBalancer struct {
	// Saved copy of the actual SDK object
	SdkObject interface{}

	// The date and time that this load balancer was created.
	// CreatedAt *strfmt.DateTime `json:"created_at" validate:"required"`
	CreatedAt string

	// The load balancer's CRN.
	// CRN *string `json:"crn" validate:"required"`

	// Fully qualified domain name assigned to this load balancer.
	// Hostname *string `json:"hostname" validate:"required"`
	Hostname string

	// The load balancer's canonical URL.
	// Href *string `json:"href" validate:"required"`

	// The unique identifier for this load balancer.
	// ID *string `json:"id" validate:"required"`
	ID string

	// The type of this load balancer, public or private.
	// IsPublic *bool `json:"is_public" validate:"required"`
	IsPublic bool

	// The listeners of this load balancer.
	// Listeners []LoadBalancerListenerReference `json:"listeners" validate:"required"`
	ListenerIDs []string

	// The logging configuration for this load balancer.
	// Logging *LoadBalancerLogging `json:"logging" validate:"required"`

	// The unique user-defined name for this load balancer.
	// Name *string `json:"name" validate:"required"`
	Name string

	// The operating status of this load balancer.
	// OperatingStatus *string `json:"operating_status" validate:"required"`
	OperatingStatus string

	// The pools of this load balancer.
	// Pools []LoadBalancerPoolReference `json:"pools" validate:"required"`
	Pools []VpcObjectReference

	// The private IP addresses assigned to this load balancer.
	// PrivateIps []IP `json:"private_ips" validate:"required"`
	PrivateIps []string

	// The profile to use for this load balancer.
	// Profile *LoadBalancerProfileReference `json:"profile" validate:"required"`
	ProfileFamily string

	// The provisioning status of this load balancer.
	// ProvisioningStatus *string `json:"provisioning_status" validate:"required"`
	ProvisioningStatus string

	// The public IP addresses assigned to this load balancer. These are applicable only for public load balancers.
	// PublicIps []IP `json:"public_ips" validate:"required"`
	PublicIps []string

	// The resource group for this load balancer.
	// ResourceGroup *ResourceGroupReference `json:"resource_group" validate:"required"`
	ResourceGroup VpcObjectReference

	// Collection of service IP addresses for this load balancer.
	// ServiceIps []LoadBalancerServiceIPs `json:"service_ips,omitempty"`
	// Service IPs will be returned in the PrivateIps field

	// The subnets this load balancer is part of.
	// Subnets []SubnetReference `json:"subnets" validate:"required"`
	Subnets []VpcObjectReference

	// The VPC this load balancer belongs to. For load balancers that use subnets, this
	// is the VPC the subnets belong to.
	// Vpc *VPCReferenceNoName `json:"vpc,omitempty"`
	VpcID string
}

// GetStatus - returns the operational/provisioning status of the VPC load balancer as a string
func (lb *VpcLoadBalancer) GetStatus() string {
	return fmt.Sprintf("%s/%s", lb.OperatingStatus, lb.ProvisioningStatus)
}

// getSubnetIDs - returns list of subnet IDs associated with the VPC load balancer
func (lb *VpcLoadBalancer) getSubnetIDs() []string {
	subnetList := []string{}
	for _, subnet := range lb.Subnets {
		subnetList = append(subnetList, subnet.ID)
	}
	return subnetList
}

// GetSuccessString - returns a string indicating success of the LB creation
func (lb *VpcLoadBalancer) GetSuccessString() string {
	if lb.Hostname != "" {
		return lb.Hostname
	}
	return strings.Join(lb.PrivateIps, ",")
}

// GetSummary - returns a string containing key information about the VPC load balancer
func (lb *VpcLoadBalancer) GetSummary() string {
	poolList := []string{}
	for _, pool := range lb.Pools {
		poolList = append(poolList, pool.Name)
	}
	sort.Strings(poolList)
	poolNames := strings.Join(poolList, ",")
	privateIPs := strings.Join(lb.PrivateIps, ",")
	publicIPs := strings.Join(lb.PublicIps, ",")
	result := fmt.Sprintf("Name:%s ID:%s Status:%s", lb.Name, lb.ID, lb.GetStatus())
	// Don't return fields that have not been set
	if lb.Hostname != "" {
		result += fmt.Sprintf(" Hostname:%s", lb.Hostname)
	}
	if poolNames != "" {
		result += fmt.Sprintf(" Pools:%s", poolNames)
	}
	if privateIPs != "" {
		result += fmt.Sprintf(" Private:%s", privateIPs)
	}
	if publicIPs != "" {
		result += fmt.Sprintf(" Public:%s", publicIPs)
	}
	return result
}

// getVpcID - return the VPC ID associated with the VPC load balancer
func (lb *VpcLoadBalancer) getVpcID(vpcSubnets []*VpcSubnet) string {
	for _, lbSubnet := range lb.Subnets {
		for _, vpcSubnet := range vpcSubnets {
			if lbSubnet.ID == vpcSubnet.ID {
				return vpcSubnet.Vpc.ID
			}
		}
	}
	return ""
}

// getZones - return the Zone(s) associated with the VPC load balancer
func (lb *VpcLoadBalancer) getZones(vpcSubnets []*VpcSubnet) []string {
	zoneMap := map[string]bool{}
	for _, lbSubnet := range lb.Subnets {
		for _, vpcSubnet := range vpcSubnets {
			if lbSubnet.ID == vpcSubnet.ID {
				zoneMap[vpcSubnet.Zone] = true
			}
		}
	}
	zoneList := []string{}
	for zone := range zoneMap {
		zoneList = append(zoneList, zone)
	}
	sort.Strings(zoneList)
	return zoneList
}

// IsNLB - returns true of the load balancer is a Network Load Balancer
func (lb *VpcLoadBalancer) IsNLB() bool {
	return strings.EqualFold(lb.ProfileFamily, "network")
}

// IsReady - returns a flag indicating if the load balancer will allow additional operations to be done
func (lb *VpcLoadBalancer) IsReady() bool {
	return lb.OperatingStatus == LoadBalancerOperatingStatusOnline &&
		lb.ProvisioningStatus == LoadBalancerProvisioningStatusActive
}

// VpcLoadBalancerListener ...
type VpcLoadBalancerListener struct {
	// The certificate instance used for SSL termination. It is applicable only to `https`
	// protocol.
	// CertificateInstance *CertificateInstanceReference `json:"certificate_instance,omitempty"`

	// The connection limit of the listener.
	// ConnectionLimit *int64 `json:"connection_limit,omitempty"`
	ConnectionLimit int64

	// The date and time that this listener was created.
	// CreatedAt *strfmt.DateTime `json:"created_at" validate:"required"`

	// The default pool associated with the listener.
	// DefaultPool *LoadBalancerPoolReference `json:"default_pool,omitempty"`
	DefaultPool VpcObjectReference

	// The listener's canonical URL.
	// Href *string `json:"href" validate:"required"`

	// The unique identifier for this load balancer listener.
	// ID *string `json:"id" validate:"required"`
	ID string

	// The list of policies of this listener.
	// Policies []LoadBalancerListenerPolicyReference `json:"policies,omitempty"`

	// The listener port number.
	// Port *int64 `json:"port" validate:"required"`
	Port int64

	// The listener protocol.
	// Protocol *string `json:"protocol" validate:"required"`
	Protocol string

	// The provisioning status of this listener.
	// ProvisioningStatus *string `json:"provisioning_status" validate:"required"`
	ProvisioningStatus string
}

// VpcLoadBalancerPool ...
type VpcLoadBalancerPool struct {
	// The load balancing algorithm.
	// Algorithm *string `json:"algorithm" validate:"required"`
	Algorithm string

	// The date and time that this pool was created.
	// CreatedAt *strfmt.DateTime `json:"created_at" validate:"required"`

	// The health monitor of this pool.
	// HealthMonitor *LoadBalancerPoolHealthMonitor `json:"health_monitor" validate:"required"`
	HealthMonitor VpcLoadBalancerPoolHealthMonitor

	// The pool's canonical URL.
	// Href *string `json:"href" validate:"required"`

	// The unique identifier for this load balancer pool.
	// ID *string `json:"id" validate:"required"`
	ID string

	// The backend server members of the pool.
	// Members []LoadBalancerPoolMembersItem `json:"members,omitempty"`
	Members []*VpcLoadBalancerPoolMember

	// The user-defined name for this load balancer pool.
	// Name *string `json:"name" validate:"required"`
	Name string

	// The protocol used for this load balancer pool.
	//
	// The enumerated values for this property are expected to expand in the future. When processing this property, check
	// for and log unknown values. Optionally halt processing and surface the error, or bypass the pool on which the
	// unexpected property value was encountered.
	// Protocol *string `json:"protocol" validate:"required"`
	Protocol string

	// The provisioning status of this pool.
	// ProvisioningStatus *string `json:"provisioning_status" validate:"required"`
	ProvisioningStatus string

	// The PROXY protocol setting for this pool:
	// - `v1`: Enabled with version 1 (human-readable header format)
	// - `v2`: Enabled with version 2 (binary header format)
	// - `disabled`: Disabled
	//
	// Supported by load balancers in the `application` family (otherwise always `disabled`).
	// ProxyProtocol *string `json:"proxy_protocol" validate:"required"`
	ProxyProtocol string

	// The session persistence of this pool.
	// SessionPersistence *LoadBalancerPoolSessionPersistenceTemplate `json:"session_persistence,omitempty"`
	SessionPersistence string
}

// VpcLoadBalancerPoolHealthMonitor ...
type VpcLoadBalancerPoolHealthMonitor struct {
	// The health check interval in seconds. Interval must be greater than timeout value.
	// Delay *int64 `json:"delay" validate:"required"`
	Delay int64

	// The health check max retries.
	// MaxRetries *int64 `json:"max_retries" validate:"required"`
	MaxRetries int64

	// The health check port number. If specified, this overrides the ports specified in the server member resources.
	// Port *int64 `json:"port,omitempty"`
	Port int64

	// The health check timeout in seconds.
	// Timeout *int64 `json:"timeout" validate:"required"`
	Timeout int64

	// The protocol type of this load balancer pool health monitor.
	//
	// The enumerated values for this property are expected to expand in the future. When processing this property, check
	// for and log unknown values. Optionally halt processing and surface the error, or bypass the health monitor on which
	// the unexpected property value was encountered.
	// Type *string `json:"type" validate:"required"`
	Type string

	// The health check url. This is applicable only to `http` type of health monitor.
	// URLPath *string `json:"url_path,omitempty"`
	URLPath string
}

// VpcLoadBalancerPoolMember ...
type VpcLoadBalancerPoolMember struct {
	// The date and time that this member was created.
	// CreatedAt *strfmt.DateTime `json:"created_at" validate:"required"`

	// Health of the server member in the pool.
	// Health *string `json:"health" validate:"required"`
	Health string

	// The member's canonical URL.
	// Href *string `json:"href" validate:"required"`

	// The unique identifier for this load balancer pool member.
	// ID *string `json:"id" validate:"required"`
	ID string

	// The port number of the application running in the server member.
	// Port *int64 `json:"port" validate:"required"`
	Port int64

	// The provisioning status of this member.
	// ProvisioningStatus *string `json:"provisioning_status" validate:"required"`
	ProvisioningStatus string

	// The pool member target type.
	// TargetIPAddress *LoadBalancerMemberTarget.Address `json:"target" validate:"required"`
	TargetIPAddress string
	// TargetAddress *LoadBalancerMemberTarget.ID `json:"target" validate:"required"`
	TargetInstanceID string

	// Weight of the server member. This takes effect only when the load balancing algorithm of its belonging pool is
	// `weighted_round_robin`.
	// Weight *int64 `json:"weight,omitempty"`
	Weight int64
}

// VpcSubnet ...
type VpcSubnet struct {
	// Saved copy of the actual SDK object
	SdkObject interface{}

	// The number of IPv4 addresses in this subnet that are not in-use, and have not been reserved by the user or the
	// provider.
	// AvailableIpv4AddressCount *int64 `json:"available_ipv4_address_count" validate:"required"`
	AvailableIpv4AddressCount int64

	// The date and time that the subnet was created.
	// CreatedAt *strfmt.DateTime `json:"created_at" validate:"required"`
	CreatedAt string

	// The CRN for this subnet.
	// CRN *string `json:"crn" validate:"required"`

	// The URL for this subnet.
	// Href *string `json:"href" validate:"required"`

	// The unique identifier for this subnet.
	// ID *string `json:"id" validate:"required"`
	ID string

	// The IP version(s) supported by this subnet.
	// IPVersion *string `json:"ip_version" validate:"required"`
	IPVersion string

	// The IPv4 range of the subnet, expressed in CIDR format.
	// Ipv4CIDRBlock *string `json:"ipv4_cidr_block,omitempty"`
	Ipv4CidrBlock string

	// The user-defined name for this subnet.
	// Name *string `json:"name" validate:"required"`
	Name string

	// The network ACL for this subnet.
	// NetworkACL *NetworkACLReference `json:"network_acl" validate:"required"`
	NetworkACL VpcObjectReference

	// The public gateway to handle internet bound traffic for this subnet.
	// PublicGateway *PublicGatewayReference `json:"public_gateway,omitempty"`
	PublicGateway VpcObjectReference

	// The resource group for this subnet.
	// ResourceGroup *ResourceGroupReference `json:"resource_group" validate:"required"`
	ResourceGroup VpcObjectReference

	// The status of the subnet.
	// Status *string `json:"status" validate:"required"`
	Status string

	// The total number of IPv4 addresses in this subnet.
	//
	// Note: This is calculated as 2<sup>(32 − prefix length)</sup>. For example, the prefix length `/24` gives:<br>
	// 2<sup>(32 − 24)</sup> = 2<sup>8</sup> = 256 addresses.
	// TotalIpv4AddressCount *int64 `json:"total_ipv4_address_count" validate:"required"`
	TotalIpv4AddressCount int64

	// The VPC this subnet is a part of.
	// VPC *VPCReference `json:"vpc" validate:"required"`
	Vpc VpcObjectReference

	// The zone this subnet resides in.
	// Zone *ZoneReference `json:"zone" validate:"required"`
	Zone string
}
