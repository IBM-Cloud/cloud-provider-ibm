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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IBM/go-sdk-core/v5/core"
	sdk "github.com/IBM/vpc-go-sdk/vpcv1"
	"github.com/stretchr/testify/assert"
)

func newNoAuthTestVpcSdkGen2(server string) *VpcSdkGen2 {
	// Create the VPC client and SDK interface
	client, _ := sdk.NewVpcV1(&sdk.VpcV1Options{
		URL:           server,
		Authenticator: &core.NoAuthAuthenticator{}})
	return &VpcSdkGen2{Client: client, Config: &ConfigVpc{}}
}

func TestVpcSdkGen2_CreateLoadBalancer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(201)
		fmt.Fprintf(res, `{"created_at": "2019-01-01T12:00:00", "crn": "crn:v1:bluemix:public:is:us-south:a/123456::load-balancer:dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "hostname": "myloadbalancer-123456-us-south-1.lb.bluemix.net", "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "id": "dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "is_public": true, "listeners": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/listeners/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004"}], "name": "my-load-balancer", "operating_status": "offline", "pools": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "name": "my-load-balancer-pool"}], "private_ips": [{"address": "192.168.3.4"}], "provisioning_status": "active", "public_ips": [{"address": "192.168.3.4"}], "resource_group": {"href": "https://resource-controller.cloud.ibm.com/v2/resource_groups/fee82deba12e4c0fb69c3b09d1f12345", "id": "fee82deba12e4c0fb69c3b09d1f12345", "name": "my-resource-group"}, "subnets": [{"crn": "crn:v1:bluemix:public:is:us-south-1:a/123456::subnet:7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "href": "https://us-south.iaas.cloud.ibm.com/v1/subnets/7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "id": "7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "name": "my-subnet"}]}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Invalid pool name
	options := newServiceOptions()
	options.healthCheckNodePort = 36963
	lb, err := v.CreateLoadBalancer("lbName", []string{"192.168.1.1"}, []string{"poolName"}, []string{"subnetID"}, options)
	assert.Nil(t, lb)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid pool name,")

	// Success
	nodes := []string{"192.168.1.1"}
	pools := []string{"tcp-80-30303"}
	subnets := []string{"subnetID"}
	lb, err = v.CreateLoadBalancer("lbName", nodes, pools, subnets, options)
	assert.NotNil(t, lb)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_CreateLoadBalancerListener(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(201)
		fmt.Fprintf(res, `{"certificate_instance": {"crn": "crn:v1:bluemix:public:cloudcerts:us-south:a/123456:b8866ea4-b8df-467e-801a-da1db7e020bf:certificate:78ff9c4c97d013fb2a95b21dddde7758"}, "connection_limit": 2000, "created_at": "2019-01-01T12:00:00", "default_pool": {"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "name": "my-load-balancer-pool"}, "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/listeners/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "policies": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/listeners/70294e14-4e61-11e8-bcf4-0242ac110004/policies/f3187486-7b27-4c79-990c-47d33c0e2278", "id": "70294e14-4e61-11e8-bcf4-0242ac110004"}], "port": 443, "protocol": "http", "provisioning_status": "active"}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Invalid pool name
	listener, err := v.CreateLoadBalancerListener("lbID", "poolName", "poolID")
	assert.Nil(t, listener)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid pool name")

	// Success
	listener, err = v.CreateLoadBalancerListener("lbID", "tcp-80-30123", "poolID")
	assert.NotNil(t, listener)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_CreateLoadBalancerPool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(201)
		fmt.Fprintf(res, `{"algorithm": "least_connections", "created_at": "2019-01-01T12:00:00", "health_monitor": {"delay": 5, "max_retries": 2, "port": 22, "timeout": 2, "type": "http", "url_path": "/"}, "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "members": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004/members/80294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "port": 80, "target": {"address": "192.168.100.5"}, "weight": 50}], "name": "my-load-balancer-pool", "protocol": "http", "provisioning_status": "active", "session_persistence": {"type": "source_ip"}}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Invalid pool name
	nodes := []string{"192.168.1.1"}
	options := newServiceOptions()
	pool, err := v.CreateLoadBalancerPool("lbID", "poolName", nodes, options)
	assert.Nil(t, pool)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid pool name")

	// Success
	pool, err = v.CreateLoadBalancerPool("lbID", "tcp-80-30123", nodes, options)
	assert.NotNil(t, pool)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_CreateLoadBalancerPoolMember(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(201)
		fmt.Fprintf(res, `{"created_at": "2019-01-01T12:00:00", "health": "faulted", "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004/members/80294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "port": 80, "provisioning_status": "active", "target": {"address": "192.168.100.5"}, "weight": 50}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Invalid pool name
	member, err := v.CreateLoadBalancerPoolMember("lbID", "poolName", "poolID", "192.168.1.1")
	assert.Nil(t, member)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid pool name")

	// Success
	member, err = v.CreateLoadBalancerPoolMember("lbID", "tcp-80-30123", "poolID", "192.168.1.1")
	assert.NotNil(t, member)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_DeleteLoadBalancer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		if strings.Contains(req.URL.String(), "loadBalancerID_123") {
			res.WriteHeader(204)
		} else {
			res.WriteHeader(404)
		}
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	err := v.DeleteLoadBalancer("loadBalancerID_123")
	assert.Nil(t, err)

	// Error
	err = v.DeleteLoadBalancer("loadBalancerID_999")
	assert.NotNil(t, err)
}

func TestVpcSdkGen2_DeleteLoadBalancerListener(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		if (strings.Contains(req.URL.String(), "loadBalancerID_123")) && (strings.Contains(req.URL.String(), "listenerID_123")) {
			res.WriteHeader(204)
		} else {
			res.WriteHeader(404)
		}
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	err := v.DeleteLoadBalancerListener("loadBalancerID_123", "listenerID_123")
	assert.Nil(t, err)

	// Error
	err = v.DeleteLoadBalancerListener("loadBalancerID_999", "listenerID_999")
	assert.NotNil(t, err)
}

func TestVpcSdkGen2_DeleteLoadBalancerPool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		if (strings.Contains(req.URL.String(), "loadBalancerID_123")) && (strings.Contains(req.URL.String(), "poolID_123")) {
			res.WriteHeader(204)
		} else {
			res.WriteHeader(404)
		}
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	err := v.DeleteLoadBalancerPool("loadBalancerID_123", "poolID_123")
	assert.Nil(t, err)

	// Error
	err = v.DeleteLoadBalancerPool("loadBalancerID_999", "poolID_999")
	assert.NotNil(t, err)
}

func TestVpcSdkGen2_DeleteLoadBalancerPoolMember(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		if (strings.Contains(req.URL.String(), "loadBalancerID_123")) && (strings.Contains(req.URL.String(), "poolID_123")) && (strings.Contains(req.URL.String(), "poolMemberID_123")) {
			res.WriteHeader(204)
		} else {
			res.WriteHeader(404)
		}
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	err := v.DeleteLoadBalancerPoolMember("loadBalancerID_123", "poolID_123", "poolMemberID_123")
	assert.Nil(t, err)

	// Error
	err = v.DeleteLoadBalancerPoolMember("loadBalancerID_999", "poolID_999", "poolMemberID_999")
	assert.NotNil(t, err)
}

func TestVpcSdkGen2_GetLoadBalancer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		fmt.Fprintf(res, `{"created_at": "2019-01-01T12:00:00", "crn": "crn:v1:bluemix:public:is:us-south:a/123456::load-balancer:dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "hostname": "myloadbalancer-123456-us-south-1.lb.bluemix.net", "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "id": "dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "is_public": true, "listeners": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/listeners/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004"}], "name": "my-load-balancer", "operating_status": "offline", "pools": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "name": "my-load-balancer-pool"}], "private_ips": [{"address": "192.168.33.44"}], "provisioning_status": "active", "public_ips": [{"address": "192.168.3.4"}], "resource_group": {"href": "https://resource-controller.cloud.ibm.com/v2/resource_groups/fee82deba12e4c0fb69c3b09d1f12345", "id": "fee82deba12e4c0fb69c3b09d1f12345", "name": "my-resource-group"}, "subnets": [{"crn": "crn:v1:bluemix:public:is:us-south-1:a/123456::subnet:7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "href": "https://us-south.iaas.cloud.ibm.com/v1/subnets/7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "id": "7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "name": "my-subnet"}]}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	lb, err := v.GetLoadBalancer("load balancer id")
	assert.NotNil(t, lb)
	assert.Nil(t, err)
	assert.Equal(t, lb.ID, "dd754295-e9e0-4c9d-bf6c-58fbc59e5727")
	assert.Equal(t, lb.IsPublic, true)
	assert.Equal(t, lb.Name, "my-load-balancer")
	assert.Equal(t, lb.PrivateIps[0], "192.168.33.44")
	assert.Equal(t, lb.PublicIps[0], "192.168.3.4")
	assert.Equal(t, lb.Subnets[0].ID, "7ec86020-1c6e-4889-b3f0-a15f2e50f87e")
}

func TestVpcSdkGen2_GetSubnet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		fmt.Fprintf(res, `{"available_ipv4_address_count": 15, "created_at": "2019-01-01T12:00:00", "crn": "crn:v1:bluemix:public:is:us-south-1:a/123456::subnet:7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "href": "https://us-south.iaas.cloud.ibm.com/v1/subnets/7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "id": "7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "ip_version": "both", "ipv4_cidr_block": "10.0.0.0/24", "name": "my-subnet", "network_acl": {"crn": "crn:v1:bluemix:public:is:us-south:a/123456::network-acl:a4e28308-8ee7-46ab-8108-9f881f22bdbf", "href": "https://us-south.iaas.cloud.ibm.com/v1/network_acls/a4e28308-8ee7-46ab-8108-9f881f22bdbf", "id": "a4e28308-8ee7-46ab-8108-9f881f22bdbf", "name": "my-network-acl"}, "public_gateway": {"crn": "crn:v1:bluemix:public:is:us-south-1:a/123456::public-gateway:dc5431ef-1fc6-4861-adc9-a59d077d1241", "href": "https://us-south.iaas.cloud.ibm.com/v1/public_gateways/dc5431ef-1fc6-4861-adc9-a59d077d1241", "id": "dc5431ef-1fc6-4861-adc9-a59d077d1241", "name": "my-public-gateway", "resource_type": "public_gateway"}, "resource_group": {"href": "https://resource-controller.cloud.ibm.com/v2/resource_groups/fee82deba12e4c0fb69c3b09d1f12345", "id": "fee82deba12e4c0fb69c3b09d1f12345", "name": "my-resource-group"}, "status": "available", "total_ipv4_address_count": 256, "vpc": {"crn": "crn:v1:bluemix:public:is:us-south:a/123456::vpc:4727d842-f94f-4a2d-824a-9bc9b02c523b", "href": "https://us-south.iaas.cloud.ibm.com/v1/vpcs/4727d842-f94f-4a2d-824a-9bc9b02c523b", "id": "4727d842-f94f-4a2d-824a-9bc9b02c523b", "name": "my-vpc"}, "zone": {"href": "https://us-south.iaas.cloud.ibm.com/v1/regions/us-south/zones/us-south-1", "name": "us-south-1"}}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	subnet, err := v.GetSubnet("subnet id")
	assert.NotNil(t, subnet)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_ListLoadBalancers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		fmt.Fprintf(res, `{"load_balancers": [{"created_at": "2019-01-01T12:00:00", "crn": "crn:v1:bluemix:public:is:us-south:a/123456::load-balancer:dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "hostname": "myloadbalancer-123456-us-south-1.lb.bluemix.net", "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "id": "dd754295-e9e0-4c9d-bf6c-58fbc59e5727", "is_public": true, "listeners": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/listeners/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004"}], "name": "my-load-balancer", "operating_status": "offline", "pools": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "name": "my-load-balancer-pool"}], "private_ips": [{"address": "192.168.3.4"}], "provisioning_status": "active", "public_ips": [{"address": "192.168.3.4"}], "resource_group": {"href": "https://resource-controller.cloud.ibm.com/v2/resource_groups/fee82deba12e4c0fb69c3b09d1f12345", "id": "fee82deba12e4c0fb69c3b09d1f12345", "name": "my-resource-group"}, "subnets": [{"crn": "crn:v1:bluemix:public:is:us-south-1:a/123456::subnet:7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "href": "https://us-south.iaas.cloud.ibm.com/v1/subnets/7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "id": "7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "name": "my-subnet"}]}]}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	lbs, err := v.ListLoadBalancers()
	assert.Equal(t, len(lbs), 1)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_ListLoadBalancerListeners(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		fmt.Fprintf(res, `{"listeners": [{"certificate_instance": {"crn": "crn:v1:bluemix:public:cloudcerts:us-south:a/123456:b8866ea4-b8df-467e-801a-da1db7e020bf:certificate:78ff9c4c97d013fb2a95b21dddde7758"}, "connection_limit": 2000, "created_at": "2019-01-01T12:00:00", "default_pool": {"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "name": "my-load-balancer-pool"}, "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/listeners/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "policies": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/listeners/70294e14-4e61-11e8-bcf4-0242ac110004/policies/f3187486-7b27-4c79-990c-47d33c0e2278", "id": "70294e14-4e61-11e8-bcf4-0242ac110004"}], "port": 443, "protocol": "http", "provisioning_status": "active"}]}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	listeners, err := v.ListLoadBalancerListeners("load balancer ID")
	assert.Equal(t, len(listeners), 1)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_ListLoadBalancerPools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		if !strings.Contains(req.URL.String(), "members") {
			fmt.Fprintf(res, `{"pools": [{"algorithm": "least_connections", "created_at": "2019-01-01T12:00:00", "health_monitor": {"delay": 5, "max_retries": 2, "port": 22, "timeout": 2, "type": "http", "url_path": "/"}, "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "members": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004/members/80294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "port": 80, "target": {"address": "192.168.100.5"}, "weight": 50}], "name": "my-load-balancer-pool", "protocol": "http", "provisioning_status": "active", "session_persistence": {"type": "source_ip"}}]}`)
		} else {
			fmt.Fprintf(res, `{"members": [{"created_at": "2019-01-01T12:00:00", "health": "faulted", "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004/members/80294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004-001", "port": 80, "provisioning_status": "active", "target": {"address": "192.168.100.5"}, "weight": 50}]}`)
		}
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	pools, err := v.ListLoadBalancerPools("load balancer ID")
	assert.Equal(t, len(pools), 1)
	assert.Nil(t, err)
	assert.Equal(t, pools[0].Name, "my-load-balancer-pool")
	assert.Equal(t, pools[0].ID, "70294e14-4e61-11e8-bcf4-0242ac110004")
	assert.Equal(t, pools[0].ProvisioningStatus, "active")
	assert.Equal(t, pools[0].Members[0].ID, "70294e14-4e61-11e8-bcf4-0242ac110004-001")
}

func TestVpcSdkGen2_ListLoadBalancerPoolMembers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		fmt.Fprintf(res, `{"members": [{"created_at": "2019-01-01T12:00:00", "health": "faulted", "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004/members/80294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "port": 80, "provisioning_status": "active", "target": {"address": "192.168.100.5"}, "weight": 50}]}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	members, err := v.ListLoadBalancerPoolMembers("load balancer ID", "pool ID")
	assert.Equal(t, len(members), 1)
	assert.Nil(t, err)
	assert.Equal(t, members[0].ID, "70294e14-4e61-11e8-bcf4-0242ac110004")
}

func TestVpcSdkGen2_ListSubnets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		fmt.Fprintf(res, `{"first": {"href": "https://us-south.iaas.cloud.ibm.com/v1/subnets?limit=20"}, "limit": 20, "subnets": [{"available_ipv4_address_count": 15, "created_at": "2019-01-01T12:00:00", "crn": "crn:v1:bluemix:public:is:us-south-1:a/123456::subnet:7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "href": "https://us-south.iaas.cloud.ibm.com/v1/subnets/7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "id": "7ec86020-1c6e-4889-b3f0-a15f2e50f87e", "ip_version": "both", "ipv4_cidr_block": "10.0.0.0/24", "name": "my-subnet", "network_acl": {"crn": "crn:v1:bluemix:public:is:us-south:a/123456::network-acl:a4e28308-8ee7-46ab-8108-9f881f22bdbf", "href": "https://us-south.iaas.cloud.ibm.com/v1/network_acls/a4e28308-8ee7-46ab-8108-9f881f22bdbf", "id": "a4e28308-8ee7-46ab-8108-9f881f22bdbf", "name": "my-network-acl"}, "public_gateway": {"crn": "crn:v1:bluemix:public:is:us-south-1:a/123456::public-gateway:dc5431ef-1fc6-4861-adc9-a59d077d1241", "href": "https://us-south.iaas.cloud.ibm.com/v1/public_gateways/dc5431ef-1fc6-4861-adc9-a59d077d1241", "id": "dc5431ef-1fc6-4861-adc9-a59d077d1241", "name": "my-public-gateway", "resource_type": "public_gateway"}, "resource_group": {"href": "https://resource-controller.cloud.ibm.com/v2/resource_groups/fee82deba12e4c0fb69c3b09d1f12345", "id": "fee82deba12e4c0fb69c3b09d1f12345", "name": "my-resource-group"}, "status": "available", "total_ipv4_address_count": 256, "vpc": {"crn": "crn:v1:bluemix:public:is:us-south:a/123456::vpc:4727d842-f94f-4a2d-824a-9bc9b02c523b", "href": "https://us-south.iaas.cloud.ibm.com/v1/vpcs/4727d842-f94f-4a2d-824a-9bc9b02c523b", "id": "4727d842-f94f-4a2d-824a-9bc9b02c523b", "name": "my-vpc"}, "zone": {"href": "https://us-south.iaas.cloud.ibm.com/v1/regions/us-south/zones/us-south-1", "name": "us-south-1"}}], "total_count": 132}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Success
	subnets, err := v.ListSubnets()
	assert.Equal(t, len(subnets), 1)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_ReplaceLoadBalancerPoolMembers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(202)
		fmt.Fprintf(res, `{"members": [{"created_at": "2019-01-01T12:00:00", "health": "faulted", "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004/members/80294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "port": 80, "provisioning_status": "active", "target": {"address": "192.168.100.5"}, "weight": 50}]}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Invalid pool name
	nodes := []string{"192.168.1.1"}
	members, err := v.ReplaceLoadBalancerPoolMembers("lbID", "poolName", "poolID", nodes)
	assert.Nil(t, members)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid pool name")

	// Success
	members, err = v.ReplaceLoadBalancerPoolMembers("lbID", "tcp-80-30123", "poolID", nodes)
	assert.NotNil(t, members)
	assert.Nil(t, err)
}

func TestVpcSdkGen2_UpdateLoadBalancerPool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-type", "application/json")
		res.WriteHeader(200)
		fmt.Fprintf(res, `{"algorithm": "least_connections", "created_at": "2019-01-01T12:00:00", "health_monitor": {"delay": 5, "max_retries": 2, "port": 22, "timeout": 2, "type": "http", "url_path": "/"}, "href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "members": [{"href": "https://us-south.iaas.cloud.ibm.com/v1/load_balancers/dd754295-e9e0-4c9d-bf6c-58fbc59e5727/pools/70294e14-4e61-11e8-bcf4-0242ac110004/members/80294e14-4e61-11e8-bcf4-0242ac110004", "id": "70294e14-4e61-11e8-bcf4-0242ac110004", "port": 80, "target": {"address": "192.168.100.5"}, "weight": 50}], "name": "my-load-balancer-pool", "protocol": "http", "provisioning_status": "active", "session_persistence": {"type": "source_ip"}}`)
	}))
	defer server.Close()

	// Create the VPC client and SDK interface
	v := newNoAuthTestVpcSdkGen2(server.URL)

	// Invalid pool name
	options := newServiceOptions()
	members, err := v.UpdateLoadBalancerPool("lbID", "poolName", &VpcLoadBalancerPool{ID: "poolID"}, options)
	assert.Nil(t, members)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid pool name")

	// Success
	members, err = v.UpdateLoadBalancerPool("lbID", "tcp-80-30123", &VpcLoadBalancerPool{ID: "poolID"}, options)
	assert.NotNil(t, members)
	assert.Nil(t, err)
}
