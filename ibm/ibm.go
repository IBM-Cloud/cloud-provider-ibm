/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2024 All Rights Reserved.
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
	"fmt"
	"io"
	"os"

	gcfg "gopkg.in/gcfg.v1"
	"k8s.io/klog/v2"

	"cloud.ibm.com/cloud-provider-ibm/pkg/classic"
	"cloud.ibm.com/cloud-provider-ibm/pkg/vpcctl"

	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
)

const (
	ProviderName = "ibm"
)

// LoadBalancerDeployment is the load balancer deployment data for classic
// load balancers. All fields are required when running on classic
// infrastructure, otherwise this section may be omitted and will be ignored
// for VPC infrastructure.
type LoadBalancerDeployment struct {
	// Name of the image to use for the load balancer deployment.
	Image string `gcfg:"image"`
	// Name of the application to use as a label for the load balancer deployment.
	Application string `gcfg:"application"`
	// Name of the VLAN IP config map in the kube-system or ibm-system namespace
	// that is used to determine the available cloud provider IPs for the
	// load balancer deployment.
	VlanIPConfigMap string `gcfg:"vlan-ip-config-map"`
}

// Provider holds information from the cloud provider.
// TODO(rtheis): Remove legacy in tree cloud provider implementation.
type Provider struct {
	// Unsupported: Cloud provider ID for the node. Only used when running the
	// legacy in tree cloud provider implementation, ignored otherwise.
	ProviderID string `gcfg:"providerID"`
	// Unsupported: Internal IP of the node. Only used when running the
	// legacy in tree cloud provider implementation, ignored otherwise.
	InternalIP string `gcfg:"internalIP"`
	// Unsupported: External IP of the node. Only used when running the
	// legacy in tree cloud provider implementation, ignored otherwise.
	ExternalIP string `gcfg:"externalIP"`
	// NOTE(rtheis): This field has multiple usages.
	// Region of the cluster. Required when configured to get node
	// data from VPC.
	// Unsupported: Region of the node. Only used when running the
	// legacy in tree cloud provider implementation.
	Region string `gcfg:"region"`
	// Unsupported: Zone of the node. Only used when running the
	// legacy in tree cloud provider implementation, ignored otherwise.
	Zone string `gcfg:"zone"`
	// Unsupported: Instance Type of the node. Only used when running the
	// legacy in tree cloud provider implementation, ignored otherwise.
	InstanceType string `gcfg:"instanceType"`
	// Required: Cluster ID of the cluster.
	ClusterID string `gcfg:"clusterID"`
	// Required: Account ID that owns the cluster.
	AccountID string `gcfg:"accountID"`
	// Required: Provider type of the cloud provider. Set to "g2" when running
	// on VPC infrastructure. All other values (including being unset)
	// yield the default, classic infrastructure.
	// TODO(rtheis): Remove support for "gc" provider type.
	ProviderType string `gcfg:"cluster-default-provider"`
	// Required for VPC: Service account ID used to allocate VPC infrastructure.
	G2WorkerServiceAccountID string `gcfg:"g2workerServiceAccountID"`
	// VPC name. Required when configured to get node data from VPC.
	G2VpcName string `gcfg:"g2VpcName"`
	// File containing VPC credentials. Required when configured to get node
	// data from VPC.
	G2Credentials string `gcfg:"g2Credentials"`
	// Resource group name. Required when configured to get node
	// data from VPC.
	G2ResourceGroupName string `gcfg:"g2ResourceGroupName"`
	// List of VPC subnet names. Required when configured to get node
	// data from VPC.
	G2VpcSubnetNames string `gcfg:"g2VpcSubnetNames"`
	// Optional: VPC RIaaS endpoint override URL
	G2EndpointOverride string `gcfg:"g2EndpointOverride"`
	// Optional: IAM endpoint override URL
	IamEndpointOverride string `gcfg:"iamEndpointOverride"`
	// Optional: Resource Manager endpoint override URL
	RmEndpointOverride string `gcfg:"rmEndpointOverride"`
}

// CloudConfig is the ibm cloud provider config data.
type CloudConfig struct {
	// [global] section
	Global struct {
		// Required: Version of the cloud config. Currently only versions
		// 1.0.0 and 1.1.0 are supported.
		Version string `gcfg:"version"`
	}
	// [kubernetes] section
	Kubernetes struct {
		// The Kubernetes config file paths. The first file found will be used.
		// If not specified, then the in cluster config will be used. Using
		// an in cluster config is not support for classic infrastructure
		// since Calico does not support such configurations.
		ConfigFilePaths []string `gcfg:"config-file"`
		// The Calico datastore type: "ETCD" or "KDD". Required when running on
		// classic infrastructure, otherwise this may be omitted and will be
		// ignored for VPC infrastructure.
		CalicoDatastore string `gcfg:"calico-datastore"`
		// If set to true, all new nodes will get the condition NetworkUnavailable
		// during node registration
		SetNetworkUnavailable bool `gcfg:"set-network-unavailable,false"`
		// The CNI being used by the cluster: "Calico" or "OVNKubernetes".
		CniProvider string `gcfg:"cniProvider"`
	}
	// [load-balancer-deployment] section
	LBDeployment LoadBalancerDeployment `gcfg:"load-balancer-deployment"`
	// [provider] section
	Prov Provider `gcfg:"provider"`
}

// Cloud is the ibm cloud provider implementation.
type Cloud struct {
	Name         string
	KubeClient   clientset.Interface
	Config       *CloudConfig
	Recorder     *CloudEventRecorder
	CloudTasks   map[string]*CloudTask
	Metadata     *MetadataService // will be nil in kubelet
	ClassicCloud *classic.Cloud   // Classic load balancer support
}

// Initialize provides the cloud with a kubernetes client builder and may spawn goroutines
// to perform housekeeping or run custom controllers specific to the cloud provider.
// Any tasks started here should be cleaned up when the stop channel closes.
func (c *Cloud) Initialize(clientBuilder cloudprovider.ControllerClientBuilder, stop <-chan struct{}) {
}

// ProviderName returns the cloud provider ID.
func (c *Cloud) ProviderName() string {
	return ProviderName
}

// HasClusterID returns true if a ClusterID is required and set
func (c *Cloud) HasClusterID() bool {
	return true
}

// SetInformers initializes any informers when the cloud provider starts
func (c *Cloud) SetInformers(informerFactory informers.SharedInformerFactory) {
	klog.Infof("Initializing Informers")

	// endpointInformer is not needed for VPC
	if !c.isProviderVpc() {
		c.ClassicCloud.SetInformers(informerFactory)
	}

	nodeInformer := informerFactory.Core().V1().Nodes().Informer()
	// #nosec G104 Error is ignored for now
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.handleNodeDelete,
	})

	if c.isProviderVpc() {
		vpcctl.SetInformers(informerFactory)
		// Configure watch on the cloud credential if it was listed in the config
		if c.Config.Prov.G2Credentials != "" {
			err := c.WatchCloudCredential()
			if err != nil {
				klog.Errorf("Failed to set up watch on the cloud credential: %v", err)
			}
		}
	}
}

// getK8SConfig returns the k8s config for the first k8s config file found.
func getK8SConfig(k8sConfigFilePaths []string) (*restclient.Config, error) {
	// Build the k8s config.
	var config *restclient.Config
	var err error
	for _, k8sConfigFilePath := range k8sConfigFilePaths {
		config, err = clientcmd.BuildConfigFromFlags("", k8sConfigFilePath)
		if nil == err {
			break
		} else {
			klog.Infof("Failed to build Kubernetes cloud configuration from %v: %v", k8sConfigFilePath, err)
		}
	}
	if nil == config {
		return nil, fmt.Errorf("Failed to build Kubernetes cloud configuration")
	}
	return config, nil
}

// getCloudConfig returns the cloud config
func getCloudConfig(config io.Reader) (*CloudConfig, error) {
	var cloudConfig CloudConfig

	if nil != config {
		err := gcfg.FatalOnly(gcfg.ReadInto(&cloudConfig, config))
		if nil != err {
			return nil, fmt.Errorf("Failed to read cloud config: %v", err)
		}
		// Validate that the cloud config version is supported.
		// Other config data will be validated when used.
		if "1.0.0" != cloudConfig.Global.Version && "1.1.0" != cloudConfig.Global.Version {
			return nil, fmt.Errorf("Cloud config version not valid: %v", cloudConfig.Global.Version)
		}
	} else {
		return nil, fmt.Errorf("Cloud config required but none specified")
	}

	return &cloudConfig, nil
}

// NewCloud creates a new instance of Cloud.
func NewCloud(config io.Reader) (cloudprovider.Interface, error) {
	var cloudConfig *CloudConfig
	var k8sConfig *restclient.Config
	var k8sClient *clientset.Clientset
	var cloudMetadata *MetadataService
	var err error

	// Get the cloud config.
	cloudConfig, err = getCloudConfig(config)
	if nil != err {
		return nil, err
	}

	// Get the k8s config.
	// Use in cluster config if no config file paths were provided.
	if 0 == len(cloudConfig.Kubernetes.ConfigFilePaths) {
		cloudConfig.Kubernetes.ConfigFilePaths = append(cloudConfig.Kubernetes.ConfigFilePaths, "")
	}
	k8sConfig, err = getK8SConfig(cloudConfig.Kubernetes.ConfigFilePaths)
	if nil != err {
		return nil, err
	}

	// Create the k8s client.
	k8sClient, err = clientset.NewForConfig(k8sConfig)
	if nil != err {
		return nil, fmt.Errorf("Failed to create Kubernetes client: %v", err)
	}

	// Create the metadataservice
	if cloudConfig.Prov.AccountID != "" {
		cloudMetadata = NewMetadataService(&cloudConfig.Prov, k8sClient)
	} else {
		cloudMetadata = nil
	}

	// Create the cloud provider instance.
	c := Cloud{
		Name:       ProviderName,
		KubeClient: k8sClient,
		Config:     cloudConfig,
		Recorder:   NewCloudEventRecorder(ProviderName, k8sClient),
		CloudTasks: map[string]*CloudTask{},
		Metadata:   cloudMetadata,
	}

	// Attempt to initialize the VPC logic (if configured)
	if c.isProviderVpc() {
		klog.Infof("Initialize VPC with cloud config: %+v", cloudConfig.Prov)
		_, err := c.InitCloudVpc(shouldPrivateEndpointBeEnabled())
		if err != nil {
			errString := fmt.Sprintf("Failed initializing VPC: %v", err)
			klog.Warningf(errString)
		}
	} else {
		// Initialize the classic logic
		classicConfig := &classic.CloudConfig{
			Application:     c.Config.LBDeployment.Application,
			CalicoDatastore: c.Config.Kubernetes.CalicoDatastore,
			ConfigFilePath:  c.Config.Kubernetes.ConfigFilePaths[0],
			Image:           c.Config.LBDeployment.Image,
			VlanIPConfigMap: c.Config.LBDeployment.VlanIPConfigMap,
		}
		c.ClassicCloud = classic.NewCloud(c.KubeClient, classicConfig, c.Recorder.Recorder)
	}
	return &c, nil
}

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		klog.Infof("RegisterCloudProvider(%v, %v, %v)", ProviderName, config, os.Args)
		return NewCloud(config)
	})
}
