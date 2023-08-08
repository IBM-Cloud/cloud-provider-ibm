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

package ibm

import (
	"os"
	"reflect"
	"strings"
	"testing"

	cloudprovider "k8s.io/cloud-provider"
)

func TestCloud(t *testing.T) {
	c := &Cloud{}

	var clientBuilder cloudprovider.ControllerClientBuilder
	stop := make(chan struct{})
	c.Initialize(clientBuilder, stop)

	providerName := c.ProviderName()
	if "ibm" != providerName {
		t.Fatalf("Cloud provider name is not correct: %s", providerName)
	}

	hasClusterID := c.HasClusterID()
	if !hasClusterID {
		t.Fatalf("Cloud does not have cluster ID")
	}
}

func TestNewCloud(t *testing.T) {
	var err error
	var config *os.File
	var c cloudprovider.Interface

	// NOTE(cjschaef): Make sure environment variables used to access a Kubernetes cluster during testing are unset
	os.Unsetenv("KUBERNETES_SERVICE_HOST")
	os.Unsetenv("KUBERNETES_MASTER")

	// No cloud config
	c, err = NewCloud(nil)
	if nil != c || nil == err {
		t.Fatalf("Unexpected cloud created: %v, %v", c, err)
	}

	// Create cloud with various invalid configurations.
	// NOTE(rtheis): ibm-cloud-config-ccm-in-cluster.ini would normally be a
	// valid configuration if we could easily simulate an in cluster Kubernetes
	// config file.
	invalidCloudConfigFilenames := []string{
		"../test-fixtures/ibm-cloud-config-error.ini",
		"../test-fixtures/ibm-cloud-config-invalid.ini",
		"../test-fixtures/ibm-cloud-config-ccm-in-cluster.ini",
	}
	for _, invalidCloudConfigFilename := range invalidCloudConfigFilenames {
		config, err = os.Open(invalidCloudConfigFilename)
		if nil != err {
			t.Fatalf("Unexpected error opening cloud config file: %v", err)
		}
		defer config.Close()
		c, err = NewCloud(config)
		if nil != c || nil == err {
			t.Fatalf("Unexpected cloud created: %v, %v", c, err)
		}
	}

	// Create cloud with various valid configurations.
	validCloudConfigFilenames := []string{
		"../test-fixtures/ibm-cloud-config.ini",
		"../test-fixtures/ibm-cloud-config-ccm.ini",
		"../test-fixtures/ibm-cloud-config-ccm-classic.ini",
		"../test-fixtures/ibm-cloud-config-ccm-vpc.ini",
	}
	for _, validCloudConfigFilename := range validCloudConfigFilenames {
		config, err = os.Open(validCloudConfigFilename)
		if nil != err {
			t.Fatalf("Unexpected error opening cloud config file: %v", err)
		}
		defer config.Close()
		c, err = NewCloud(config)
		if nil == c || nil != err {
			t.Fatalf("Unexpected error creating cloud: %v, %v", c, err)
		}
		ibmCloud, ok := c.(*Cloud)
		if !ok {
			t.Fatalf("Unexpected cloud type created")
		}
		if 0 != strings.Compare("ibm", ibmCloud.Name) {
			t.Fatalf("Unexpected cloud name: %v", ibmCloud.Name)
		}
	}
}

func verifyCloudConfig(t *testing.T, cc *CloudConfig, ecc *CloudConfig) {
	if ecc.Global.Version != cc.Global.Version {
		t.Fatalf("Unexpected cloud config version: %v, expected: %v", cc.Global.Version, ecc.Global.Version)
	}

	if !reflect.DeepEqual(ecc.Kubernetes.ConfigFilePaths, cc.Kubernetes.ConfigFilePaths) {
		t.Fatalf("Unexpected cloud config k8s config file paths: %v, expected: %v", cc.Kubernetes.ConfigFilePaths, ecc.Kubernetes.ConfigFilePaths)
	}

	if !reflect.DeepEqual(ecc.Kubernetes.CalicoDatastore, cc.Kubernetes.CalicoDatastore) {
		t.Fatalf("Unexpected calico datastore type: %v, expected: %v", cc.Kubernetes.CalicoDatastore, ecc.Kubernetes.CalicoDatastore)
	}

	if !reflect.DeepEqual(ecc.LBDeployment, cc.LBDeployment) {
		t.Fatalf("Unexpected cloud config load balancer deployment: %v, expected: %v", cc.LBDeployment, ecc.LBDeployment)
	}

	if !reflect.DeepEqual(ecc.Prov, cc.Prov) {
		t.Fatalf("Unexpected cloud config provider: %v, expected: %v", cc.Prov, ecc.Prov)
	}
}

func TestGetCloudConfig(t *testing.T) {
	var config *os.File
	var config2 *os.File
	var configccm *os.File
	var cc *CloudConfig
	var ecc CloudConfig
	var err error

	// Build expected cloud config.
	ecc.Global.Version = "1.0.0"
	ecc.Kubernetes.ConfigFilePaths = []string{"../test-fixtures/kubernetes/k8s-config"}
	ecc.Kubernetes.CalicoDatastore = "KDD"
	ecc.LBDeployment.Image = "registry.ng.bluemix.net/armada-master/keepalived:1328"
	ecc.LBDeployment.Application = "keepalived"
	ecc.LBDeployment.VlanIPConfigMap = "ibm-cloud-provider-vlan-ip-config"
	ecc.Prov.ProviderID = "testaccount/testorg/testspace/testclusterID/testworkerID"

	// Verify cloud config from file.
	config, err = os.Open("../test-fixtures/ibm-cloud-config.ini")
	if nil != err {
		t.Fatalf("Failed to open cloud config: %v", err)
	}
	defer config.Close()
	cc, err = getCloudConfig(config)
	if nil != err {
		t.Fatalf("getCloudConfig failed for ibm-cloud-config.ini: %v", err)
	}
	verifyCloudConfig(t, cc, &ecc)

	config2, err = os.Open("../test-fixtures/ibm-cloud-config-2.ini")
	if nil != err {
		t.Fatalf("Failed to open cloud config 2: %v", err)
	}
	defer config2.Close()
	cc, err = getCloudConfig(config2)
	if nil != err {
		t.Fatalf("getCloudConfig failed for ibm-cloud-config-2.ini: %v", err)
	}
	ecc.Global.Version = "1.1.0"
	ecc.Kubernetes.CalicoDatastore = ""
	ecc.Prov.InternalIP = "10.190.31.186"
	ecc.Prov.ExternalIP = "169.61.102.244"
	ecc.Prov.Region = "testregion"
	ecc.Prov.Zone = "testzone"
	ecc.Prov.InstanceType = "testinstancetype"
	verifyCloudConfig(t, cc, &ecc)

	configccm, err = os.Open("../test-fixtures/ibm-cloud-config-ccm.ini")
	if nil != err {
		t.Fatalf("Failed to open cloud config ccm: %v", err)
	}
	defer configccm.Close()
	cc, err = getCloudConfig(configccm)
	if nil != err {
		t.Fatalf("getCloudConfig failed for ibm-cloud-config-ccm.ini: %v", err)
	}
	ecc = CloudConfig{}
	ecc.Global.Version = "1.1.0"
	ecc.Kubernetes.ConfigFilePaths = []string{"../test-fixtures/kubernetes/k8s-config"}
	ecc.LBDeployment.Image = "registry.ng.bluemix.net/armada-master/keepalived:1328"
	ecc.LBDeployment.Application = "keepalived"
	ecc.LBDeployment.VlanIPConfigMap = "ibm-cloud-provider-vlan-ip-config"
	ecc.Prov.ClusterID = "testClusterID"
	ecc.Prov.AccountID = "testAccountID"
	verifyCloudConfig(t, cc, &ecc)

	configccm, err = os.Open("../test-fixtures/ibm-cloud-config-ccm-classic.ini")
	if nil != err {
		t.Fatalf("Failed to open cloud config ccm: %v", err)
	}
	defer configccm.Close()
	cc, err = getCloudConfig(configccm)
	if nil != err {
		t.Fatalf("getCloudConfig failed for ibm-cloud-config-ccm-classic.ini: %v", err)
	}
	// Build off previous expected configuration with select overrides.
	ecc.Kubernetes.CalicoDatastore = "KDD"
	verifyCloudConfig(t, cc, &ecc)

	configccm, err = os.Open("../test-fixtures/ibm-cloud-config-ccm-vpc.ini")
	if nil != err {
		t.Fatalf("Failed to open cloud config ccm: %v", err)
	}
	defer configccm.Close()
	cc, err = getCloudConfig(configccm)
	if nil != err {
		t.Fatalf("getCloudConfig failed for ibm-cloud-config-vpc.ini: %v", err)
	}
	// Build off previous expected configuration with select overrides.
	ecc.Kubernetes.CalicoDatastore = ""
	ecc.LBDeployment.Image = ""
	ecc.LBDeployment.Application = ""
	ecc.LBDeployment.VlanIPConfigMap = ""
	ecc.Prov.ProviderType = "g2"
	ecc.Prov.G2WorkerServiceAccountID = "testServiceAccountID"
	verifyCloudConfig(t, cc, &ecc)

	configccm, err = os.Open("../test-fixtures/ibm-cloud-config-ccm-in-cluster.ini")
	if nil != err {
		t.Fatalf("Failed to open cloud config ccm: %v", err)
	}
	defer configccm.Close()
	cc, err = getCloudConfig(configccm)
	if nil != err {
		t.Fatalf("getCloudConfig failed for ibm-cloud-config-ccm-in-cluster.ini: %v", err)
	}
	// Build off previous expected configuration with select overrides.
	ecc.Kubernetes.ConfigFilePaths = nil
	ecc.Prov.G2EndpointOverride = "https://us-south.iaas.cloud.ibm.com"
	ecc.Prov.IamEndpointOverride = "https://iam.cloud.ibm.com"
	ecc.Prov.RmEndpointOverride = "https://resource-controller.cloud.ibm.com"
	verifyCloudConfig(t, cc, &ecc)

	// Verify nil cloud config.
	cc, err = getCloudConfig(nil)
	if nil == err {
		t.Fatalf("getCloudConfig successful for nil cloud config: %v", cc)
	}

	// Verify invalid configurations.
	invalidCloudConfigFilenames := []string{
		"../test-fixtures/ibm-cloud-config-error.ini",
		"../test-fixtures/ibm-cloud-config-invalid.ini",
	}
	for _, invalidCloudConfigFilename := range invalidCloudConfigFilenames {
		invalidCloudConfigFile, err := os.Open(invalidCloudConfigFilename)
		if nil != err {
			t.Fatalf("Failed to open cloud config: %v", err)
		}
		defer invalidCloudConfigFile.Close()
		cc, err = getCloudConfig(invalidCloudConfigFile)
		if nil == err {
			t.Fatalf("getCloudConfig successful for %v: %v", invalidCloudConfigFile, cc)
		}
	}
}

func TestGetK8SConfig(t *testing.T) {
	var err error
	_, err = getK8SConfig([]string{})
	if nil == err {
		t.Fatalf("Unexpected k8s config found from empty list")
	}

	_, err = getK8SConfig([]string{"../test-fixtures/kubernetes/doesntexist"})
	if nil == err {
		t.Fatalf("Unexpected k8s config found")
	}

	_, err = getK8SConfig([]string{"../test-fixtures/kubernetes/k8s-config"})
	if nil != err {
		t.Fatalf("Failed to get k8s config: %v", err)
	}
}
