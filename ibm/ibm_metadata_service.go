/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2019, 2025 All Rights Reserved.
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
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	clientretry "k8s.io/client-go/util/retry"
	cloudproviderapi "k8s.io/cloud-provider/api"
	nodeutil "k8s.io/component-helpers/node/util"
	"k8s.io/klog/v2"
)

// NodeMetadata holds the provider metatdata from a node.
// Field names reflects Kubernetes CCM terminology.
type NodeMetadata struct {
	InternalIP    string
	ExternalIP    string
	WorkerID      string
	InstanceType  string
	FailureDomain string
	Region        string
	ProviderID    string
}

// MetadataService provides access to provider metadata stored in node labels.
type MetadataService struct {
	provider       Provider
	kubeClient     kubernetes.Interface
	vpcClient      *vpcClient
	nodeMap        map[string]NodeMetadata
	nodeMapMux     sync.Mutex
	nodeCacheStart time.Time
}

const (
	internalIPLabel    string = "ibm-cloud.kubernetes.io/internal-ip"
	externalIPLabel    string = "ibm-cloud.kubernetes.io/external-ip"
	failureDomainLabel string = "ibm-cloud.kubernetes.io/zone"
	regionLabel        string = "ibm-cloud.kubernetes.io/region"
	workerIDLabel      string = "ibm-cloud.kubernetes.io/worker-id"
	machineTypeLabel   string = "ibm-cloud.kubernetes.io/machine-type"
)

var (
	errLabelsMissing = errors.New("node is missing labels")
	cacheTTL         = time.Duration(24) * time.Hour
)

// NewMetadataService creates a service using the specified client to connect to the
// cluster.  kubernetes.Interface could be a kubernetes/fake ClientSet
func NewMetadataService(provider *Provider, kubeClient kubernetes.Interface) *MetadataService {
	ms := MetadataService{}
	if provider != nil {
		ms.provider = *provider
		klog.Infof("MetadataService: created for provider: %+v", ms.provider)
	}
	ms.kubeClient = kubeClient
	ms.nodeMap = make(map[string]NodeMetadata)
	ms.nodeMapMux = sync.Mutex{}
	ms.nodeCacheStart = time.Now()
	return &ms
}

func (ms *MetadataService) deleteCachedNode(name string) {
	ms.nodeMapMux.Lock()
	defer ms.nodeMapMux.Unlock()
	klog.Infof("MetadataService: node %s deleted from cache, metadata: %+v", name, ms.nodeMap[name])
	delete(ms.nodeMap, name)
}

func (ms *MetadataService) getCachedNode(name string) (NodeMetadata, bool) {
	ms.nodeMapMux.Lock()
	defer ms.nodeMapMux.Unlock()
	var node NodeMetadata
	var ok bool
	if time.Since(ms.nodeCacheStart) < cacheTTL {
		node, ok = ms.nodeMap[name]
	} else {
		klog.Infof("MetadataService: node %s cacheTTL expired, allocate new node meta data array", name)
		ms.nodeMap = make(map[string]NodeMetadata)
		ms.nodeCacheStart = time.Now()
		ok = false
	}
	return node, ok
}

func (ms *MetadataService) putCachedNode(name string, node NodeMetadata) {
	ms.nodeMapMux.Lock()
	defer ms.nodeMapMux.Unlock()
	ms.nodeMap[name] = node
}

// GetNodeMetadata returns the metadata for the named node.  If the node does
// not exist, or not all data is available, an error is returned.
func (ms *MetadataService) GetNodeMetadata(name string, applyNetworkUnavailable bool, cni string) (NodeMetadata, error) {
	node, ok := ms.getCachedNode(name)
	if ok {
		// The following trace statement occurs too often so it was commented out
		// klog.Infof("MetadataService: node %s found in cache, cached: %+v", name, node)
		return node, nil
	}
	klog.Infof("MetadataService: node %s not in cache (applyNetUnAvail:%v, cni:%s)", name, applyNetworkUnavailable, cni)
	k8sNode, err := ms.kubeClient.CoreV1().Nodes().Get(context.TODO(), string(name), metav1.GetOptions{})
	if nil != err {
		klog.Errorf("MetadataService: node %s failed to get k8s node data: %v", name, err)
		return node, err
	}
	if applyNetworkUnavailable && cni != "OVNKubernetes" {
		// Check if the node has the external cloud provider taint (which means we are initializing a node)
		cloudTaintFound := false
		for _, taint := range k8sNode.Spec.Taints {
			if taint.Key == cloudproviderapi.TaintExternalCloudProvider {
				cloudTaintFound = true
				break
			}
		}
		// Check if the node has the NetworkUnavailable condition
		_, networkUnavailableCondition := nodeutil.GetNodeCondition(&k8sNode.Status, v1.NodeNetworkUnavailable)
		// If it has the taint, but not the condition, add the condition
		if cloudTaintFound && networkUnavailableCondition == nil {
			UpdateNodeSpecBackoff := wait.Backoff{
				Steps:    20,
				Duration: 50 * time.Millisecond,
				Jitter:   1.0,
			}
			err = clientretry.RetryOnConflict(UpdateNodeSpecBackoff, func() error {
				if err := nodeutil.SetNodeCondition(ms.kubeClient, types.NodeName(name), v1.NodeCondition{
					Type:               v1.NodeNetworkUnavailable,
					Status:             v1.ConditionTrue,
					Reason:             "No CNI present",
					Message:            "There is no active CNI present on the node",
					LastTransitionTime: metav1.Now(),
				}); err != nil {
					return err
				}
				return nil
			})
			if err != nil {
				klog.Errorf("MetadataService: node %s failed to apply 'NetworkUnavailable' condition: %v ", name, err)
				return node, err
			}
			klog.Infof("MetadataService: node %s successfully applied 'NetworkUnavailable' condition", name)
		}
	}
	newNode := NodeMetadata{}
	// When getting labels, it is possible the node labels have not yet been set.
	// vagrant adds labels one by one, so make sure we have all the labels.
	missingLabels := []string{}
	var labelOk bool
	ok = true
	newNode.InternalIP, labelOk = k8sNode.Labels[internalIPLabel]
	if !labelOk {
		missingLabels = append(missingLabels, internalIPLabel)
		ok = false
	}
	// ExternalIP is not present for "private-only" workers.
	newNode.ExternalIP = k8sNode.Labels[externalIPLabel]
	newNode.WorkerID, labelOk = k8sNode.Labels[workerIDLabel]
	if !labelOk {
		missingLabels = append(missingLabels, workerIDLabel)
		ok = false
	}
	newNode.InstanceType, labelOk = k8sNode.Labels[machineTypeLabel]
	if !labelOk {
		missingLabels = append(missingLabels, machineTypeLabel)
		ok = false
	}
	newNode.FailureDomain, labelOk = k8sNode.Labels[failureDomainLabel]
	if !labelOk {
		missingLabels = append(missingLabels, failureDomainLabel)
		ok = false
	}
	newNode.Region, labelOk = k8sNode.Labels[regionLabel]
	if !labelOk {
		missingLabels = append(missingLabels, regionLabel)
		ok = false
	}
	newNode.ProviderID = k8sNode.Spec.ProviderID

	// Indicate what node labels are missing
	if len(missingLabels) > 0 {
		klog.Warningf("MetadataService: node %s missing %d label(s): %s", name, len(missingLabels), strings.Join(missingLabels, ","))
		// Give bootstrap some time to update the node labels
		time.Sleep(time.Second)
	}

	// If all labels were set, cache and return the result
	if ok {
		klog.Infof("MetadataService: node %s saved to cache, metadata: %+v", name, newNode)
		ms.putCachedNode(name, newNode)
		return newNode, nil
	} else if isProviderVpc(ms.provider.ProviderType) && ms.provider.AccountID == ms.provider.G2WorkerServiceAccountID {
		// labels were not set; if VPC we can try to call api for values
		klog.Infof("MetadataService: node %s retrieve details from VPC instance", name)

		// create vpcClient if we haven't already
		if ms.vpcClient == nil {
			ms.vpcClient, err = newVpcClient(ms.provider)
			if err != nil {
				klog.Errorf("MetadataService: node %s failed to create VPC client: %v", node, err)
				return node, err
			}
		}

		// gather node information from VPC
		err = ms.vpcClient.populateNodeMetadata(name, &newNode)
		if err != nil {
			klog.Errorf("MetadataService: node %s failed to get details from VPC instance: %v", name, err)
			return node, err
		}

		klog.Infof("MetadataService: node %s save to cache, metadata: %+v", name, newNode)
		ms.putCachedNode(name, newNode)
		return newNode, nil
	}

	return node, errLabelsMissing
}
