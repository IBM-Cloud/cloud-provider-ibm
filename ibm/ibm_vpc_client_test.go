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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"

	"github.com/stretchr/testify/assert"
)

func TestReadCredentials(t *testing.T) {
	provider := Provider{G2Credentials: "../test-fixtures/ibmcloud_api_key"} // pragma: allowlist secret

	value, err := readCredential(provider)
	if err != nil {
		t.Fatalf("Got an error for readCredential: %v", err)
	}

	assert.Equal(t, "the_api_key", value, "Unexpected credentials")
}

func TestCredentialsNotProvided(t *testing.T) {
	provider := Provider{}

	value, err := readCredential(provider)
	if err != nil {
		assert.Equal(t, "", value)
		assert.Contains(t, err.Error(), "VPC credentials file not provided")
	} else {
		t.Fatalf("Expected an error for readCredential")
	}
}

func TestCredentialsBad(t *testing.T) {
	provider := Provider{G2Credentials: "does-not-exist"} // pragma: allowlist secret

	value, err := readCredential(provider)
	if err != nil {
		assert.Equal(t, "", value)
		assert.Contains(t, err.Error(), "no such file or directory")
	} else {
		t.Fatalf("Expected an error for readCredential")
	}
}

func TestPopulateNodeMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		fmt.Fprintf(res, `{"first":{"href":"https://us-south.iaas.cloud.ibm.com/v1/instances?limit=50"},"instances":[{"bandwidth":4000,"boot_volume_attachment":{"device":{"id":"a8a15363-a6f7-4f01-af60-715e85b28141"},"href":"https://us-south.iaas.cloud.ibm.com/v1/instances/eb1b7391-2ca2-4ab5-84a8-b92157a633b0/volume_attachments/7389-a8a15363-a6f7-4f01-af60-715e85b28141","id":"a8a15363-a6f7-4f01-af60-715e85b28141","name":"my-boot-volume-attachment","volume":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/volumes/49c5d61b-41e7-4c01-9b7a-1a97366c6916","id":"49c5d61b-41e7-4c01-9b7a-1a97366c6916","name":"my-boot-volume"}},"created_at":"2020-03-26T16:11:57Z","crn":"crn:[...]","dedicated_host":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/dedicated_hosts/0787-8c2a09be-ee18-4af2-8ef4-6a6060732221","id":"0787-8c2a09be-ee18-4af2-8ef4-6a6060732221","name":"test-new","resource_type":"dedicated_host"},"disks":[],"href":"https://us-south.iaas.cloud.ibm.com/v1/instances/eb1b7391-2ca2-4ab5-84a8-b92157a633b0","id":"eb1b7391-2ca2-4ab5-84a8-b92157a633b0","image":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/images/9aaf3bcb-dcd7-4de7-bb60-24e39ff9d366","id":"9aaf3bcb-dcd7-4de7-bb60-24e39ff9d366","name":"my-image"},"memory":8,"name":"my-instance","network_interfaces":[{"href":"https://us-south.iaas.cloud.ibm.com/v1/instances/e402fa1b-96f6-4aa2-a8d7-703aac843651/network_interfaces/7ca88dfb-8962-469d-b1de-1dd56f4c3275","id":"7ca88dfb-8962-469d-b1de-1dd56f4c3275","name":"my-network-interface","primary_ip": {"address": "192.168.3.4", "deleted": {"more_info": "https://cloud.ibm.com/apidocs/vpc#deleted-resources"}, "href": "https://us-south.iaas.cloud.ibm.com/v1/subnets/7ec86020-1c6e-4889-b3f0-a15f2e50f87e/reserved_ips/6d353a0f-aeb1-4ae1-832e-1110d10981bb", "id": "6d353a0f-aeb1-4ae1-832e-1110d10981bb", "name": "my-reserved-ip", "resource_type": "subnet_reserved_ip"}, "resource_type":"network_interface","subnet":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/subnets/7389-bea6a632-5e13-42a4-b4b8-31dc877abfe4","id":"bea6a632-5e13-42a4-b4b8-31dc877abfe4","name":"my-subnet"}}],"placement_target":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/dedicated_hosts/0787-8c2a09be-ee18-4af2-8ef4-6a6060732221","id":"0787-8c2a09be-ee18-4af2-8ef4-6a6060732221","name":"test-new","resource_type":"dedicated_host"},"primary_network_interface":{"href":"https://us-south.iaas.cloud.ibm.com/v1/instances/e402fa1b-96f6-4aa2-a8d7-703aac843651/network_interfaces/7ca88dfb-8962-469d-b1de-1dd56f4c3275","id":"7ca88dfb-8962-469d-b1de-1dd56f4c3275","name":"my-network-interface","primary_ip": {"address": "10.0.0.32", "deleted": {"more_info": "https://cloud.ibm.com/apidocs/vpc#deleted-resources"}, "href": "https://us-south.iaas.cloud.ibm.com/v1/subnets/7ec86020-1c6e-4889-b3f0-a15f2e50f87e/reserved_ips/6d353a0f-aeb1-4ae1-832e-1110d10981bb", "id": "6d353a0f-aeb1-4ae1-832e-1110d10981bb", "name": "my-reserved-ip", "resource_type": "subnet_reserved_ip"}, "resource_type":"network_interface","subnet":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/subnets/bea6a632-5e13-42a4-b4b8-31dc877abfe4","id":"bea6a632-5e13-42a4-b4b8-31dc877abfe4","name":"my-subnet"}},"profile":{"href":"https://us-south.iaas.cloud.ibm.com/v1/instance/profiles/bx2-2x8","name":"bx2-2x8"},"resource_group":{"href":"https://resource-controller.cloud.ibm.com/v2/resource_groups/4bbce614c13444cd8fc5e7e878ef8e21","id":"4bbce614c13444cd8fc5e7e878ef8e21","name":"Default"},"startable":true,"status":"running","status_reasons":[],"total_network_bandwidth":3000,"total_volume_bandwidth":1000,"vcpu":{"architecture":"amd64","count":2},"volume_attachments":[{"device":{"id":"a8a15363-a6f7-4f01-af60-715e85b28141"},"href":"https://us-south.iaas.cloud.ibm.com/v1/instances/e402fa1b-96f6-4aa2-a8d7-703aac843651/volume_attachments/7389-a8a15363-a6f7-4f01-af60-715e85b28141","id":"a8a15363-a6f7-4f01-af60-715e85b28141","name":"my-boot-volume-attachment","volume":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/volumes/49c5d61b-41e7-4c01-9b7a-1a97366c6916","id":"49c5d61b-41e7-4c01-9b7a-1a97366c6916","name":"my-boot-volume"}},{"device":{"id":"e77125cb-4df0-4988-a878-531ae0ae0b70"},"href":"https://us-south.iaas.cloud.ibm.com/v1/instances/e402fa1b-96f6-4aa2-a8d7-703aac843651/volume_attachments/7389-e77125cb-4df0-4988-a878-531ae0ae0b70","id":"e77125cb-4df0-4988-a878-531ae0ae0b70","name":"my-volume-attachment-1","volume":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/volumes/2cc091f5-4d46-48f3-99b7-3527ae3f4392","id":"2cc091f5-4d46-48f3-99b7-3527ae3f4392","name":"my-data-volume"}}],"vpc":{"crn":"crn:[...]","href":"https://us-south.iaas.cloud.ibm.com/v1/vpcs/f0aae929-7047-46d1-92e1-9102b07a7f6f","id":"f0aae929-7047-46d1-92e1-9102b07a7f6f","name":"my-vpc"},"zone":{"href":"https://us-south.iaas.cloud.ibm.com/v1/regions/us-south/zones/us-south-1","name":"us-south-1"}}],"limit":50,"total_count":1}`) // pragma: allowlist secret
	}))
	defer server.Close()

	origNewVpcSdkClient := newVpcSdkClient
	newVpcSdkClient = func(provider Provider) (*vpcv1.VpcV1, error) {
		sdk, _ := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{
			URL:           server.URL,
			Authenticator: &core.NoAuthAuthenticator{}})
		return sdk, nil
	}
	defer func() { newVpcSdkClient = origNewVpcSdkClient }()

	provider := Provider{
		G2VpcName:     "my-vpc",
		Region:        "us-south",
		G2Credentials: "../test-fixtures/ibmcloud_api_key", // pragma: allowlist secret
	}
	vpcClient, err := newVpcClient(provider)
	if err != nil {
		t.Fatalf("Got an error from newVpcClient: %v", err)
	}

	assert.NotNil(t, vpcClient)

	name := "mynode"
	newNode := NodeMetadata{}
	err = vpcClient.populateNodeMetadata(name, &newNode)
	if err != nil {
		t.Fatalf("Got an error from populateNodeMetadata: %v", err)
	}

	assert.Equal(t, "10.0.0.32", newNode.InternalIP, "Unexpected InternalIP")
	assert.Equal(t, "eb1b7391-2ca2-4ab5-84a8-b92157a633b0", newNode.WorkerID, "Unexpected WorkerID")
	assert.Equal(t, "bx2-2x8", newNode.InstanceType, "Unexpected InstanceType")
	assert.Equal(t, "us-south-1", newNode.FailureDomain, "Unexpected FailureDomain")
	assert.Equal(t, "us-south", newNode.Region, "Unexpected Region")
}
