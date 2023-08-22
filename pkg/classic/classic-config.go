/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2017, 2023 All Rights Reserved.
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

package classic

import (
	clientset "k8s.io/client-go/kubernetes"
)

// LoadBalancerDeployment is the load balancer deployment data for classic
// load balancers. All fields are required when running on classic
// infrastructure.
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
		// classic infrastructure
		CalicoDatastore string `gcfg:"calico-datastore"`
	}
	// [load-balancer-deployment] section
	LBDeployment LoadBalancerDeployment `gcfg:"load-balancer-deployment"`
	// [provider] section
	Prov Provider `gcfg:"provider"`
}

// Cloud is the ibm cloud provider implementation.
type Cloud struct {
	KubeClient clientset.Interface
	Config     *CloudConfig
	Recorder   *CloudEventRecorder
}
