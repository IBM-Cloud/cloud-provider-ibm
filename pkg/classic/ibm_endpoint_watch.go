/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2018, 2023 All Rights Reserved.
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
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	panicCooldownPeriod = 10
)

// Defined and used for allowing tests to override functions
var deletePodViaName = deletePod
var listPodsViaLabel = listPods
var getServiceViaEndpoint = getService

// deletePod deletes a pods in a specific namespace. Do not reference this function directly,
// use deleteServiceLBPod so it can be overridden for testing.
func deletePod(namespace string, podName string, kubeClient clientset.Interface) error {
	return kubeClient.CoreV1().Pods(namespace).Delete(context.TODO(), podName, metav1.DeleteOptions{})
}

// listPods lists all pods that match a specific label selector. Do not reference this function directly,
// use listPodsViaLabel so it can be overridden for testing.
func listPods(namespace string, labelSelector string, kubeClient clientset.Interface) (*v1.PodList, error) {
	return kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
}

// getService gets the pod that uses a specific endpoint. Do not reference this function directly,
// use getServiceViaEndpoint so it can be overridden for testing.
func getService(namespace string, endpointName string, kubeClient clientset.Interface) (*v1.Service, error) {
	return kubeClient.CoreV1().Services(namespace).Get(context.TODO(), endpointName, metav1.GetOptions{})
}

func (c *Cloud) handleEndpointWatchCrash() {
	if r := recover(); r != nil {
		klog.Errorf("Background Endpoint Watch Process StackTrace: %v \nBackground Endpoint Watch Process Panic Error: %v", string(debug.Stack()), r)

		// Cool down period before retrying.
		time.Sleep(time.Second * panicCooldownPeriod)
		klog.Info("Recovered panic in background endpoint watcher")
	}
}

// Main logic to handle Endpoint updates
func (c *Cloud) handleEndpointUpdate(oldObj, newObj interface{}) {

	// Catch all panics that come from the endpoint watch, sleep then close the channel to allow a restart
	defer c.handleEndpointWatchCrash()

	// We don't care about the old state of the endpoints, only the new state
	ep := newObj.(*v1.Endpoints)

	deleteCallback := func(podToDelete v1.Pod, service *v1.Service) {
		err := deletePodViaName(lbDeploymentNamespace, podToDelete.Name, c.KubeClient)
		if err != nil && service != nil {
			errorMessage := fmt.Sprintf("Failed to move the load balancer pod %v in namespace %v due to "+
				"error %v. Moving the pod is required to support the local external traffic policy spec for "+
				"service %v. Delete the pod to resolve the problem.", podToDelete.Name, podToDelete.Namespace, err, service.Name)
			c.Recorder.LoadBalancerServiceWarningEvent(service, DeletingLoadBalancerPodFailed, errorMessage)
		}
	}

	c.checkIfKeepalivedPodShouldBeDeleted(
		ep,
		deleteCallback,
	)
}

func (c *Cloud) checkIfKeepalivedPodShouldBeDeleted(ep *v1.Endpoints, deletePod func(podToDelete v1.Pod, service *v1.Service)) bool {
	var service *v1.Service
	var err error

	klog.V(3).Infof("Handle Endpoint Update: Service attributes: Namespace: %v, Name %v, Number of Subsets: %d", ep.Namespace, ep.Name, len(ep.Subsets))

	// Determine what nodes the service endpoints are running on
	endpointNodes := map[string]string{}
	if len(ep.Subsets) > 0 {
		for _, subset := range ep.Subsets {
			for _, address := range subset.Addresses {
				if address.NodeName != nil {
					endpointNodes[*address.NodeName] = address.IP
				}
			}
		}
	}
	if len(endpointNodes) == 0 {
		// If there are no active endpoints for the service, then it does not make sense to kill the keepalived pods
		// because they won't be dispatched to any additional node since the pod affinity to the service will not be satisfied
		// Therefore, if there are no endpoints, just return
		return false
	}

	// Retrieve the service that is associated with these endpoints
	service, err = getServiceViaEndpoint(ep.Namespace, ep.Name, c.KubeClient)
	if err != nil {
		klog.Errorf("ERROR: Failed to get service: %v", err)
		return false
	}

	// We only care about services that are LoadBalancers
	if service.Spec.Type != v1.ServiceTypeLoadBalancer {
		return false
	}

	// We only care if the ExternalTrafficPolicy is set to "Local"
	if service.Spec.ExternalTrafficPolicy != v1.ServiceExternalTrafficPolicyTypeLocal {
		return false
	}

	// We don't need pod affinity with the endpoint when using IPVS
	ipvsEnabled := isFeatureEnabled(service, lbFeatureIPVS)
	if ipvsEnabled {
		return false
	}

	var LoadBalancerIP string
	if len(service.Status.LoadBalancer.Ingress) > 0 {
		// Determine the Load Balancer IP address
		LoadBalancerIP = service.Status.LoadBalancer.Ingress[0].IP
	}

	// We only care if the service was assigned a load balancer IP
	if LoadBalancerIP == "" {
		return false
	}

	klog.Infof("Service Load balancer IP address: %v, Service type: %v, ExternalTrafficPolicy: %v, Nodes where endpoint is running: %v",
		LoadBalancerIP,
		service.Spec.Type,
		service.Spec.ExternalTrafficPolicy,
		endpointNodes)

	// Retrieve list of keepalived pods
	lbIPLabelValue := getCloudProviderIPLabelValue(LoadBalancerIP)

	podList, err := listPodsViaLabel(lbDeploymentNamespace, lbIPLabel+"="+lbIPLabelValue, c.KubeClient)

	if err != nil {
		klog.Errorf("ERROR: Failed to get list of pods: %v", err)
		return false
	}

	for _, pod := range podList.Items {

		if pod.Status.HostIP == "" {
			klog.V(3).Infof("Host IP is not set for pod: Name: %v, Namespace: %v", pod.Name, pod.Namespace)
			continue
		}

		// Determine where the pod is running
		if podIP, exist := endpointNodes[pod.Status.HostIP]; exist {
			klog.V(3).Infof("Endpoint %v is running on this node", podIP)
			continue
		}
		klog.Warning("Keepalived pod is running on a node that does not have an endpoint")

		if deletePod != nil {
			klog.Infof("Calling delete callback to delete pod %s", pod.Name)
			deletePod(pod, service)
		} else {
			return true
		}
	}

	return false
}
