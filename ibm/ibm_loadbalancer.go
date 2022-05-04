/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2022 All Rights Reserved.
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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	cloudprovider "k8s.io/cloud-provider"
	servicehelper "k8s.io/cloud-provider/service/helpers"
)

const (
	k8sNamespace                        = "kube-system"
	lbDeploymentNamespace               = "ibm-system"
	lbIPLabel                           = "ibm-cloud-provider-ip"
	lbNameLabel                         = "ibm-cloud-provider-lb-name"
	lbApplicationLabel                  = "ibm-cloud-provider-lb-app"
	lbDeploymentServiceAccountName      = "ibm-cloud-provider-lb"
	lbDeploymentNamePrefix              = lbIPLabel + "-"
	lbPublicVlanLabel                   = "publicVLAN"
	lbPrivateVlanLabel                  = "privateVLAN"
	lbDedicatedLabel                    = "dedicated"
	lbEdgeNodeValue                     = "edge"
	lbGatewayNodeValue                  = "gateway"
	lbNetAdminCapability                = "NET_ADMIN"
	lbNetRawCapability                  = "NET_RAW"
	lbTolerationKey                     = "dedicated"
	lbTolerationValueEdge               = "edge"
	lbTolerationValueGateway            = "gateway"
	lbNoIPsBaseMessage                  = "No cloud provider IPs are available to fulfill the load balancer service request."
	lbNoIPsMessage                      = lbNoIPsBaseMessage + " Add a portable subnet to the cluster and try again."
	lbPortableSubnetMessage             = lbNoIPsBaseMessage + " Resolve the following issues then add a portable subnet to the cluster: "
	lbLiteClusterMessage                = "Clusters with one node must use services of type NodePort."
	lbDocIKSNetworkURL                  = "https://ibm.biz/lb-debug"
	lbDocROKSNetworkURL                 = "https://ibm.biz/oc-lb-debug"
	lbDocSupportedSchedulers            = "https://ibm.biz/lbv2-scheduling"
	lbDocReferenceMessage               = "See " + lbDocIKSNetworkURL + "(IKS) or " + lbDocROKSNetworkURL + " (Openshift) for more details."
	lbDocTroubleshootMessage            = "For more information read the troubleshooting cluster networking doc: " + lbDocIKSNetworkURL + " (IKS) or " + lbDocROKSNetworkURL + " (Openshift)"
	lbDocUnsupportedScheduler           = "For more information read the supported scheduler doc: " + lbDocSupportedSchedulers
	lbUnsupportedScheduler              = "You have specified an unsupported scheduler: %s. Supported schedulers are: %s. " + lbDocUnsupportedScheduler
	lbDefaultNoIPPortableSubnetErrorMsg = lbNoIPsMessage + " " + lbDocReferenceMessage
	lbFeatureIPVS                       = "ipvs"
	calicoEtcdSecrets                   = "calico-etcd-secrets" // #nosec G101 Name of Kubernetes secret resource which contains the secrets, not an actual secret
	lbPriorityClassName                 = "ibm-app-cluster-critical"
	clusterInfoCM                       = "cluster-info"
	lbIPVSInvlaidExternalTrafficPolicy  = "Cluster networking is not supported for IPVS-based load balancers. Set 'externalTrafficPolicy' to 'Local', and try again."
	lbVpcClassicProvider                = "gc"
	lbVpcNextGenProvider                = "g2"
)

// Run Keepalived Deployments as non-root user with UID:GID 2000:2000
var (
	lbNonRootUser  = int64(2000)
	lbNonRootGroup = int64(2000)
	lbRootUser     = int64(0)
	lbRootGroup    = int64(0)
)

// ServiceAnnotationIngressControllerPublic is the annotation used on the service
// to indicate it is for the cluster's public IBM ingress controller.
const ServiceAnnotationIngressControllerPublic = "service.kubernetes.io/ibm-ingress-controller-public"

// ServiceAnnotationIngressControllerPrivate is the annotation used on the service
// to indicate it is for the cluster's private IBM ingress controller.
const ServiceAnnotationIngressControllerPrivate = "service.kubernetes.io/ibm-ingress-controller-private"

// ServiceAnnotationLoadBalancerCloudProviderIPType is the annotation used on the
// service to indicate the requested cloud provider IP type (public or private).
// The default is public when there is at least one node on the public VLAN,
// otherwise the default is private.
const ServiceAnnotationLoadBalancerCloudProviderIPType = "service.kubernetes.io/ibm-load-balancer-cloud-provider-ip-type"

// ServiceAnnotationLoadBalancerCloudProviderZone is the annotation used on the service
// to indicate the Availability Zone for which to pick a IP from. It can be combined
// with anyone of the other annotations. If the annotation is not provided, then an IP will
// be chosen from any Zone.
const ServiceAnnotationLoadBalancerCloudProviderZone = "service.kubernetes.io/ibm-load-balancer-cloud-provider-zone"

// ServiceAnnotationLoadBalancerCloudProviderEnableFeatures is the annotation used on the
// service to enable non-released features in the load balancer.  The annotation value can
// be a single feature or a list of multiple features, delimited by a comma.
const ServiceAnnotationLoadBalancerCloudProviderEnableFeatures = "service.kubernetes.io/ibm-load-balancer-cloud-provider-enable-features"

// ServiceAnnotationLoadBalancerCloudProviderIPVSSchedulingAlgorithm is the annotation used on the
// service to allow the customer to define a scheduling algorithm
const ServiceAnnotationLoadBalancerCloudProviderIPVSSchedulingAlgorithm = "service.kubernetes.io/ibm-load-balancer-cloud-provider-ipvs-scheduler"

// ServiceAnnotationLoadBalancerCloudProviderVlan is the annotation used on the service
// to indicate the Vlan for which to pick a IP from. It can be combined
// with anyone of the other annotations. If the annotation is not provided, then an IP will
// be chosen from any Vlan.
const ServiceAnnotationLoadBalancerCloudProviderVlan = "service.kubernetes.io/ibm-load-balancer-cloud-provider-vlan"

// CloudProviderIPType describes the type of the cloud provider IP
type CloudProviderIPType string

const (
	// PublicIP cloud provider IP
	PublicIP CloudProviderIPType = "public"
	// PrivateIP cloud provider IP
	PrivateIP CloudProviderIPType = "private"
)

// CloudProviderIPReservation describes the reservation of the cloud provider IP
type CloudProviderIPReservation string

const (
	// ReservedIP cloud provider IP
	ReservedIP CloudProviderIPReservation = "reserved"
	// UnreservedIP cloud provider IP
	UnreservedIP CloudProviderIPReservation = "unreserved"
)

var (
	supportedIPVSSchedulerTypes = []string{"rr", "sh"}

	lbDeploymentResourceRequests = map[v1.ResourceName]string{v1.ResourceName(v1.ResourceCPU): "5m", v1.ResourceName(v1.ResourceMemory): "10Mi"}
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

// getCloudProviderVlanIPsRequest returns the cloud provider VLAN IPs
// request information for the load balancer service.
func (c *Cloud) getCloudProviderVlanIPsRequest(service *v1.Service) (CloudProviderIPType, CloudProviderIPReservation, string, string, string, error) {
	// Default request is for unreserved cloud provider VLAN IPs.
	cloudProviderIPReservation := UnreservedIP

	// Default request is for public cloud provider VLAN IPs unless there
	// aren't any nodes on a public VLAN.
	cloudProviderIPType := PublicIP
	nodes, err := c.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: lbPublicVlanLabel})
	if nil != err {
		return "", "", "", "", "", fmt.Errorf("Failed to list nodes: %v", err)
	} else if 0 == len(nodes.Items) {
		cloudProviderIPType = PrivateIP
	}

	// Used for setting the Availability zone for which to select a public IP
	cloudProviderZone := ""
	cloudProviderVlan := ""

	// Override defaults based on the service annotations.
	if nil != service.Annotations {
		annotationCount := 0
		if _, ok := service.Annotations[ServiceAnnotationIngressControllerPublic]; ok {
			annotationCount++
			cloudProviderIPType = PublicIP
			cloudProviderIPReservation = ReservedIP
		}
		if _, ok := service.Annotations[ServiceAnnotationIngressControllerPrivate]; ok {
			annotationCount++
			cloudProviderIPType = PrivateIP
			cloudProviderIPReservation = ReservedIP
		}
		if ipType, ok := service.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPType]; ok {
			annotationCount++
			switch ipType {
			case fmt.Sprintf("%v", PublicIP):
				cloudProviderIPType = PublicIP
			case fmt.Sprintf("%v", PrivateIP):
				cloudProviderIPType = PrivateIP
			default:
				return "", "", "", "", "", fmt.Errorf("Value for service annotation %v must be '%v' or '%v'", ServiceAnnotationLoadBalancerCloudProviderIPType, PublicIP, PrivateIP)
			}
			cloudProviderIPReservation = UnreservedIP
		}
		if annotationCount > 1 {
			return "", "", "", "", "", fmt.Errorf("Conflicting cloud provider IP service annotations were specified")
		}
		if zone, ok := service.Annotations[ServiceAnnotationLoadBalancerCloudProviderZone]; ok {
			cloudProviderZone = zone
		}
		if vlan, ok := service.Annotations[ServiceAnnotationLoadBalancerCloudProviderVlan]; ok {
			cloudProviderVlan = vlan
		}
	}
	lbVlanLabel := lbPublicVlanLabel
	if PrivateIP == cloudProviderIPType {
		lbVlanLabel = lbPrivateVlanLabel
	}
	return cloudProviderIPType, cloudProviderIPReservation, lbVlanLabel, cloudProviderZone, cloudProviderVlan, nil
}

// getCloudProviderIPLabelValue returns a modified version of the cloud provider
// IP which can be used as a label value.
func getCloudProviderIPLabelValue(cloudProviderIP string) string {
	return strings.ReplaceAll(cloudProviderIP, ".", "-")
}

// getLoadBalancerDeploymentName returns the load balancer deployment name
// for a given cloud provider IP.
func getLoadBalancerDeploymentName(cloudProviderIP string) string {
	return lbDeploymentNamePrefix + getCloudProviderIPLabelValue(cloudProviderIP)
}

// getLoadBalancerStatus returns the load balancer status for a given cloud
// provider IP.
func getLoadBalancerStatus(cloudProviderIP string) *v1.LoadBalancerStatus {
	lbStatus := &v1.LoadBalancerStatus{}
	lbStatus.Ingress = []v1.LoadBalancerIngress{{IP: cloudProviderIP}}
	return lbStatus
}

// getLabelsCloudProviderIP returns the cloud provider IP for the given labels.
func getLabelsCloudProviderIP(labels map[string]string) string {
	return strings.ReplaceAll(labels[lbIPLabel], "-", ".")
}

// getSelectorCloudProviderIP returns the cloud provider IP for a given
// label selector.
func getSelectorCloudProviderIP(selector *metav1.LabelSelector) string {
	if nil != selector {
		return getLabelsCloudProviderIP(selector.MatchLabels)
	}
	return ""
}

// getLoadBalancerLogName returns the load balancer name to use in log messages.
func getLoadBalancerLogName(lbName string, cloudProviderIP string) string {
	return "(name: " + lbName + ", IP: " + cloudProviderIP + ")"
}

// LoadBalancer returns a balancer interface. Also returns true if the interface is supported, false otherwise.
func (c *Cloud) LoadBalancer() (cloudprovider.LoadBalancer, bool) {
	// Ensure that the monitor task is started.
	c.StartTask(MonitorLoadBalancers, time.Minute*5)
	return c, true
}

// The cloudProviderReservedIP, cloudProviderSubnet, cloudProviderVlan and
// cloudProviderVlanIPConfig structs describe the layout of the cloud provider
// VLAN IP config map vlanipmap.json data.

type cloudProviderReservedIP struct {
	IP       string `json:"ip"`
	SubnetID string `json:"subnet_id"`
	VlanID   string `json:"vlan_id"`
	IsPublic bool   `json:"is_public"`
	Zone     string `json:"zone"`
}

type cloudProviderSubnet struct {
	ID       string   `json:"id"`
	IPs      []string `json:"ips"`
	IsPublic bool     `json:"is_public"`
}

type subnetConfigErrorField struct {
	IsPublic        bool   `json:"is_public"`
	IsBYOIP         bool   `json:"is_byoip"`
	ErrorReasonCode string `json:"error_reason_code"`
	ErrorMessage    string `json:"error_message"`
	Status          string `json:"status"`
}

type cloudProviderVlan struct {
	ID      string                `json:"id"`
	Subnets []cloudProviderSubnet `json:"subnets"`
	Zone    string                `json:"zone"`
}

type vlanConfigErrorField struct {
	ID      string                   `json:"id"`
	Subnets []subnetConfigErrorField `json:"subnets"`
	Zone    string                   `json:"zone"`
	Region  string                   `json:"region"`
}

type cloudProviderVlanIPConfig struct {
	ReservedIPs []cloudProviderReservedIP `json:"reserved_ips"`
	Vlans       []cloudProviderVlan       `json:"vlans"`
	VlanErrors  []vlanConfigErrorField    `json:"vlan_errors"`
}

// getCloudProviderIPConfigMap returns the cloud provider VLAN IP config.
func (c *Cloud) getCloudProviderVlanIPConfig() (*cloudProviderVlanIPConfig, error) {
	// Get the raw cloud provider VLAN IP config map.
	// TODO(rtheis): Temporarily support both the load balancer deployment
	// namespace and the kubernetes namespace for finding the config map.
	// When ready, switch to the kubernetes namespace only.
	cmName := c.Config.LBDeployment.VlanIPConfigMap
	cmNamespace := k8sNamespace
	cm, err := c.KubeClient.CoreV1().ConfigMaps(cmNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	if nil != err && errors.IsNotFound(err) {
		cmNamespace = lbDeploymentNamespace
		cm, err = c.KubeClient.CoreV1().ConfigMaps(cmNamespace).Get(context.TODO(), cmName, metav1.GetOptions{})
	}
	if nil != err {
		// Handle special error case when the config map isn't found.
		if errors.IsNotFound(err) {
			nodes, err := c.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
			if nil == err && 1 == len(nodes.Items) {
				return nil, fmt.Errorf("%v %v", lbLiteClusterMessage, lbDocReferenceMessage)
			}
			return nil, fmt.Errorf("%v %v", lbNoIPsMessage, lbDocReferenceMessage)
		}
		return nil, fmt.Errorf("Failed to get config map %v in namespace %v: %v", cmName, cmNamespace, err)
	}

	// Parse the config map vlanipmap.json data.
	var config cloudProviderVlanIPConfig
	err = json.Unmarshal([]byte(cm.Data["vlanipmap.json"]), &config)
	if nil != err {
		return nil, fmt.Errorf("Failed to parse config map %v in namespace %v: %v", cmName, cmNamespace, err)
	}
	return &config, nil
}

// getLoadBalancerIPTypeLabel returns the cloud provider VLAN IP type string.
func (c *Cloud) getLoadBalancerIPTypeLabel(lbDeployment *apps.Deployment) string {
	deployAffinity := lbDeployment.Spec.Template.Spec.Affinity
	var nodeSelector []v1.NodeSelectorRequirement

	// If node affinity is defined on the deployment check for VLAN Affinity rule
	if nil != deployAffinity &&
		nil != deployAffinity.NodeAffinity &&
		nil != deployAffinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution &&
		0 != len(deployAffinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) {

		nodeSelector = deployAffinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
		for _, label := range nodeSelector {
			if lbPublicVlanLabel == label.Key || lbPrivateVlanLabel == label.Key {
				return label.Key
			}
		}
	}
	// Default to the public vlan label
	return lbPublicVlanLabel
}

// populateAvailableCloudProviderVlanIPConfig populates the available cloud provider VLAN IP config.
func (c *Cloud) populateAvailableCloudProviderVlanIPConfig(
	availableCloudProviderVlanIDs map[string][]string,
	availableCloudProviderIPs map[string]string,
	availableCloudProviderVlanErrors map[string][]subnetConfigErrorField,
	cloudProviderIPType CloudProviderIPType,
	cloudProviderIPReservation CloudProviderIPReservation,
	cloudProviderZone string,
	cloudProviderVlan string) error {

	// Get the cloud provider VLAN IP config.
	config, err := c.getCloudProviderVlanIPConfig()
	if nil != err {
		return err
	}

	// Convert cloud provider IP type to boolean
	usePublic := true
	if PrivateIP == cloudProviderIPType {
		usePublic = false
	}

	// Loop through the vlan errors in the configmap
	for _, vlanError := range config.VlanErrors {
		if vlanError.Zone == cloudProviderZone || cloudProviderZone == "" {
			if vlanError.ID == cloudProviderVlan || cloudProviderVlan == "" {
				for _, subnet := range vlanError.Subnets {
					// Only append the errors that are for the same type of LB that is being created (public or private)
					if (usePublic && subnet.IsPublic) || (!usePublic && !subnet.IsPublic) {
						availableCloudProviderVlanErrors[vlanError.ID] = append(availableCloudProviderVlanErrors[vlanError.ID], subnet)
					}
				}
			}
		}
	}

	// Determine the available cloud provider VLANs and IPs from the config
	// based on the request.
	if UnreservedIP == cloudProviderIPReservation {
		for _, vlan := range config.Vlans {
			if vlan.Zone == cloudProviderZone || cloudProviderZone == "" {
				if vlan.ID == cloudProviderVlan || cloudProviderVlan == "" {
					for _, subnet := range vlan.Subnets {
						if (usePublic && subnet.IsPublic) || (!usePublic && !subnet.IsPublic) {
							for _, ip := range subnet.IPs {
								IP := net.ParseIP(ip)
								if nil != IP && nil != IP.To4() {
									availableCloudProviderIPs[ip] = vlan.ID
								}
							}
							availableCloudProviderVlanIDs[vlan.ID] = append(availableCloudProviderVlanIDs[vlan.ID], subnet.IPs...)
						}
					}
				}
			}
		}
	} else { // ReservedIP == cloudProviderIPReservation
		for _, reservedIP := range config.ReservedIPs {
			if reservedIP.Zone == cloudProviderZone || cloudProviderZone == "" {
				if reservedIP.VlanID == cloudProviderVlan || cloudProviderVlan == "" {
					if (usePublic && reservedIP.IsPublic) || (!usePublic && !reservedIP.IsPublic) {
						IP := net.ParseIP(reservedIP.IP)
						if nil != IP && nil != IP.To4() {
							availableCloudProviderIPs[reservedIP.IP] = reservedIP.VlanID
						}
						availableCloudProviderVlanIDs[reservedIP.VlanID] = append(availableCloudProviderVlanIDs[reservedIP.VlanID], reservedIP.IP)
					}
				}
			}
		}
	}
	return nil
}

// getLoadBalancerDeployment returns the load balancer deployment for a given
// load balancer name.
func (c *Cloud) getLoadBalancerDeployment(lbName string) (*apps.Deployment, error) {
	matchLabel := lbNameLabel + "=" + lbName
	listOptions := metav1.ListOptions{LabelSelector: matchLabel}
	baseMessage := fmt.Sprintf("load balancer deployment with label selector %v in namespace %v", matchLabel, lbDeploymentNamespace)
	lbDeployments, err := c.KubeClient.AppsV1().Deployments(lbDeploymentNamespace).List(context.TODO(), listOptions)
	switch {
	case nil != err:
		return nil, fmt.Errorf("Failed to get %v: %v", baseMessage, err)
	case len(lbDeployments.Items) > 1:
		return nil, fmt.Errorf("Found multiple %v", baseMessage)
	case len(lbDeployments.Items) == 1:
		klog.V(4).Infof("Found %v", baseMessage)
		return &lbDeployments.Items[0], nil
	}

	// The load balancer deployment doesn't exist. Ensure there aren't any
	// orphaned resources.
	lbReplicaSets, err := c.KubeClient.AppsV1().ReplicaSets(lbDeploymentNamespace).List(context.TODO(), listOptions)
	if nil != err {
		return nil, fmt.Errorf("Failed to get replicasets for %v: %v", baseMessage, err)
	}
	lbPods, err := c.KubeClient.CoreV1().Pods(lbDeploymentNamespace).List(context.TODO(), listOptions)
	if nil != err {
		return nil, fmt.Errorf("Failed to get pods for %v: %v", baseMessage, err)
	}
	if 0 != len(lbReplicaSets.Items) || 0 != len(lbPods.Items) {
		return nil, fmt.Errorf("Found orphaned resources for %v: number of orphaned replicasets %v, pods %v", baseMessage, len(lbReplicaSets.Items), len(lbPods.Items))
	}
	klog.V(4).Infof("No %v", baseMessage)
	return nil, nil
}

// updateLoadBalancerDeployment ensures that the load balancer deployment is
// updated if necessary.
func (c *Cloud) updateLoadBalancerDeployment(lbLogName string, lbDeployment *apps.Deployment, service *v1.Service, nodes []*v1.Node) error {
	var err error
	var updatesRequired []string

	// Ensure minReadySeconds is 90
	if lbDeployment.Spec.MinReadySeconds != 90 {
		lbDeployment.Spec.MinReadySeconds = 90
		updatesRequired = append(updatesRequired, "SetMinReadySeconds")
	}

	if 1 == len(lbDeployment.Spec.Template.Spec.Containers) {
		updateImage := false
		lbDeploymentImageList := strings.Split(lbDeployment.Spec.Template.Spec.Containers[0].Image, ":")
		configImageList := strings.Split(c.Config.LBDeployment.Image, ":")
		// Update the load balancer deployment Container if a latest image is available.
		if len(lbDeploymentImageList) > 1 && len(configImageList) > 1 {
			lbDeploymentImageValue, _ := strconv.Atoi(lbDeploymentImageList[1])
			configImageValue, _ := strconv.Atoi(configImageList[1])
			if lbDeploymentImageValue < configImageValue {
				updateImage = true
			}
		} else if c.Config.LBDeployment.Image != lbDeployment.Spec.Template.Spec.Containers[0].Image {
			updateImage = true
		}
		if updateImage {
			klog.Infof("Updating LB deployment container image to %v", c.Config.LBDeployment.Image)
			updatesRequired = append(updatesRequired, "Image")
			// Always use the new image.
			lbDeployment.Spec.Template.Spec.Containers[0].Image = c.Config.LBDeployment.Image
		}
		// If necessary, update the security context from a privileged container to one with NET_ADMIN capability.
		// Updated images use NET_ADMIN capability. Also add NET_RAW, which might not be part of the
		// CRI default capabilities (cri-o 4.5). Ensure that updated containers run as non-root user.
		if nil == lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext {
			lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext = &v1.SecurityContext{}
		}
		if nil == lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser ||
			lbNonRootUser != *lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser {
			klog.Infof("LB Deployment is running as an unexpected user. Adding non-root context to %v", lbDeployment.Name)
			updatesRequired = append(updatesRequired, "SecurityContext-Incorrect-User")
			lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext.RunAsUser = &lbNonRootUser
			lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext.RunAsGroup = &lbNonRootGroup
		}
		if nil == lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities ||
			2 > len(lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities.Add) {
			updatesRequired = append(updatesRequired, "SecurityContext")
			isPrivileged := false
			lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext.Privileged = &isPrivileged
			lbDeployment.Spec.Template.Spec.Containers[0].SecurityContext.Capabilities = &v1.Capabilities{
				Add: []v1.Capability{lbNetAdminCapability, lbNetRawCapability},
			}
		}
	}

	// Ensure that existing Load Balancers are configured to run root initContainer to modify
	// the host's '/tmp/keepalived' directory ownership
	if 0 == len(lbDeployment.Spec.Template.Spec.InitContainers) {
		klog.Infof("Adding initContainer to %v", lbDeployment.Name)
		updatesRequired = append(updatesRequired, "Add-InitContainer")
		lbDeployment.Spec.Template.Spec.InitContainers = []v1.Container{
			{
				Name:            lbDeploymentNamePrefix + "keepalived-init",
				Image:           c.Config.LBDeployment.Image,
				Command:         []string{"/usr/local/bin/hostDirPerms"},
				ImagePullPolicy: v1.PullIfNotPresent,
				VolumeMounts: []v1.VolumeMount{
					{
						Name:      c.Config.LBDeployment.Application + "-status",
						MountPath: "/status",
					},
				},
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceName(v1.ResourceCPU):    resource.MustParse(lbDeploymentResourceRequests[v1.ResourceName(v1.ResourceCPU)]),
						v1.ResourceName(v1.ResourceMemory): resource.MustParse(lbDeploymentResourceRequests[v1.ResourceName(v1.ResourceMemory)]),
					},
				},
				SecurityContext: &v1.SecurityContext{
					RunAsUser:  &lbRootUser,
					RunAsGroup: &lbRootGroup,
				},
			},
		}
	} else {
		updateInitImage := false
		lbDeploymentInitImageList := strings.Split(lbDeployment.Spec.Template.Spec.InitContainers[0].Image, ":")
		configImageList := strings.Split(c.Config.LBDeployment.Image, ":")
		// The initContainer exists. Update the load balancer deployment initContainer if a new image is available.
		if len(lbDeploymentInitImageList) > 1 && len(configImageList) > 1 {
			lbDeploymentInitImageValue, _ := strconv.Atoi(lbDeploymentInitImageList[1])
			configImageValue, _ := strconv.Atoi(configImageList[1])
			if lbDeploymentInitImageValue < configImageValue {
				updateInitImage = true
			}
		} else if c.Config.LBDeployment.Image != lbDeployment.Spec.Template.Spec.InitContainers[0].Image {
			updateInitImage = true
		}

		if updateInitImage {
			klog.Infof("Updating LB deployment Initcontainer image to %v", c.Config.LBDeployment.Image)
			updatesRequired = append(updatesRequired, "InitContainer-New-Image")
			// Always use the new image.
			lbDeployment.Spec.Template.Spec.InitContainers[0].Image = c.Config.LBDeployment.Image
		}
	}

	// Check if the edge and gateway tolerations need to be applied to lb deployments. This ensures a cluster with a lb
	// that has been deployed a very long time ago when the edge and gateway code was not in production, will be updated
	// with the gateway and edge tolerations when the service is updated.
	if nil != lbDeployment.Spec.Template.Spec.Tolerations {
		edgeTolerationExists := false
		gatewayTolerationExists := false
		for _, toleration := range lbDeployment.Spec.Template.Spec.Tolerations {
			if toleration.Key == lbTolerationKey {
				if toleration.Value == lbTolerationValueEdge {
					edgeTolerationExists = true
				} else if toleration.Value == lbTolerationValueGateway {
					gatewayTolerationExists = true
				}
			}
		}
		if !edgeTolerationExists {
			klog.Infof("LB Deployment has no Edge Toleration. Adding Edge toleration to %v", lbDeployment.Name)
			lbDeployment.Spec.Template.Spec.Tolerations = append(
				lbDeployment.Spec.Template.Spec.Tolerations,
				v1.Toleration{
					Key:   lbTolerationKey,
					Value: lbTolerationValueEdge,
				},
			)
			updatesRequired = append(updatesRequired, "AddEdgeNodeToleration")
		}
		if !gatewayTolerationExists {
			klog.Infof("LB Deployment has no Gateway Toleration. Adding Gateway toleration to %v", lbDeployment.Name)
			lbDeployment.Spec.Template.Spec.Tolerations = append(
				lbDeployment.Spec.Template.Spec.Tolerations,
				v1.Toleration{
					Key:   lbTolerationKey,
					Value: lbTolerationValueGateway,
				},
			)
			updatesRequired = append(updatesRequired, "AddGatewayNodeToleration")
		}
	} else {
		// Tolerations are nil
		klog.Infof("LB Deployment has no Tolerations. Adding Gateway and Edge tolerations to %v", lbDeployment.Name)
		lbDeployment.Spec.Template.Spec.Tolerations = []v1.Toleration{
			{
				Key:   lbTolerationKey,
				Value: lbTolerationValueEdge,
			},
			{
				Key:   lbTolerationKey,
				Value: lbTolerationValueGateway,
			},
		}
		updatesRequired = append(updatesRequired, "AddGatewayAndEdgeNodeTolerations")
	}

	// Check to see if edge or gateway affinity label needs to be applied or removed
	if nil != lbDeployment.Spec.Template.Spec.Affinity {
		nodeAffinity := lbDeployment.Spec.Template.Spec.Affinity.NodeAffinity
		var nodeSelector []v1.NodeSelectorRequirement
		vlanLabel := ""
		currentDeploymentSelectorValue := ""

		const (
			Gateway = lbGatewayNodeValue
			Edge    = lbEdgeNodeValue
			None    = ""
		)

		if nil != nodeAffinity && nil != nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution &&
			0 != len(nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) {
			nodeSelector = nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
			for _, label := range nodeSelector {
				switch label.Key {
				case lbPublicVlanLabel:
					vlanLabel = lbPublicVlanLabel + "=" + label.Values[0]
				case lbPrivateVlanLabel:
					vlanLabel = lbPrivateVlanLabel + "=" + label.Values[0]
				case lbDedicatedLabel:
					currentDeploymentSelectorValue = label.Values[0]
				}
			}
		}

		expectedSelectorValue := None

		if vlanLabel != "" {
			// check to see if any nodes have the dedicated:firwall label.  If so, we will produce deployment affinity to those nodes
			nodeListOptions := metav1.ListOptions{LabelSelector: vlanLabel + "," + lbDedicatedLabel + "=" + lbGatewayNodeValue}
			gatewayNodes, err := c.KubeClient.CoreV1().Nodes().List(context.TODO(), nodeListOptions)
			if nil != err {
				return fmt.Errorf("Failed to list nodes with gateway label for load balancer deployment %v: %v", lbLogName, err)
			} else if len(gatewayNodes.Items) > 0 {
				expectedSelectorValue = Gateway
			}

			if expectedSelectorValue == None {
				// no dedicated:gateway node labels found.  check to see if any nodes have the dedicated:edge label.
				// if so, we will produce deployment affinity to those nodes
				nodeListOptions = metav1.ListOptions{LabelSelector: vlanLabel + "," + lbDedicatedLabel + "=" + lbEdgeNodeValue}
				edgeNodes, err := c.KubeClient.CoreV1().Nodes().List(context.TODO(), nodeListOptions)
				if nil != err {
					return fmt.Errorf("Failed to list nodes with edge label for load balancer deployment %v: %v", lbLogName, err)
				} else if len(edgeNodes.Items) > 0 {
					expectedSelectorValue = Edge
				}
			}

			if expectedSelectorValue == None && currentDeploymentSelectorValue != None {
				// remove the dedicated selector from the deployment since no nodes are labeled as dedicated:gateway or dedicated:edge
				var newNodeSelector []v1.NodeSelectorRequirement
				for _, label := range nodeSelector {
					if lbDedicatedLabel != label.Key {
						newNodeSelector = append(newNodeSelector, label)
					}
				}
				nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = newNodeSelector
				updatesRequired = append(updatesRequired, "RemoveDedicatedNodeAffinity")
			} else if expectedSelectorValue != None {
				if currentDeploymentSelectorValue == None {
					// currently, no dedicated selector is present on the lb deployment.  add one.
					dedicatedSelector := v1.NodeSelectorRequirement{
						Key:      lbDedicatedLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{expectedSelectorValue},
					}
					nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions =
						append(nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, dedicatedSelector)
					updatesRequired = append(updatesRequired, "AddDedicatedNodeAffinity")
				} else if currentDeploymentSelectorValue != expectedSelectorValue {
					// dedicated selector expected and current values are different.  update selector value
					matchExpressions := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
					for _, matchExpression := range matchExpressions {
						if lbDedicatedLabel == matchExpression.Key {
							matchExpression.Values = []string{expectedSelectorValue}
							break
						}
					}
					nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = matchExpressions
					updatesRequired = append(updatesRequired, "UpdateDedicatedNodeAffinity")
				}
			}
		}
		// Check if cross loadbalancer anti affinity need to be applied
		// PodAntiAffinity rule set are expected to exist as RequiredDuringSchedulingIgnoredDuringExecution is a must
		if nil != lbDeployment.Spec.Template.Spec.Affinity.PodAntiAffinity {
			if 0 == len(lbDeployment.Spec.Template.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution) {
				lbDeploymentPodAntiAffinitySelector := &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      lbApplicationLabel,
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{c.Config.LBDeployment.Application},
						},
					},
				}
				lbDeployment.Spec.Template.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = []v1.WeightedPodAffinityTerm{
					{
						Weight: 100,
						PodAffinityTerm: v1.PodAffinityTerm{
							LabelSelector: lbDeploymentPodAntiAffinitySelector,
							TopologyKey:   v1.LabelHostname,
						},
					},
				}
				updatesRequired = append(updatesRequired, "PreferredDuringSchedulingPodAntiAffinty")
			}
		}
	}

	if isFeatureEnabled(service, lbFeatureIPVS) {
		// Check if user enabled the IPVS feature on a non-IPVS Service by check if the
		// current LB Deployment doesn't have the IPVS feature enabled
		if !isFeatureEnabledDeployment(lbDeployment, lbFeatureIPVS) {
			localErrStr := "Unsupported - Attempt to enable IPVS feature during service update"
			klog.Error(localErrStr)
			return fmt.Errorf(localErrStr)
		}
		if !servicehelper.RequestsOnlyLocalTraffic(service) {
			klog.Errorf("%s - %v", lbIPVSInvlaidExternalTrafficPolicy, service)
			return fmt.Errorf(lbIPVSInvlaidExternalTrafficPolicy)
		}
		// If the IPVS feature was added and wasn't previously set, we need to go configure it
		lbName := GetCloudProviderLoadBalancerName(service)
		matchLabel := lbNameLabel + "=" + lbName
		listOptions := metav1.ListOptions{LabelSelector: matchLabel}
		cmList, err := c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).List(context.TODO(), listOptions)
		if err != nil {
			klog.Error(err)
			return err
		}

		for i, cm := range cmList.Items {
			cloudProviderIP := cm.Data["externalIP"]
			ipvsCmNew, err := c.createIPVSConfigMapStruct(service, cloudProviderIP, nodes)
			if err != nil {
				klog.Error(err)
				return err
			}
			err = c.createCalicoIngressPolicy(service, cloudProviderIP, c.getLoadBalancerIPTypeLabel(lbDeployment))
			if err != nil {
				klog.Error(err)
				return err
			}
			if !c.isIPVSConfigMapEqual(&cmList.Items[i], ipvsCmNew) {
				cmList.Items[i].Data = ipvsCmNew.Data
				_, err := c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Update(context.TODO(), &cmList.Items[i], metav1.UpdateOptions{})
				if err != nil {
					klog.Error(err)
					return err
				}
			}
		}
	} else if isFeatureEnabledDeployment(lbDeployment, lbFeatureIPVS) { // IPVS Feature is not enabled on the service update
		localErrStr := "Unsupported - Attempt to disable IPVS feature during service update"
		klog.Error(localErrStr)
		return fmt.Errorf(localErrStr)
	}

	// We can live without the LB priority class so only use it if available.
	if lbPriorityClassName != lbDeployment.Spec.Template.Spec.PriorityClassName {
		_, err = c.KubeClient.SchedulingV1().PriorityClasses().Get(context.TODO(), lbPriorityClassName, metav1.GetOptions{})
		if nil == err {
			lbDeployment.Spec.Template.Spec.PriorityClassName = lbPriorityClassName
			updatesRequired = append(updatesRequired, "PriorityClassName")
		}
	}

	// Add new resource requests map if the CPU and/or Memory don't already exist (Value defaults to 0)
	if len(lbDeployment.Spec.Template.Spec.Containers[0].Resources.Requests) < 1 ||
		lbDeployment.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().Value() == 0 ||
		lbDeployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().Value() == 0 {
		lbDeployment.Spec.Template.Spec.Containers[0].Resources.Requests = make(map[v1.ResourceName]resource.Quantity)
	}
	// Add resource requests for CPU and/or memory if they don't already exist
	resources := []v1.ResourceName{v1.ResourceName(v1.ResourceCPU), v1.ResourceName(v1.ResourceMemory)}
	update := false
	for _, r := range resources {
		if _, ok := lbDeployment.Spec.Template.Spec.Containers[0].Resources.Requests[r]; !ok {
			lbDeployment.Spec.Template.Spec.Containers[0].Resources.Requests[r] = resource.MustParse(lbDeploymentResourceRequests[r])
			update = true
		}
	}
	if update {
		updatesRequired = append(updatesRequired, "ResourceRequests")
	}

	// Verify Update LB Deploy for Source IP.
	//    1) Local External Traffic Policy Has been removed, we need to update the load balancer deployment
	//    2) Since no Affinity is defined on this deployment we need to first define it before we can add a podAffinity rule
	//    3) Local External Traffic Policy Has been added, and is not defined in the deployment, we need to update the deployment
	//    4) Local External Traffic Policy Has been enabled and the Match label between the service and the LB Deployment is different
	if !isFeatureEnabledDeployment(lbDeployment, lbFeatureIPVS) {
		updatesRequired = append(updatesRequired, isUpdateSourceIPRequired(lbDeployment, service)...)
	}

	// If necessary, update the load balancer deployment.
	if 0 != len(updatesRequired) {
		_, err = c.KubeClient.AppsV1().Deployments(lbDeployment.ObjectMeta.Namespace).Update(context.TODO(), lbDeployment, metav1.UpdateOptions{})
		if nil != err {
			return fmt.Errorf("Failed to update load balancer deployment %v with changes to %v: %v", lbLogName, updatesRequired, err)
		}
		klog.Infof("Updated Load balancer deployment %v with changes to %v", lbLogName, updatesRequired)
	}

	return nil
}

func (c *Cloud) createIPVSConfigMapStruct(service *v1.Service, lbIP string, nodes []*v1.Node) (*v1.ConfigMap, error) {
	/*
		apiVersion: v1
		kind: ConfigMap
		metadata:
			name: ibm-cloud-provider-ip-169-48-173-194
			namespace: ibm-system
		data:
			externalIP: 169.48.173.194
			externalTrafficPolicy: Local
			healthCheckNodePort: "30520"
			nodes: 10.1.2.3,10.1.2.4,10.1.2.5,10.1.2.6
			ports: 80:30524/TCP,443:31532/TCP
	*/

	dataMap := map[string]string{}
	dataMap["externalIP"] = lbIP
	dataMap["healthCheckNodePort"] = fmt.Sprint(service.Spec.HealthCheckNodePort)

	for _, node := range nodes {
		for _, address := range node.Status.Addresses {
			if address.Type == v1.NodeInternalIP {
				if len(dataMap["nodes"]) > 0 {
					dataMap["nodes"] += ","
				}
				dataMap["nodes"] += address.Address
				break
			}
		}
	}

	for _, port := range service.Spec.Ports {
		portString := fmt.Sprint(port.Port) + ":" + fmt.Sprint(port.NodePort) + "/" + string(port.Protocol)
		if len(dataMap["ports"]) > 0 {
			dataMap["ports"] += ","
		}
		dataMap["ports"] += portString
	}

	schedulerAlgorithm := getSchedulingAlgorithm(service)
	// If there is no scheduling algorithm set, keepalived will use its default.
	if schedulerAlgorithm != "" {
		// Validate customer defined a valid scheduling algorithm
		if !sliceContains(supportedIPVSSchedulerTypes, strings.TrimSpace(schedulerAlgorithm)) {
			return nil, fmt.Errorf(getUnsupportedSchedulerMsg(schedulerAlgorithm))
		}
		dataMap["scheduler"] = schedulerAlgorithm
	}

	dataMap["externalTrafficPolicy"] = string(v1.ServiceExternalTrafficPolicyTypeLocal)

	ipName := lbDeploymentNamePrefix + getCloudProviderIPLabelValue(lbIP)
	labels := map[string]string{}
	labels[lbNameLabel] = GetCloudProviderLoadBalancerName(service)
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ipName,
			Labels: labels,
		},
		Data: dataMap,
	}
	return cm, nil
}

func (c *Cloud) isIPVSConfigMapEqual(cmLeft *v1.ConfigMap, cmRight *v1.ConfigMap) bool {
	commaDelimStrEqual := func(leftStr string, rightStr string) bool {
		leftVals := strings.Split(leftStr, ",")
		rightVals := strings.Split(rightStr, ",")

		if len(leftVals) != len(rightVals) {
			return false
		}

		for _, leftVal := range leftVals {
			found := false
			for _, rightVal := range rightVals {
				if leftVal == rightVal {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}

	if len(cmLeft.Data) != len(cmRight.Data) {
		return false
	}

	for k := range cmLeft.Data {
		if k == "nodes" || k == "ports" {
			if !commaDelimStrEqual(cmLeft.Data[k], cmRight.Data[k]) {
				return false
			}
		} else if cmLeft.Data[k] != cmRight.Data[k] {
			return false
		}
	}

	return true
}

func (c *Cloud) createIPVSConfigMap(ipvsConfigMap *v1.ConfigMap) (result *v1.ConfigMap, err error) {
	_, err = c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Get(context.TODO(), ipvsConfigMap.ObjectMeta.Name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		result, err = c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Create(context.TODO(), ipvsConfigMap, metav1.CreateOptions{})
	} else {
		result, err = c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Update(context.TODO(), ipvsConfigMap, metav1.UpdateOptions{})
	}

	if err == nil {
		klog.Infof("Created IPVS Configmap %v/%v", lbDeploymentNamespace, result.ObjectMeta.Name)
	}
	return
}

func (c *Cloud) deleteIPVSConfigMap(service *v1.Service) error {
	lbName := GetCloudProviderLoadBalancerName(service)
	matchLabel := lbNameLabel + "=" + lbName
	listOptions := metav1.ListOptions{LabelSelector: matchLabel}
	cmList, err := c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).List(context.TODO(), listOptions)
	if err != nil {
		return err
	}
	var ret error
	if len(cmList.Items) > 0 {
		for _, cm := range cmList.Items {
			err = c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Delete(context.TODO(), cm.ObjectMeta.Name, metav1.DeleteOptions{})
			if err != nil {
				ret = err
			}
		}
	}

	return ret
}

func (c *Cloud) createCalicoCfg() (string, error) {
	/* normally, we'd get this from the calico-config configmap.  However, there's a hostname used
	   for ha-masters, so we need to pull that hostname / port from the cluster-info configmap instead
	*/

	var caliCfgYaml string
	calicoCfgFile, err := ioutil.TempFile("", "calicfg")
	if err != nil {
		klog.Error("Unable to create temp calico configuration file")
		return "", err
	}

	// Create Calico configuration file for KDD
	if c.kddEnabled() {
		klog.Info("KDD (Kubernetes Datastore) is enabled on this cluster. Configuring Calico with DATASTORE_TYPE=kubernetes ")
		caliCfgYaml = `apiVersion: projectcalico.org/v3
kind: CalicoAPIConfig
metadata:
spec:
  datastoreType: kubernetes
  kubeconfig: ` + c.Config.Kubernetes.ConfigFilePaths[0]
	} else {

		cm, err := c.KubeClient.CoreV1().ConfigMaps(k8sNamespace).Get(context.TODO(), clusterInfoCM, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Unable to retrieve configmap: %v", clusterInfoCM)
			return "", err
		}
		etcdHost := cm.Data["etcd_host"]
		etcdPort := cm.Data["etcd_port"]
		etcdEndpoints := "https://" + etcdHost + ":" + etcdPort

		secrets, err := c.KubeClient.CoreV1().Secrets(k8sNamespace).Get(context.TODO(), calicoEtcdSecrets, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("Unable to retrieve secrets: %v", calicoEtcdSecrets)
			return "", err
		}

		caFile, err := ioutil.TempFile("", "ca")
		if err != nil {
			klog.Error("Unable to create temp calico ca file")
			return "", err
		}

		certFile, err := ioutil.TempFile("", "cert")
		if err != nil {
			klog.Error("Unable to create temp calico cert file")
			return "", err
		}

		keyFile, err := ioutil.TempFile("", "key")
		if err != nil {
			klog.Error("Unable to create temp calico key file")
			return "", err
		}

		if _, err := caFile.Write(secrets.Data["etcd-ca"]); err != nil {
			klog.Error("Unable to write ca contents to temp file")
			return "", err
		}
		if err := caFile.Close(); err != nil {
			klog.Error("Unable to close ca temp file")
			return "", err
		}

		if _, err := certFile.Write(secrets.Data["etcd-cert"]); err != nil {
			klog.Error("Unable to write cert contents to temp file")
			return "", err
		}
		if err := certFile.Close(); err != nil {
			klog.Error("Unable to close cert temp file")
			return "", err
		}

		if _, err := keyFile.Write(secrets.Data["etcd-key"]); err != nil {
			klog.Error("Unable to write key contents to temp file")
			return "", err
		}
		if err := keyFile.Close(); err != nil {
			klog.Error("Unable to close key temp file")
			return "", err
		}

		caliCfgYaml = `apiVersion: projectcalico.org/v3
kind: CalicoAPIConfig
metadata:
spec:
  etcdEndpoints: ` + etcdEndpoints + `
  etcdKeyFile: ` + keyFile.Name() + `
  etcdCertFile: ` + certFile.Name() + `
  etcdCACertFile: ` + caFile.Name()

	}

	if _, err := calicoCfgFile.Write([]byte(caliCfgYaml)); err != nil {
		klog.Error("Unable to write Calico config contents to temp file")
		return "", err
	}
	if err := calicoCfgFile.Close(); err != nil {
		klog.Error("Unable to close Calico config temp file")
		return "", err
	}
	return calicoCfgFile.Name(), nil
}

func cleanupCalicoCfg(calicoCfgFile string) error {
	calicoCtlCfgBytes, err := ioutil.ReadFile(filepath.Clean(calicoCfgFile))
	if err != nil {
		klog.Error("Unable to read Calico config temp file")
		return err
	}

	calicoCfgLines := strings.Split(string(calicoCtlCfgBytes), "\n")
	for _, line := range calicoCfgLines {
		keyIndex := strings.Index(line, "etcdKeyFile:")
		if keyIndex >= 0 {
			os.Remove(line[keyIndex+13:])
			continue
		}
		certIndex := strings.Index(line, "etcdCertFile:")
		if certIndex >= 0 {
			os.Remove(line[certIndex+14:])
			continue
		}
		caIndex := strings.Index(line, "etcdCACertFile:")
		if caIndex >= 0 {
			os.Remove(line[caIndex+16:])
			continue
		}
	}
	os.Remove(calicoCfgFile)
	return nil
}

// NOTE(cjschaef): this assignment prevents 'gosec' from flagging this as 'G204'. There is no external access, we should not be affected by the issue.
var execCommand = exec.Command

func (c *Cloud) kddEnabled() bool {
	return c.Config.Kubernetes.CalicoDatastore == "KDD"
}

func (c *Cloud) createCalicoIngressPolicy(service *v1.Service, cloudProviderIP string, ipTypeLabel string) error {
	calicoCfgFileName, err := c.createCalicoCfg()
	defer cleanupCalicoCfg(calicoCfgFileName)
	if err != nil {
		return err
	}

	policyYaml := `apiVersion: projectcalico.org/v3
kind: GlobalNetworkPolicy
metadata:
  name: allow-lb-` + GetCloudProviderLoadBalancerName(service) + `
spec:
  applyOnForward: true
  doNotTrack: true`
	if ipTypeLabel == lbPrivateVlanLabel {
		policyYaml += `
  selector: ibm.role in { 'worker_private' }`
	} else {
		policyYaml += `
  selector: ibm.role in { 'worker_public' }`
	}

	policyYaml += `
  order: 1100
  ingress:`

	for _, port := range service.Spec.Ports {
		policyYaml += `
    - action: Allow
      protocol: ` + string(port.Protocol) + `
      destination:
        nets:
        - ` + cloudProviderIP + `/32
        ports:
        - ` + fmt.Sprint(port.Port)
	}

	caliCmd := execCommand("calicoctl", "apply", "--config", calicoCfgFileName, "-f", "-")
	stdin, err := caliCmd.StdinPipe()
	if err != nil {
		klog.Error("Unable to open calico stdin pipe")
		return err
	}

	go func() {
		defer stdin.Close()
		io.WriteString(stdin, policyYaml)
	}()

	stdoutStderr, err := caliCmd.CombinedOutput()
	if err != nil {
		stdoutStderrStr := fmt.Sprintf("Error running calicoctl: %s, %v", string(stdoutStderr), err)
		klog.Errorf(stdoutStderrStr)
		return fmt.Errorf(stdoutStderrStr)
	}
	return nil
}

func (c *Cloud) deleteCalicoIngressPolicy(service *v1.Service) error {
	calicoCfgFileName, err := c.createCalicoCfg()
	defer cleanupCalicoCfg(calicoCfgFileName)
	if err != nil {
		return err
	}

	policyName := "allow-lb-" + GetCloudProviderLoadBalancerName(service)
	caliCmd := execCommand("calicoctl", "delete", "--skip-not-exists", "globalNetworkPolicy", policyName, "--config", calicoCfgFileName)
	// 'err': contains the error information about the cmd. For example the status code.
	// 'stdoutStderr': holds the details such as the actual error message that is returned
	stdoutStderr, err := caliCmd.CombinedOutput()
	if err != nil {
		stdoutStderrStr := fmt.Sprintf("Error running calicoctl: %s, %v", string(stdoutStderr), err)
		klog.Errorf(stdoutStderrStr)
		return fmt.Errorf(stdoutStderrStr)
	}
	return nil
}

// GetLoadBalancerName returns the name of the load balancer. Implementations must treat the
// *v1.Service parameter as read-only and not modify it.
func (c *Cloud) GetLoadBalancerName(ctx context.Context, clusterName string, service *v1.Service) string {
	// For a VPC cluster, we use a slightly different load balancer name
	if c.isProviderVpc() {
		return c.vpcGetLoadBalancerName(service)
	}
	return GetCloudProviderLoadBalancerName(service)
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
	lbName := GetCloudProviderLoadBalancerName(service)
	klog.Infof("GetLoadBalancer(%v, %v)", lbName, clusterName)
	lbDeployment, err := c.getLoadBalancerDeployment(lbName)
	if nil != err {
		err = c.Recorder.LoadBalancerServiceWarningEvent(
			service, GettingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to get deployment: %v", err),
		)
		return nil, false, err
	} else if nil == lbDeployment {
		klog.Infof("Load balancer %v not found", lbName)
		return nil, false, nil
	}
	cloudProviderIP := getSelectorCloudProviderIP(lbDeployment.Spec.Selector)
	klog.Infof("Load balancer %v found", getLoadBalancerLogName(lbName, cloudProviderIP))
	return getLoadBalancerStatus(cloudProviderIP), true, nil
}

func isUpdateSourceIPRequired(lbDeployment *apps.Deployment, service *v1.Service) []string {

	localUpdatesRequired := []string{}
	deploymentStrategy := lbDeployment.Spec.Strategy

	if !servicehelper.RequestsOnlyLocalTraffic(service) {

		if lbDeployment.Spec.Template.Spec.Affinity != nil &&
			lbDeployment.Spec.Template.Spec.Affinity.PodAffinity != nil {

			// Local External Traffic Policy Has been removed, we need to update the load balancer deployment
			klog.Infof("Removing Pod Affinity For - Service: %v Deployment: %v", service.Name, lbDeployment.Name)
			lbDeployment.Spec.Template.Spec.Affinity.PodAffinity = nil
			localUpdatesRequired = append(localUpdatesRequired, "RemoveSourceIPPodAffinity")
		}
	} else {
		// Check when service has local only traffic that max unavailability is set to 100%
		if deploymentStrategy.Type != apps.RollingUpdateDeploymentStrategyType ||
			deploymentStrategy.RollingUpdate == nil ||
			deploymentStrategy.RollingUpdate.MaxUnavailable == nil ||
			strings.Compare("100%", deploymentStrategy.RollingUpdate.MaxUnavailable.StrVal) != 0 {

			var lbDeploymentMaxSurge = intstr.FromInt(1)
			var lbDeploymentMaxUnavailable = intstr.FromString("100%")

			lbDeployment.Spec.Strategy = apps.DeploymentStrategy{
				Type: apps.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &apps.RollingUpdateDeployment{
					MaxUnavailable: &lbDeploymentMaxUnavailable,
					MaxSurge:       &lbDeploymentMaxSurge,
				},
			}

			localUpdatesRequired = append(localUpdatesRequired, "UpdateSourceIPPodMaxUnavailable")

			klog.Infof("Detected Service has External Traffic Policy set to Local. Setting Max Unavailability "+
				"of the LoadBalancer to 100%% to allow for updates to work correctly in the future. Service: %s, LB Name: %s", service.Name, lbDeployment.Name)
		}

		// Ensure Affinity is defined
		if lbDeployment.Spec.Template.Spec.Affinity == nil {
			// Since no Affinity is defined on this deployment we need to first define it before we can add a podAffinity rule
			lbDeployment.Spec.Template.Spec.Affinity = &v1.Affinity{}
		}
		// Ensure Pod Affinity is defined
		if lbDeployment.Spec.Template.Spec.Affinity.PodAffinity == nil {
			lbDeployment.Spec.Template.Spec.Affinity.PodAffinity = &v1.PodAffinity{}
		}

		klog.Infof("Service Pod Affinity: %v LB Deployment Affinity: %v", service.Spec.Selector, lbDeployment.Spec.Template.Spec.Affinity.PodAffinity)

		if !podAffinityMatchLabelAndServiceSelectorEqual(lbDeployment.Spec.Template.Spec.Affinity.PodAffinity, service.Spec.Selector) {
			// Local External Traffic Policy Has been added, and is not defined in the deployment, we need to update the deployment
			//    or
			// Local External Traffic Policy Has been enabled and the Match label between the service and the LB Deployment is different
			lbServiceLabelSelector := &metav1.LabelSelector{
				MatchLabels: service.Spec.Selector,
			}
			lbPodAffinity := &v1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
					{
						LabelSelector: lbServiceLabelSelector,
						TopologyKey:   v1.LabelHostname,
						Namespaces:    []string{service.Namespace},
					},
				},
			}
			lbDeployment.Spec.Template.Spec.Affinity.PodAffinity = lbPodAffinity
			localUpdatesRequired = append(localUpdatesRequired, "DifferentSourceIPAffinityWithPodAffinity")

			klog.Infof("Enabled Pod Affinity - Service: %v Deployment: %v MatchLabels: %v", service.Name, lbDeployment.Name, service.Spec.Selector)
		}
	}

	return localUpdatesRequired
}

// EnsureLoadBalancer creates a new load balancer 'name', or updates the existing one. Returns the status of the balancer
// Implementations must treat the *v1.Service and *v1.Node
// parameters as read-only and not modify them.
// Parameter 'clusterName' is the name of the cluster as presented to kube-controller-manager
func (c *Cloud) EnsureLoadBalancer(ctx context.Context, clusterName string, service *v1.Service, nodes []*v1.Node) (*v1.LoadBalancerStatus, error) {

	// Verify that the load balancer service configuration is supported.
	err := isServiceConfigurationSupported(service)
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

	var lbLogName string
	lbName := GetCloudProviderLoadBalancerName(service)
	requestedCloudProviderIP := service.Spec.LoadBalancerIP
	klog.Infof(
		"EnsureLoadBalancer(%v, %v, %v, %v, %v) - Service Name: %v - Selector: %v",
		clusterName,
		lbName,
		requestedCloudProviderIP,
		service.Spec.LoadBalancerSourceRanges,
		service.Annotations,
		service.Name,
		service.Spec.Selector,
	)

	// Get the load balancer deployment.
	lbDeployment, err := c.getLoadBalancerDeployment(lbName)
	if nil != err {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to get deployment: %v", err),
		)
	} else if nil != lbDeployment {
		// The load balancer deployment already exists.
		cloudProviderIP := getSelectorCloudProviderIP(lbDeployment.Spec.Selector)
		lbLogName := getLoadBalancerLogName(lbName, cloudProviderIP)
		if 0 != len(requestedCloudProviderIP) && 0 != strings.Compare(requestedCloudProviderIP, cloudProviderIP) {
			return nil, c.Recorder.LoadBalancerServiceWarningEvent(
				service, UpdatingCloudLoadBalancerFailed,
				fmt.Sprintf("Updating cloud provider IP to %v is not supported", requestedCloudProviderIP),
			)
		}
		// Ensure that the load balancer deployment is up-to-date.
		err = c.updateLoadBalancerDeployment(lbLogName, lbDeployment, service, nodes)
		if nil != err {
			return nil, c.Recorder.LoadBalancerWarningEvent(
				lbDeployment, service, UpdatingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to update deployment: %v", err),
			)
		}
		klog.Infof("Load balancer %v exists", lbLogName)
		return getLoadBalancerStatus(cloudProviderIP), nil
	}

	// Get the cloud provider VLAN IPs request information.
	cloudProviderIPType, cloudProviderIPReservation, lbVlanLabel, cloudProviderZone, cloudProviderVlan, err := c.getCloudProviderVlanIPsRequest(service)
	if nil != err {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to determine cloud provider IPs to request for load balancer services: %v", err),
		)
	}

	// Get the available cloud provider VLAN IP config.
	klog.Infof(
		"Requesting available %v %v cloud provider IPs for load balancer %v",
		cloudProviderIPType, cloudProviderIPReservation, lbName,
	)
	if cloudProviderZone != "" {
		klog.Infof(
			"Requesting from Zone %v",
			cloudProviderZone,
		)
	}
	if cloudProviderVlan != "" {
		klog.Infof(
			"Requesting from Vlan %v",
			cloudProviderVlan,
		)
	}
	availableCloudProviderVlanIDs := map[string][]string{}
	availableCloudProviderIPs := map[string]string{}
	availableCloudProviderVlanErrors := map[string][]subnetConfigErrorField{}
	err = c.populateAvailableCloudProviderVlanIPConfig(
		availableCloudProviderVlanIDs,
		availableCloudProviderIPs,
		availableCloudProviderVlanErrors,
		cloudProviderIPType,
		cloudProviderIPReservation,
		cloudProviderZone,
		cloudProviderVlan,
	)
	if nil != err {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to get available cloud provider IPs for load balancer services: %v", err),
		)
	}

	// Ensure there is at least one available cloud provider IP.
	if 0 == len(availableCloudProviderIPs) {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			getLoadBalancerPortableSubnetPossibleErrors(availableCloudProviderVlanErrors),
		)
	}

	// Only use cloud provider IPs that have nodes on the available VLANs.
	for cloudProviderVlanID, cloudProviderIPs := range availableCloudProviderVlanIDs {
		nodes, err := c.KubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{LabelSelector: lbVlanLabel + "=" + cloudProviderVlanID})
		if nil != err {
			return nil, c.Recorder.LoadBalancerServiceWarningEvent(
				service, CreatingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to list nodes: %v", err),
			)
		} else if 0 == len(nodes.Items) {
			klog.Warningf(
				"No available nodes with label %v=%v to support cloud provider IPs %v while creating load balancer %v",
				lbVlanLabel, cloudProviderVlanID, cloudProviderIPs, lbName,
			)
			for _, cloudProviderIP := range cloudProviderIPs {
				delete(availableCloudProviderIPs, cloudProviderIP)
			}
		}
	}
	if 0 == len(availableCloudProviderIPs) {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			"No available nodes for load balancer services",
		)
	}
	// Search for all resources managing cloud provider IPs and remove the
	// IPs from the available map.
	listOptions := metav1.ListOptions{LabelSelector: lbIPLabel}
	deployments, err := c.KubeClient.AppsV1().Deployments(lbDeploymentNamespace).List(context.TODO(), listOptions)
	if nil != err {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to list deployments in namespace %v: %v", lbDeploymentNamespace, err),
		)
	}
	replicasets, err := c.KubeClient.AppsV1().ReplicaSets(lbDeploymentNamespace).List(context.TODO(), listOptions)
	if nil != err {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to list replicasets in namespace %v: %v", lbDeploymentNamespace, err),
		)
	}
	pods, err := c.KubeClient.CoreV1().Pods(lbDeploymentNamespace).List(context.TODO(), listOptions)
	if nil != err {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to list pods in namespace %v: %v", lbDeploymentNamespace, err),
		)
	}
	removeCloudProviderIP := func(availableCloudProviderIPs map[string]string, inuseCloudProviderIP string) {
		if _, ok := availableCloudProviderIPs[inuseCloudProviderIP]; ok {
			klog.Infof("Found in-use cloud provider IP %v while creating load balancer %v", inuseCloudProviderIP, lbName)
		}
		delete(availableCloudProviderIPs, inuseCloudProviderIP)
	}
	for _, deployment := range deployments.Items {
		inuseCloudProviderIP := getSelectorCloudProviderIP(deployment.Spec.Selector)
		removeCloudProviderIP(availableCloudProviderIPs, inuseCloudProviderIP)
	}
	for _, replicaset := range replicasets.Items {
		inuseCloudProviderIP := getSelectorCloudProviderIP(replicaset.Spec.Selector)
		removeCloudProviderIP(availableCloudProviderIPs, inuseCloudProviderIP)
	}
	for _, pod := range pods.Items {
		inuseCloudProviderIP := getLabelsCloudProviderIP(pod.Labels)
		removeCloudProviderIP(availableCloudProviderIPs, inuseCloudProviderIP)
	}

	// Use the requested cloud provider IP if available.
	selectedCloudProviderIPErrorMessage := lbDefaultNoIPPortableSubnetErrorMsg
	if 0 != len(requestedCloudProviderIP) {
		klog.Infof("Requesting cloud provider IP %v for load balancer %v", requestedCloudProviderIP, lbName)
		var availableCloudProviderIPsList []string
		for cloudProviderIP := range availableCloudProviderIPs {
			if cloudProviderIP != requestedCloudProviderIP {
				availableCloudProviderIPsList = append(availableCloudProviderIPsList, cloudProviderIP)
			}
		}
		if 0 != len(availableCloudProviderIPsList) {
			sort.Strings(availableCloudProviderIPsList)
			selectedCloudProviderIPErrorMessage = fmt.Sprintf(
				"Requested cloud provider IP %v is not available. The following cloud provider IPs are available: %v",
				requestedCloudProviderIP,
				strings.Join(availableCloudProviderIPsList, ","),
			)
		}
		vlandid, ok := availableCloudProviderIPs[requestedCloudProviderIP]
		if ok {
			availableCloudProviderIPs = map[string]string{requestedCloudProviderIP: vlandid}
		} else {
			if selectedCloudProviderIPErrorMessage == lbDefaultNoIPPortableSubnetErrorMsg {
				selectedCloudProviderIPErrorMessage = getLoadBalancerPortableSubnetPossibleErrors(availableCloudProviderVlanErrors)
			}
			return nil, c.Recorder.LoadBalancerServiceWarningEvent(
				service, CreatingCloudLoadBalancerFailed,
				selectedCloudProviderIPErrorMessage,
			)
		}
	}

	klog.Infof("Available cloud provider IPs %v while creating load balancer %v", availableCloudProviderIPs, lbName)
	var selectedCloudProviderIP string
	for cloudProviderIP, vlanID := range availableCloudProviderIPs {
		gatewayNodeFound := false
		edgeNodeFound := false
		var vlanLabel string

		if cloudProviderIPType == PrivateIP {
			vlanLabel = lbPrivateVlanLabel + "=" + vlanID
		} else {
			vlanLabel = lbPublicVlanLabel + "=" + vlanID
		}
		nodeListOptions := metav1.ListOptions{LabelSelector: vlanLabel + "," + lbDedicatedLabel + "=" + lbGatewayNodeValue}
		gatewayNodes, err := c.KubeClient.CoreV1().Nodes().List(context.TODO(), nodeListOptions)
		if nil != err {
			return nil, c.Recorder.LoadBalancerServiceWarningEvent(
				service, CreatingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to list nodes: %v", err),
			)
		} else if len(gatewayNodes.Items) > 0 {
			gatewayNodeFound = true
		}

		nodeListOptions = metav1.ListOptions{LabelSelector: vlanLabel + "," + lbDedicatedLabel + "=" + lbEdgeNodeValue}
		edgeNodes, err := c.KubeClient.CoreV1().Nodes().List(context.TODO(), nodeListOptions)
		if nil != err {
			return nil, c.Recorder.LoadBalancerServiceWarningEvent(
				service, CreatingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to list nodes: %v", err),
			)
		} else if len(edgeNodes.Items) > 0 {
			edgeNodeFound = true
		}

		lbLogName = getLoadBalancerLogName(lbName, cloudProviderIP)
		klog.Infof("Creating deployment for load balancer %v", lbLogName)
		lbDeploymentName := getLoadBalancerDeploymentName(cloudProviderIP)
		lbDeploymentLabels := map[string]string{
			lbIPLabel:          getCloudProviderIPLabelValue(cloudProviderIP),
			lbNameLabel:        GetCloudProviderLoadBalancerName(service),
			lbApplicationLabel: c.Config.LBDeployment.Application,
		}
		lbDeploymentLabelSelector := &metav1.LabelSelector{
			MatchLabels: map[string]string{
				lbIPLabel:   lbDeploymentLabels[lbIPLabel],
				lbNameLabel: lbDeploymentLabels[lbNameLabel],
			},
		}
		lbDeploymentPodAntiAffinitySelector := &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      lbApplicationLabel,
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{c.Config.LBDeployment.Application},
				},
			},
		}
		lbPodAntiAffinity := &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: lbDeploymentLabelSelector,
					TopologyKey:   v1.LabelHostname,
				},
			},
			PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
				{
					Weight: 100,
					PodAffinityTerm: v1.PodAffinityTerm{
						LabelSelector: lbDeploymentPodAntiAffinitySelector,
						TopologyKey:   v1.LabelHostname,
					},
				},
			},
		}
		lbNodeAffinity := &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{{
					MatchExpressions: []v1.NodeSelectorRequirement{{
						Key:      lbVlanLabel,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{vlanID},
					}},
				}},
			},
		}
		if gatewayNodeFound {
			gatewayNodeSelector := v1.NodeSelectorRequirement{
				Key:      lbDedicatedLabel,
				Operator: v1.NodeSelectorOpIn,
				Values:   []string{lbGatewayNodeValue},
			}
			lbNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions =
				append(lbNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, gatewayNodeSelector)
		} else if edgeNodeFound {
			edgeNodeSelector := v1.NodeSelectorRequirement{
				Key:      lbDedicatedLabel,
				Operator: v1.NodeSelectorOpIn,
				Values:   []string{lbEdgeNodeValue},
			}
			lbNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions =
				append(lbNodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, edgeNodeSelector)
		}
		lbDeploymentAffinity := &v1.Affinity{
			PodAntiAffinity: lbPodAntiAffinity,
			NodeAffinity:    lbNodeAffinity,
		}
		lbDeploymentTolerations := []v1.Toleration{
			{
				Key:   lbTolerationKey,
				Value: lbTolerationValueGateway,
			},
			{
				Key:   lbTolerationKey,
				Value: lbTolerationValueEdge,
			},
		}

		// -------------------------------------------------------------------------------------------------------------
		// Service.Spec.ExternalTrafficPolicy == "Local"
		// -------------------------------------------------------------------------------------------------------------
		// NOTE(tjcocozz): Within the `ExternalTrafficPolicy == "Local"` code we only allow ONE LoadBalancer Deployment
		// `RequiredDuringSchedulingIgnoredDuringExecution.PodAffinityTerm` within the IBM Loadbalacer code. This is
		// a limitation of the design, since the `PodAffinityTerm` is defined by what is set in
		// the `service.Spec.Selector` and there is no way to track more than one PodAffinityTerm per all Service Selectors.
		if !isFeatureEnabled(service, lbFeatureIPVS) {
			//Note: we don't want do do this with IPVS as IPVS maintains src IP through
			// IPIP encapsulation when routing to each node
			lbServiceLabelSelector := &metav1.LabelSelector{
				MatchLabels: service.Spec.Selector,
			}
			lbPodAffinity := &v1.PodAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
					{
						LabelSelector: lbServiceLabelSelector,
						TopologyKey:   v1.LabelHostname,
						Namespaces:    []string{service.Namespace},
					},
				},
			}
			if servicehelper.RequestsOnlyLocalTraffic(service) {

				klog.Infof("Adding Pod Affinity to LB Deploy. Service: %v - Pod Affinity: %v - Selector: %v",
					service.Name,
					lbPodAffinity,
					lbServiceLabelSelector)

				lbDeploymentAffinity.PodAffinity = lbPodAffinity
			}
		}

		var ipvsCm *v1.ConfigMap
		if isFeatureEnabled(service, lbFeatureIPVS) {
			// Check customer isn't enabling cluster networking with the IPVS LB
			if !servicehelper.RequestsOnlyLocalTraffic(service) {
				klog.Errorf("%s - %v", lbIPVSInvlaidExternalTrafficPolicy, service)
				return nil, c.Recorder.LoadBalancerServiceWarningEvent(
					service, CreatingCloudLoadBalancerFailed,
					fmt.Sprint(lbIPVSInvlaidExternalTrafficPolicy),
				)
			}

			// Check if the service account is created
			_, err := c.KubeClient.CoreV1().ServiceAccounts(lbDeploymentNamespace).Get(context.TODO(), lbDeploymentServiceAccountName, metav1.GetOptions{})
			if err != nil {
				return nil, c.Recorder.LoadBalancerServiceWarningEvent(
					service, CreatingCloudLoadBalancerFailed, err.Error())
			}

			localIPVSCM, err := c.createIPVSConfigMapStruct(service, cloudProviderIP, nodes)
			if err != nil {
				return nil, c.Recorder.LoadBalancerServiceWarningEvent(
					service, CreatingCloudLoadBalancerFailed,
					fmt.Sprintf("Failed to generate IPVS configmap %v", err),
				)
			}
			ipvsCm, err = c.createIPVSConfigMap(localIPVSCM)
			if err != nil {
				return nil, c.Recorder.LoadBalancerServiceWarningEvent(
					service, CreatingCloudLoadBalancerFailed,
					fmt.Sprintf("Failed to create IPVS configmap %v", err),
				)
			}
			ipTypeLabel := lbPublicVlanLabel
			if cloudProviderIPType == PrivateIP {
				ipTypeLabel = lbPrivateVlanLabel
			}
			err = c.createCalicoIngressPolicy(service, cloudProviderIP, ipTypeLabel)
			if err != nil {
				return nil, c.Recorder.LoadBalancerServiceWarningEvent(
					service, CreatingCloudLoadBalancerFailed,
					fmt.Sprintf("Failed to create Calico ingress policy %v", err),
				)
			}
		}
		// -------------------------------------------------------------------------------------------------------------

		lbDeploymentPrivileged := false
		// NOTE(rtheis): Use a rolling update deployment strategy to keep at least one
		// load balancer pod running during an update (assuming enough nodes available).
		// This configuration minimizes downtime during load balancer deployment updates.
		// Only one deployment revision will be saved for rollback purposes.
		var lbDeploymentReplicas int32 = 2
		var lbDeploymentRevisionHistoryLimit int32 = 1
		var lbDeploymentMinReadySeconds int32 = 90
		lbDeploymentMaxUnavailable := intstr.FromInt(1)
		lbDeploymentMaxSurge := intstr.FromInt(1)
		lbDeploymentStrategy := apps.DeploymentStrategy{
			Type: apps.RollingUpdateDeploymentStrategyType,
			RollingUpdate: &apps.RollingUpdateDeployment{
				MaxUnavailable: &lbDeploymentMaxUnavailable,
				MaxSurge:       &lbDeploymentMaxSurge,
			},
		}

		var nodes *v1.NodeList
		switch {
		case gatewayNodeFound:
			nodes = gatewayNodes
		case edgeNodeFound:
			nodes = edgeNodes
		default:
			nodeListOptions := metav1.ListOptions{LabelSelector: lbVlanLabel + "=" + vlanID}
			nodes, err = c.KubeClient.CoreV1().Nodes().List(context.TODO(), nodeListOptions)

			if nil != err {
				return nil, c.Recorder.LoadBalancerServiceWarningEvent(
					service, CreatingCloudLoadBalancerFailed,
					fmt.Sprintf("Failed to list nodes: %v", err),
				)
			}
		}

		if 2 > len(nodes.Items) || (servicehelper.RequestsOnlyLocalTraffic(service) && !isFeatureEnabled(service, lbFeatureIPVS)) {
			// NOTE: This is verifying all LoadBalancers can restart properly when there is only 1
			// node or the legacy LB has 1 backing pod. In the update path we
			// will only enabled 100% Max Unavailability and will never disable it.
			lbDeploymentMaxUnavailable = intstr.FromString("100%")
			lbDeploymentStrategy.RollingUpdate.MaxUnavailable = &lbDeploymentMaxUnavailable
		}

		envVars := []v1.EnvVar{
			{Name: "VIRTUAL_IP", Value: cloudProviderIP},
			{Name: "FEATURES", Value: service.Annotations[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures]},
		}

		if isFeatureEnabled(service, lbFeatureIPVS) {
			cfgMapEnvVar := v1.EnvVar{
				Name:  "CONFIG_MAP",
				Value: ipvsCm.Namespace + "/" + ipvsCm.Name,
			}
			envVars = append(envVars, cfgMapEnvVar)
		}

		// We can live without the LB priority class so only use it if available.
		lbActualPriorityClassName := ""
		_, err = c.KubeClient.SchedulingV1().PriorityClasses().Get(context.TODO(), lbPriorityClassName, metav1.GetOptions{})
		if nil == err {
			lbActualPriorityClassName = lbPriorityClassName
		}

		lbDeployment := &apps.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lbDeploymentName,
				Namespace: lbDeploymentNamespace,
				Labels:    lbDeploymentLabels,
			},
			Spec: apps.DeploymentSpec{
				Replicas:             &lbDeploymentReplicas,
				RevisionHistoryLimit: &lbDeploymentRevisionHistoryLimit,
				Selector:             lbDeploymentLabelSelector,
				MinReadySeconds:      lbDeploymentMinReadySeconds,
				Strategy:             lbDeploymentStrategy,
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Name:   lbDeploymentName,
						Labels: lbDeploymentLabels,
					},
					Spec: v1.PodSpec{
						Affinity:    lbDeploymentAffinity,
						Tolerations: lbDeploymentTolerations,
						InitContainers: []v1.Container{
							{
								Name:            lbDeploymentNamePrefix + "keepalived-init",
								Image:           c.Config.LBDeployment.Image,
								Command:         []string{"/usr/local/bin/hostDirPerms"},
								ImagePullPolicy: v1.PullIfNotPresent,
								VolumeMounts: []v1.VolumeMount{
									{
										Name:      c.Config.LBDeployment.Application + "-status",
										MountPath: "/status",
									},
								},
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceName(v1.ResourceCPU):    resource.MustParse(lbDeploymentResourceRequests[v1.ResourceName(v1.ResourceCPU)]),
										v1.ResourceName(v1.ResourceMemory): resource.MustParse(lbDeploymentResourceRequests[v1.ResourceName(v1.ResourceMemory)]),
									},
								},
								SecurityContext: &v1.SecurityContext{
									RunAsUser:  &lbRootUser,
									RunAsGroup: &lbRootGroup,
								},
							},
						},
						Containers: []v1.Container{
							{
								Name:            lbDeploymentName,
								Image:           c.Config.LBDeployment.Image,
								ImagePullPolicy: v1.PullIfNotPresent,
								Env:             envVars,
								VolumeMounts: []v1.VolumeMount{
									{
										Name:      c.Config.LBDeployment.Application + "-status",
										MountPath: "/status",
									},
								},
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceName(v1.ResourceCPU):    resource.MustParse(lbDeploymentResourceRequests[v1.ResourceName(v1.ResourceCPU)]),
										v1.ResourceName(v1.ResourceMemory): resource.MustParse(lbDeploymentResourceRequests[v1.ResourceName(v1.ResourceMemory)]),
									},
								},
								SecurityContext: &v1.SecurityContext{
									RunAsUser:  &lbNonRootUser,
									RunAsGroup: &lbNonRootGroup,
									Privileged: &lbDeploymentPrivileged,
									Capabilities: &v1.Capabilities{
										Add: []v1.Capability{lbNetAdminCapability, lbNetRawCapability},
									},
								},
							},
						},
						Volumes: []v1.Volume{
							{
								Name: c.Config.LBDeployment.Application + "-status",
								VolumeSource: v1.VolumeSource{
									HostPath: &v1.HostPathVolumeSource{
										Path: "/tmp/" + c.Config.LBDeployment.Application,
									},
								},
							},
						},
						HostNetwork:       true,
						PriorityClassName: lbActualPriorityClassName,
					},
				},
			},
		}
		if isFeatureEnabled(service, lbFeatureIPVS) {
			// Only use the service account for IPVS load balancers
			lbDeployment.Spec.Template.Spec.ServiceAccountName = lbDeploymentServiceAccountName
		}
		_, err = c.KubeClient.AppsV1().Deployments(lbDeploymentNamespace).Create(context.TODO(), lbDeployment, metav1.CreateOptions{})
		if nil != err {
			_, tmpErr := c.KubeClient.AppsV1().Deployments(lbDeploymentNamespace).Get(context.TODO(), lbDeploymentName, metav1.GetOptions{})
			if nil == tmpErr {
				// Another load balancer creation beat us to this cloud provider IP.
				klog.Infof("Cloud provider IP %v now in-use for load balancer %v", cloudProviderIP, lbLogName)
				continue
			} else {
				return nil, c.Recorder.LoadBalancerServiceWarningEvent(
					service, CreatingCloudLoadBalancerFailed,
					fmt.Sprintf("Failed to create deployment %v: %v", types.NamespacedName{Namespace: lbDeploymentNamespace, Name: lbDeploymentName}, err),
				)
			}
		}
		selectedCloudProviderIP = cloudProviderIP
		break
	}
	if 0 == len(selectedCloudProviderIP) {
		return nil, c.Recorder.LoadBalancerServiceWarningEvent(
			service, CreatingCloudLoadBalancerFailed,
			selectedCloudProviderIPErrorMessage,
		)
	}

	klog.Infof("Load balancer %v created", lbLogName)
	return getLoadBalancerStatus(selectedCloudProviderIP), nil
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
	klog.Infof("UpdateLoadBalancer(%v, %v, %v)", clusterName, service, len(nodes))

	lbName := GetCloudProviderLoadBalancerName(service)
	lbDeployment, err := c.getLoadBalancerDeployment(lbName)
	if err != nil {
		return c.Recorder.LoadBalancerServiceWarningEvent(
			service, UpdatingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to get deployment. %v", err))
	} else if nil == lbDeployment {
		// The load balancer deployment has not yet been created.
		klog.Infof("Load balancer %v does not exist", lbName)
		return nil
	}
	if isFeatureEnabledDeployment(lbDeployment, lbFeatureIPVS) {
		matchLabel := lbNameLabel + "=" + lbName
		listOptions := metav1.ListOptions{LabelSelector: matchLabel}
		cmList, err := c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).List(context.TODO(), listOptions)
		if err != nil {
			return c.Recorder.LoadBalancerServiceWarningEvent(
				service, UpdatingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to update IPVS configmap %v", err))
		}

		for i, cm := range cmList.Items {
			cloudProviderIP := cm.Data["externalIP"]
			ipvsCmNew, err := c.createIPVSConfigMapStruct(service, cloudProviderIP, nodes)
			if err != nil {
				return c.Recorder.LoadBalancerServiceWarningEvent(
					service, UpdatingCloudLoadBalancerFailed,
					fmt.Sprintf("Failed to generate IPVS configmap %v", err))
			}
			if !c.isIPVSConfigMapEqual(&cmList.Items[i], ipvsCmNew) {
				cmList.Items[i].Data = ipvsCmNew.Data
				err := c.createCalicoIngressPolicy(service, cloudProviderIP, c.getLoadBalancerIPTypeLabel(lbDeployment))
				if err != nil {
					klog.Errorf("Unable to update calico ingress policy for service: %v", service.Name)
					return err
				}
				_, err = c.KubeClient.CoreV1().ConfigMaps(lbDeploymentNamespace).Update(context.TODO(), &cmList.Items[i], metav1.UpdateOptions{})
				if err != nil {
					return c.Recorder.LoadBalancerServiceWarningEvent(
						service, UpdatingCloudLoadBalancerFailed,
						fmt.Sprintf("Failed to update IPVS configmap %v", err))
				}
			}
		}
	}
	return nil
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
	lbName := GetCloudProviderLoadBalancerName(service)
	klog.Infof("EnsureLoadBalancerDeleted(%v, %v)", lbName, clusterName)

	var err error
	var lbDeployment *apps.Deployment

	// Get the load balancer deployment.
	lbDeployment, err = c.getLoadBalancerDeployment(lbName)
	if nil != err {
		return c.Recorder.LoadBalancerServiceWarningEvent(
			service, DeletingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to get deployment: %v", err),
		)
	} else if nil == lbDeployment {
		// The load balancer deployment has already been deleted.
		klog.Infof("Load balancer %v does not exist", lbName)
		return nil
	}
	lbLogName := getLoadBalancerLogName(lbName, getSelectorCloudProviderIP(lbDeployment.Spec.Selector))

	// Pause and scale down the load balancer deployment. This is required
	// so that all resources can be deleted.
	var revisionHistoryLimit int32
	var replicas int32
	lbDeployment.Spec.RevisionHistoryLimit = &revisionHistoryLimit
	lbDeployment.Spec.Replicas = &replicas
	lbDeployment.Spec.Paused = true
	lbDeployment, err = c.KubeClient.AppsV1().Deployments(lbDeployment.ObjectMeta.Namespace).Update(context.TODO(), lbDeployment, metav1.UpdateOptions{})
	if nil != err {
		return c.Recorder.LoadBalancerWarningEvent(
			lbDeployment, service, DeletingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to update deployment: %v", err),
		)
	}

	klog.Infof("Waiting for update to deployment for load balancer %v ...", lbLogName)
	waitInterval := time.Second * 2
	waitTimeout := time.Minute * 5
	if err := waitForObservedDeployment(func() (*apps.Deployment, error) {
		return c.KubeClient.AppsV1().Deployments(lbDeployment.ObjectMeta.Namespace).Get(context.TODO(), lbDeployment.ObjectMeta.Name, metav1.GetOptions{})
	}, lbDeployment.Generation, waitInterval, waitTimeout); err != nil {
		return c.Recorder.LoadBalancerWarningEvent(
			lbDeployment, service, DeletingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to update deployment to zero replicas: %v", err),
		)
	}

	// Get the load balancer replicasets for the load balancer deployment.
	allLbReplicaSets, err := c.KubeClient.AppsV1().ReplicaSets(lbDeploymentNamespace).List(context.TODO(), metav1.ListOptions{LabelSelector: lbNameLabel + "=" + lbName})
	if nil != err {
		return c.Recorder.LoadBalancerWarningEvent(
			lbDeployment, service, DeletingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to get deployment replicasets: %v", err),
		)
	}

	// Scale down then delete all replicasets for the load balancer deployment.
	// This is required so that all resources can be deleted.
	var gracePeriodSeconds int64
	deletePropagationBackground := metav1.DeletePropagationBackground
	deleteOptions := metav1.DeleteOptions{GracePeriodSeconds: &gracePeriodSeconds, PropagationPolicy: &deletePropagationBackground}
	for i := range allLbReplicaSets.Items {
		lbReplicaSetName := allLbReplicaSets.Items[i].ObjectMeta.Name
		lbReplicaSetNamespace := allLbReplicaSets.Items[i].ObjectMeta.Namespace
		lbReplicaSetNamespacedName := types.NamespacedName{Namespace: lbReplicaSetNamespace, Name: lbReplicaSetName}
		allLbReplicaSets.Items[i].Spec.Replicas = &replicas
		lbReplicaSet, err := c.KubeClient.AppsV1().ReplicaSets(lbReplicaSetNamespace).Update(context.TODO(), &allLbReplicaSets.Items[i], metav1.UpdateOptions{})
		if nil != err {
			return c.Recorder.LoadBalancerWarningEvent(
				lbDeployment, service, DeletingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to update deployment replicaset %v: %v", lbReplicaSetNamespacedName, err),
			)
		}
		klog.Infof("Waiting for update to deployment replicaset %v for load balancer %v ...", lbReplicaSetNamespacedName, lbLogName)
		err = wait.Poll(waitInterval, waitTimeout, replicaSetHasDesiredReplicas(c.KubeClient, lbReplicaSet))
		if nil != err {
			return c.Recorder.LoadBalancerWarningEvent(
				lbDeployment, service, DeletingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to update deployment replicaset %v to zero replicas: %v", lbReplicaSetNamespacedName, err),
			)
		}
		err = c.KubeClient.AppsV1().ReplicaSets(lbReplicaSetNamespace).Delete(context.TODO(), lbReplicaSetName, deleteOptions)
		if nil != err {
			return c.Recorder.LoadBalancerWarningEvent(
				lbDeployment, service, DeletingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to delete deployment replicaset %v: %v", lbReplicaSetNamespacedName, err),
			)
		}
	}

	ipvsEnabled := isFeatureEnabledDeployment(lbDeployment, lbFeatureIPVS)

	if ipvsEnabled {
		var ret error
		err = c.deleteCalicoIngressPolicy(service)

		if nil != err {
			ret = c.Recorder.LoadBalancerWarningEvent(
				lbDeployment, service,
				DeletingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to delete calico ingress policy: %v", err),
			)
		}

		err = c.deleteIPVSConfigMap(service)
		if nil != err && !errors.IsNotFound(err) {
			ret = fmt.Errorf("%v, %v", ret, c.Recorder.LoadBalancerWarningEvent(
				lbDeployment, service,
				DeletingCloudLoadBalancerFailed,
				fmt.Sprintf("Failed to delete IPVS configmap: %v", err),
			))
		}

		if ret != nil {
			return ret
		}
	}

	// Delete the load balancer deployment.
	err = c.KubeClient.AppsV1().Deployments(lbDeployment.ObjectMeta.Namespace).Delete(context.TODO(), lbDeployment.ObjectMeta.Name, deleteOptions)
	if nil != err {
		return c.Recorder.LoadBalancerWarningEvent(
			lbDeployment, service, DeletingCloudLoadBalancerFailed,
			fmt.Sprintf("Failed to delete deployment: %v", err),
		)
	}

	klog.Infof("Load balancer %v deleted", lbLogName)
	return nil
}

// replicaSetHasDesiredReplicas returns a condition that will be true if and only if
// the desired replica count for a ReplicaSet's ReplicaSelector equals the Replicas count.
// NOTE(rtheis): This function is based on a similar function in kubernetes but
// updated to support a versioned client.
func replicaSetHasDesiredReplicas(clientset clientset.Interface, replicaSet *apps.ReplicaSet) wait.ConditionFunc {
	desiredGeneration := replicaSet.Generation
	return func() (bool, error) {
		rs, err := clientset.AppsV1().ReplicaSets(replicaSet.Namespace).Get(context.TODO(), replicaSet.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return rs.Status.ObservedGeneration >= desiredGeneration && rs.Status.Replicas == *(rs.Spec.Replicas), nil
	}
}

// Filter the services list to just contain the load balancers without defined load balancer class and nothing else.
func filterLoadBalancersFromServiceList(services *v1.ServiceList) {
	var lbItems []v1.Service
	for i := range services.Items {
		if services.Items[i].Spec.Type == v1.ServiceTypeLoadBalancer &&
			services.Items[i].Spec.LoadBalancerClass == nil {
			lbItems = append(lbItems, services.Items[i])
		}
	}
	services.Items = lbItems
}

// MonitorLoadBalancers monitors load balancer services to ensure that they
// are working properly. This is a cloud task run via ticker.
func MonitorLoadBalancers(c *Cloud, data map[string]string) {
	klog.Infof("Monitoring load balancers ...")

	// Monitor all load balancer services and generate a warning event for
	// each service that fails at least two consecutive monitors. A warning event
	// will also be generated to note that a service is restored after a failure.
	services, err := c.KubeClient.CoreV1().Services(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if nil != err {
		klog.Warningf("Failed to list load balancer services: %v", err)
		return
	}

	// Filtering out the services which type is not load blancer and also filtering out
	// the load blanacer services which has got defined load blancer class.
	// The ServiceList struct was modified in place so there is no returning value
	filterLoadBalancersFromServiceList(services)

	// Invoke VPC specific logic if this is a VPC cluster
	if c.isProviderVpc() {
		c.VpcMonitorLoadBalancers(services, data)
		return
	}

	// Verify each load balancer service that has a status with an IP
	// address set. If the load balancer has no such status then it is in
	// the process of being created, updated or deleted and doesn't need to
	// be monitored since those actions will do the appropriate error
	// handling and event generation.
	for i := range services.Items {
		if 0 != len(services.Items[i].Status.LoadBalancer.Ingress) &&
			0 != len(services.Items[i].Status.LoadBalancer.Ingress[0].IP) {

			lbName := GetCloudProviderLoadBalancerName(&services.Items[i])
			monitorData, isEventRequired := data[lbName]
			klog.V(2).Infof("Verifying load balancer %v with monitor data: %v", lbName, monitorData)

			// Get the load balancer deployment. This will ensure
			// that a deployment exists for the service and that
			// the deployment isn't "corrupted".
			lbDeployment, err := c.getLoadBalancerDeployment(lbName)
			if nil == lbDeployment || nil != err {
				errorMessage := fmt.Sprintf("Cloud load balancer deployment not found: %v", err)
				data[lbName] = errorMessage
				if isEventRequired {
					c.Recorder.LoadBalancerServiceWarningEvent(
						&services.Items[i],
						VerifyingCloudLoadBalancerFailed,
						errorMessage,
					)
				}
				continue
			}

			// Verify there is at least one available replica for
			// the deployment.
			if 1 > lbDeployment.Status.AvailableReplicas {
				errorMessage := "Cloud load balancer deployment not available"
				data[lbName] = errorMessage
				if isEventRequired {
					c.Recorder.LoadBalancerWarningEvent(
						lbDeployment, &services.Items[i],
						VerifyingCloudLoadBalancerFailed,
						errorMessage,
					)
				}
				continue
			}

			// Verify when the service source IP is set that there is a keepalived pod running on the same node as the resource
			// that is being loadbalanced
			ipvsEnabled := isFeatureEnabledDeployment(lbDeployment, lbFeatureIPVS)
			if services.Items[i].Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal && !ipvsEnabled {
				endpoints, _ := c.KubeClient.CoreV1().Endpoints(services.Items[i].Namespace).Get(context.TODO(), services.Items[i].Name, metav1.GetOptions{})
				if endpoints != nil {
					keepalivedPodOnTheWrongNode := c.checkIfKeepalivedPodShouldBeDeleted(endpoints, nil)
					if keepalivedPodOnTheWrongNode {
						errorMessage := fmt.Sprintf("Failed validation for loadbalancer service %v in namespace %v which enables the local "+
							"external traffic policy spec. Delete the pods in the loadbalancer deployment %v namespace %v to resolve the "+
							"problem.", services.Items[i].Name, services.Items[i].Namespace, lbDeployment.Name, lbDeployment.Namespace)

						data[lbName] = errorMessage
						if isEventRequired {
							c.Recorder.LoadBalancerWarningEvent(lbDeployment, &services.Items[i], VerifyingCloudLoadBalancerFailed, errorMessage)
						}
						continue
					}
				}
			}

			// The load balancer deployment has been recovered.
			if isEventRequired {
				c.Recorder.LoadBalancerNormalEvent(
					lbDeployment, &services.Items[i], CloudLoadBalancerNormalEvent,
					"Cloud load balancer deployment recovered",
				)
			}
			delete(data, lbName)
		}
	}
}

// Compare the Match Labels on the LB Deployment to the Selector on the Service to see if they are equal
func podAffinityMatchLabelAndServiceSelectorEqual(lbDeploymentPodAffinity *v1.PodAffinity, serviceSelector map[string]string) bool {

	lbPodAffinityTerm := lbDeploymentPodAffinity.RequiredDuringSchedulingIgnoredDuringExecution

	// Verify we have zero or one Pod Affinity Term.
	if lbPodAffinityTerm == nil || len(lbPodAffinityTerm) > 1 {
		return false
	}

	// Verify Label Selector is the same as the service Selector
	found := false
	for _, lbDeploymentPodAffinityTerm := range lbPodAffinityTerm {
		if reflect.DeepEqual(lbDeploymentPodAffinityTerm.LabelSelector.MatchLabels, serviceSelector) {
			found = true
		}
	}

	return found
}

func isFeatureEnabled(service *v1.Service, feature string) bool {
	enableFeaturesString := service.Annotations[ServiceAnnotationLoadBalancerCloudProviderEnableFeatures]
	if strings.TrimSpace(enableFeaturesString) != "" {
		featureList := strings.Split(enableFeaturesString, ",")
		for _, enabledFeature := range featureList {
			if strings.EqualFold(feature, enabledFeature) {
				return true
			}
		}
	}
	return false
}
func isFeatureEnabledDeployment(lbDeployment *apps.Deployment, feature string) bool {
	if 1 <= len(lbDeployment.Spec.Template.Spec.Containers) && lbDeployment.Spec.Template.Spec.Containers[0].Env != nil {
		for _, envVar := range lbDeployment.Spec.Template.Spec.Containers[0].Env {
			if envVar.Name == "FEATURES" {
				if strings.Contains(envVar.Value, feature) {
					return true
				}
			}
		}
	}
	return false
}

func isProviderVpc(provider string) bool {
	if provider == lbVpcClassicProvider || provider == lbVpcNextGenProvider {
		return true
	}
	return false
}

func getSchedulingAlgorithm(service *v1.Service) string {
	schedulingAlgorithmAnnotation := service.Annotations[ServiceAnnotationLoadBalancerCloudProviderIPVSSchedulingAlgorithm]
	if strings.TrimSpace(schedulingAlgorithmAnnotation) != "" {
		return strings.TrimSpace(schedulingAlgorithmAnnotation)
	}
	return ""
}

func getUnsupportedSchedulerMsg(badScheduler string) string {
	return fmt.Sprintf(lbUnsupportedScheduler, badScheduler, strings.Join(supportedIPVSSchedulerTypes, ", "))
}

func sliceContains(stringSlice []string, searchString string) bool {
	for _, value := range stringSlice {
		if value == searchString {
			return true
		}
	}
	return false
}

func isServiceConfigurationSupported(service *v1.Service) error {
	var hasTCP bool
	var hasUDP bool

	for _, port := range service.Spec.Ports {
		switch port.Protocol {
		case v1.ProtocolTCP:
			hasTCP = true
		case v1.ProtocolUDP:
			hasUDP = true
		default:
			return fmt.Errorf("%s protocol", port.Protocol)
		}

		if port.AppProtocol != nil {
			return fmt.Errorf("application protocol")
		}
	}

	if hasTCP && hasUDP {
		return fmt.Errorf("mixed protocol")
	}

	return nil
}

// NOTE(rtheis): This function is based on a similar function in kubernetes.
func waitForObservedDeployment(getDeploymentFunc func() (*apps.Deployment, error), desiredGeneration int64, interval, timeout time.Duration) error {
	return wait.PollImmediate(interval, timeout, func() (bool, error) {
		deployment, err := getDeploymentFunc()
		if err != nil {
			return false, err
		}
		return deployment.Status.ObservedGeneration >= desiredGeneration, nil
	})
}
