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
	"errors"
	"os"
	"strings"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"

	"k8s.io/klog/v2"
)

// ibmCloudClient makes call to IBM Cloud APIs
type vpcClient struct {
	provider Provider
	sdk      *vpcv1.VpcV1
}

// newVpcSdkClient initializes a new sdk client and can be overridden by testing
var newVpcSdkClient = func(provider Provider) (*vpcv1.VpcV1, error) {
	// check id used to allocate worker nodes
	if provider.AccountID != provider.G2WorkerServiceAccountID {
		return nil, errors.New("Cluster nodes allocated under different account")
	}

	// read VPC credentials from mounted secret
	credential, err := readCredential(provider)
	if err != nil {
		return nil, err
	}

	// authenticator needs api key
	authenticator := &core.IamAuthenticator{
		ApiKey: credential, // pragma: allowlist secret
	}

	// Virtual Private Cloud (VPC) API
	sdk, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{
		Authenticator: authenticator,
	})
	if err != nil {
		return nil, err
	}

	// Get Region and Set Service URL
	region, _, err := sdk.GetRegion(sdk.NewGetRegionOptions(provider.Region))
	if err != nil {
		return nil, err
	}

	// Set the Service URL
	err = sdk.SetServiceURL(*region.Endpoint + "/v1")
	if err != nil {
		return nil, err
	}

	return sdk, nil
}

// newVpcClient initializes a new validated VpcClient
func newVpcClient(provider Provider) (*vpcClient, error) {
	// create vpc sdk service
	sdk, err := newVpcSdkClient(provider)
	if err != nil {
		return nil, err
	}

	return &vpcClient{
		provider: provider,
		sdk:      sdk,
	}, nil
}

func readCredential(provider Provider) (string, error) {
	// No credentials file provided
	if provider.G2Credentials == "" {
		return "", errors.New("VPC credentials file not provided")
	}

	// read credential
	data, err := os.ReadFile(provider.G2Credentials)
	if err != nil {
		return "", err
	}

	credential := string(data)
	credential = strings.TrimSpace(credential)
	return credential, nil
}

func (vpc *vpcClient) populateNodeMetadata(nodeName string, node *NodeMetadata) error {
	// Initialize New List Instances Options
	listInstOptions := vpc.sdk.NewListInstancesOptions()
	listInstOptions.SetName(nodeName)
	listInstOptions.SetVPCName(vpc.provider.G2VpcName)

	// Get Instances list
	instances, _, err := vpc.sdk.ListInstances(listInstOptions)
	if err != nil {
		return err
	}

	// Check if instance is not nil
	if instances == nil {
		return errors.New("Could not retrieve a list of instances: name=" + nodeName + " url=" + vpc.sdk.GetServiceURL())
	}

	// Found the instance
	if len(instances.Instances) == 1 {
		node.InternalIP = *instances.Instances[0].PrimaryNetworkInterface.PrimaryIP.Address
		klog.Infof("***** InternalIP " + node.InternalIP)

		node.WorkerID = *instances.Instances[0].ID
		klog.Infof("***** WorkerId " + node.WorkerID)

		node.InstanceType = *instances.Instances[0].Profile.Name
		klog.Infof("***** InstanceType " + node.InstanceType)

		node.FailureDomain = *instances.Instances[0].Zone.Name
		klog.Infof("***** FailureDomain " + node.FailureDomain)

		node.Region = vpc.provider.Region
		klog.Infof("***** Region " + node.Region)

		// Success
		return nil
	}

	if len(instances.Instances) == 0 {
		// Not found
		return errors.New("Instance not found: name=" + nodeName + " url=" + vpc.sdk.GetServiceURL())
	}

	// Too many entries
	return errors.New("More than one instance entry returned: name=" + nodeName + " url=" + vpc.sdk.GetServiceURL())
}
