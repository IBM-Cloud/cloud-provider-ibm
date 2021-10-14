/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2021 All Rights Reserved.
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

// LoadBalancerDeployment is the load balancer deployment data.
type LoadBalancerDeployment struct {
	// Required: Name of the image to use for the deployment.
	Image string `gcfg:"image"`
	// Required: Name of the application to use as a label for the deployment.
	Application string `gcfg:"application"`
	// Required: Name of the VLAN IP config map used to determine the
	// available cloud provider IPs for the deployment.
	VlanIPConfigMap string `gcfg:"vlan-ip-config-map"`
}

// Provider holds information from the cloud provider node (i.e. instance).
type Provider struct {
	// Optional: Cloud provider ID for the node Only set in worker.
	ProviderID string `gcfg:"providerID"`
	// Optional: Internal IP of the node. Only set in worker.
	InternalIP string `gcfg:"internalIP"`
	// Optional: External IP of the node. Only set in worker.
	ExternalIP string `gcfg:"externalIP"`
	// Optional: Region of the node. Only set in worker.
	Region string `gcfg:"region"`
	// Optional: Zone of the node. Only set in worker.
	Zone string `gcfg:"zone"`
	// Optional: Instance Type of the node. Only set in worker.
	InstanceType string `gcfg:"instanceType"`
	// Optional: Cluster ID of the master. Only set in controller manager.
	ClusterID string `gcfg:"clusterID"`
	// Optional: Account ID of the master. Only set in controller manager.
	AccountID string `gcfg:"accountID"`
	// Optional: Provider type of the cloud provider
	ProviderType string `gcfg:"cluster-default-provider"`
	// Optional: Service account ID used to allocate worker nodes in VPC Gen2 environment
	G2WorkerServiceAccountID string `gcfg:"g2workerServiceAccountID"`
	// Optional: VPC Gen2 name
	G2VpcName string `gcfg:"g2VpcName"`
	// Optional: File containing VPC Gen2 credentials
	G2Credentials string `gcfg:"g2Credentials"`
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
		// Required: The kubernetes config file paths. The first file
		// found will be used.
		ConfigFilePaths []string `gcfg:"config-file"`
		CalicoDatastore string   `gcfg:"calico-datastore"`
	}
	// [load-balancer-deployment] section
	LBDeployment LoadBalancerDeployment `gcfg:"load-balancer-deployment"`
	// [provider] section
	Prov Provider `gcfg:"provider"`
}

// Cloud is the ibm cloud provider implementation.
type Cloud struct {
	Name       string
	KubeClient clientset.Interface
	Config     *CloudConfig
	Recorder   *CloudEventRecorder
	CloudTasks map[string]*CloudTask
	Metadata   *MetadataService // will be nil in kubelet
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

	// endpointInformer is not needed for VPC Gen2
	if !isProviderVpc(c.Config.Prov.ProviderType) {
		endpointInformer := informerFactory.Core().V1().Endpoints().Informer()
		endpointInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			UpdateFunc: c.handleEndpointUpdate,
		})
	}

	nodeInformer := informerFactory.Core().V1().Nodes().Informer()
	nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.handleNodeDelete,
	})
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

	return &c, nil
}

func init() {
	cloudprovider.RegisterCloudProvider(ProviderName, func(config io.Reader) (cloudprovider.Interface, error) {
		klog.Infof("RegisterCloudProvider(%v, %v, %v)", ProviderName, config, os.Args)
		return NewCloud(config)
	})
}
