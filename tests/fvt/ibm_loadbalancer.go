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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.ibm.com/cloud-provider-ibm/ibm"
	"k8s.io/klog/v2"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	cloudprovider "k8s.io/cloud-provider"
)

const (
	lbDeploymentNamespace = "ibm-system"
	lbNameLabel           = "ibm-cloud-provider-lb-name"
	npFvtTesterLabel      = "ibm-fvt-tester-created-service"
)

func init() {
	flag.StringVar(&td.action, "action", "default", "Action to take: default ('create' if load balancer doesn't exist, else 'delete'), create, update, delete, monitor")
	flag.StringVar(&td.name, "name", "test", "Name of the load balancer")
	flag.StringVar(&td.ip, "ip", "", "Requested load balancer IP")
	flag.StringVar(&td.sourceIPRange, "source-ip-range", "", "Requested load balancer source IP range")
	flag.StringVar(&td.annotation, "annotation", "", "Requested load balancer annotation")
	flag.IntVar(&td.imageVersion, "image-version", 0, "Requested load balancer deployment image version")
}

func exitHandler(ibmCloud *ibm.Cloud, startTime time.Time) {
	klog.Info("Collecting warning events ...")
	// Sleep to give the recorder time to sync all events. Syncing is done
	// every 10 seconds.
	time.Sleep(time.Second * 12)
	listOptions := metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("type", v1.EventTypeWarning).String()}
	events, err := ibmCloud.KubeClient.CoreV1().Events(lbDeploymentNamespace).List(context.TODO(), listOptions)
	if nil != err {
		klog.Warningf("Failed to get warning events: %v", err)
	} else {
		for _, event := range events.Items {
			// Allow for a minute of clock drift between systems for
			// determining the events to collect.
			if startTime.Before(event.LastTimestamp.Time.Add(time.Minute)) {
				klog.Warningf("Warning Event: %v", event.Message)
			}
		}
	}
}

type testData struct {
	action        string
	name          string
	ip            string
	sourceIPRange string
	annotation    string
	imageVersion  int
}

var td testData

/*
ibm_loadbalancer tests the ibm cloud provider LoadBalancer CRUD operations
by creating or deleting a "test" cloud load balancer based on its existence.

	$ ./ibm_loadbalancer --logtostderr=true
*/
func main() {
	var exist bool
	var err error
	var config *os.File
	var c cloudprovider.Interface
	var lb cloudprovider.LoadBalancer
	var s *v1.Service

	// Parse the klog flags.
	klog.InitFlags(nil)
	flag.Parse()

	// Get cloud provider config from current directory.
	config, err = os.Open("./ibm-cloud-config.ini")
	if nil != err {
		panic(err.Error())
	}
	defer func() {
		if err := config.Close(); err != nil {
			panic(err.Error())
		}
	}()
	c, err = ibm.NewCloud(config)
	if nil != err {
		panic(err.Error())
	}
	ibmCloud, ok := c.(*ibm.Cloud)
	if !ok {
		panic(fmt.Errorf("Unexpected cloud type created"))
	}

	// Add exit handler for event handling.
	defer exitHandler(ibmCloud, time.Now())

	lb, _ = c.LoadBalancer()

	clusterName := "test"
	s = &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      td.name,
			Namespace: lbDeploymentNamespace,
			SelfLink:  "/api/v1/namespaces/" + lbDeploymentNamespace + "/services/" + td.name,
		},
	}
	if ibmCloud.Config.Prov.ProviderType != "gc" && ibmCloud.Config.Prov.ProviderType != "g2" {
		// For classic LB, just use name as UID
		s.ObjectMeta.UID = types.UID(td.name)
	} else {
		// For VPC LB, use a 32 character UID
		lbUID := td.name + "0123456789abcdef0123456789abcdef" // Make a UID that is at least 32 characters
		if len(lbUID) > 32 {
			lbUID = lbUID[:32] // shorten the UID to 32 characters if needed
		}
		s.ObjectMeta.UID = types.UID(lbUID)
	}

	s.Annotations = map[string]string{}

	matchLabel := lbNameLabel + "=" + ibm.GetCloudProviderLoadBalancerName(s)
	_, exist, err = lb.GetLoadBalancer(context.Background(), clusterName, s)
	if nil != err {
		panic(err.Error())
	}

	// Determine the default action to take.
	if 0 == strings.Compare("default", td.action) {
		if !exist {
			td.action = "create"
		} else {
			td.action = "delete"
		}
	}

	// If VPC, we need to ensure a NodePort service exists in the cluster
	// before we try to create/delete/modfiy a LB service using this FVT
	// test code
	if ibmCloud.Config.Prov.ProviderType == "gc" || ibmCloud.Config.Prov.ProviderType == "g2" {
		err = createNodePortServiceIfNeeded(ibmCloud, s)
		if err != nil {
			panic(err.Error())
		}
	}

	// Take the action on the load balancer.
	switch td.action {
	case "create":
		klog.Info("Creating load balancer ...")
		s.Spec.LoadBalancerIP = td.ip
		if 0 < len(td.sourceIPRange) {
			s.Spec.LoadBalancerSourceRanges = []string{td.sourceIPRange}
		}
		if 0 < len(td.annotation) {
			s.Annotations[td.annotation] = ""
		}

		// Loop for 5 minutes calling EnsureLoadBalancer, checking every 30 seconds if it was created successfully
		for i := 1; i <= 10; i++ {
			_, err = lb.EnsureLoadBalancer(context.Background(), clusterName, s, nil)
			if nil == err {
				break
			}
			klog.Infof("Try %d: EnsureLoadBalancer returned error: %v", i, err)
			time.Sleep(time.Second * 30)
		}
		if nil != err {
			panic(err.Error())
		}
		klog.Info("Ensuring load balancer created ...")
		// Loop for 20 more minutes, until the LB is fully created
		//   - VPC LBs might take this long, they will return an error while they are in the process of being created
		//   - classic LBs should return quickly)
		var status *v1.LoadBalancerStatus
		for i := 1; i <= 40; i++ {
			status, err = lb.EnsureLoadBalancer(context.Background(), clusterName, s, nil)
			if err == nil {
				break
			}
			klog.Infof("Try %d: EnsureLoadBalancer returned error: %v", i, err)
			time.Sleep(time.Second * 30)
		}

		if nil != err {
			panic(err.Error())
		}

		// Don't do this with VPC since it doesn't make sense
		if ibmCloud.Config.Prov.ProviderType != "gc" && ibmCloud.Config.Prov.ProviderType != "g2" {
			klog.Info("Creating load balancer with duplicate IP ...")
			s.ObjectMeta.UID = types.UID("recreate")
			s.Spec.LoadBalancerIP = status.Ingress[0].IP
			_, err = lb.EnsureLoadBalancer(context.Background(), clusterName, s, nil)
			if nil == err {
				panic("Creating load balancer with duplicate IP was successful")
			} else {
				klog.Infof("Creating load balancer with duplicate IP failed: %v", err)
			}

			klog.Info("Creating load balancer with unavailable IP ...")
			s.ObjectMeta.UID = types.UID("unavailable")
			s.Spec.LoadBalancerIP = "8.8.8.8"
			_, err = lb.EnsureLoadBalancer(context.Background(), clusterName, s, nil)
			if nil == err {
				panic("Creating load balancer with unavailable IP was successful")
			} else {
				klog.Infof("Creating load balancer with unavailable IP failed: %v", err)
			}
		}
	case "update":
		klog.Info("Updating load balancer ...")
		lbDeployments, err := ibmCloud.KubeClient.AppsV1().Deployments(lbDeploymentNamespace).List(context.TODO(), metav1.ListOptions{LabelSelector: matchLabel})
		if nil != err {
			panic(err.Error())
		} else if 0 == len(lbDeployments.Items) {
			panic(fmt.Errorf("Load balancer does not exist"))
		}
		imageSplit := strings.Split(ibmCloud.Config.LBDeployment.Image, ":")
		ibmCloud.Config.LBDeployment.Image = fmt.Sprintf("%v:%v", imageSplit[0], td.imageVersion)
		klog.Infof("Using image: %v", ibmCloud.Config.LBDeployment.Image)
		_, err = lb.EnsureLoadBalancer(context.Background(), clusterName, s, nil)
		if nil != err {
			panic(err.Error())
		}
	case "delete":
		klog.Info("Deleting load balancer ...")
		// Loop for 5 minutes calling EnsureLoadBalancerDeleted, checking every 15 seconds if it was deleted successfully
		for i := 1; i <= 20; i++ {
			err = lb.EnsureLoadBalancerDeleted(context.Background(), clusterName, s)
			if nil == err {
				break
			}
			klog.Infof("Try %d: EnsureLoadBalancerDeleted returned error: %v", i, err)
			time.Sleep(time.Second * 15)
		}
		if nil != err {
			panic(err.Error())
		}

		// For Classic LBv1 and LBv2 Loadbalancers, wait for keepalived pods to be deleted, then call EnsureLoadBalancerDeleted again
		if ibmCloud.Config.Prov.ProviderType != "gc" && ibmCloud.Config.Prov.ProviderType != "g2" {
			klog.Info("Waiting for load balancer keepalived pods to go away ...")
			for i := 1; i <= 30; i++ {
				time.Sleep(time.Second)
				lbPods, err := ibmCloud.KubeClient.CoreV1().Pods(lbDeploymentNamespace).List(context.TODO(), metav1.ListOptions{LabelSelector: matchLabel})
				if nil != err {
					panic(err.Error())
				}
				if 0 == len(lbPods.Items) {
					break
				}
			}
			klog.Info("Ensuring load balancer deleted ...")
			err = lb.EnsureLoadBalancerDeleted(context.Background(), clusterName, s)
			if err != nil {
				panic(err.Error())
			}

		} else {
			// For VPC Loadbalancers, delete the NodePort service if we created it
			err = deleteNodePortService(ibmCloud, s)
			if nil != err {
				panic(err.Error())
			}
		}

	case "monitor":
		// Stop the default monitor and start another for the test.
		ibmCloud.StopTask(ibm.MonitorLoadBalancers)
		klog.Infof("Monitoring load balancers every 3 seconds for 15 seconds ...")
		ibmCloud.StartTask(ibm.MonitorLoadBalancers, time.Second*3)
		time.Sleep(time.Second * 15)
		ibmCloud.StopTask(ibm.MonitorLoadBalancers)
		time.Sleep(time.Second * 3)
	default:
		panic(fmt.Errorf("Action not valid: %v", td.action))
	}

	klog.Info("Test completed successfully")
}

// Create a NodePort service based on the info passed in, so tht when the vpcctl
// binaray tries to create a LB, it will find the service and pull the NodePort, etc
// info from the service.
func createNodePortServiceIfNeeded(c *ibm.Cloud, s *v1.Service) error {
	// Check if any service exists with this name
	_, err := c.KubeClient.CoreV1().Services(s.ObjectMeta.Namespace).Get(context.TODO(), s.ObjectMeta.Name, metav1.GetOptions{})
	if err == nil {
		klog.Infof("Service %v/%v already exists", s.ObjectMeta.Namespace, s.ObjectMeta.Name)
		return nil
	}

	// The service does not exists, so add a simple NodePort service
	npService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.ObjectMeta.Name,
			Namespace: s.ObjectMeta.Namespace,
			Labels: map[string]string{
				"run":            s.ObjectMeta.Name,
				npFvtTesterLabel: s.ObjectMeta.Name,
			},
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"run": s.ObjectMeta.Name,
			},
			Ports: []v1.ServicePort{
				{
					Name:       "port-1",
					Port:       80,
					Protocol:   "TCP",
					TargetPort: intstr.FromInt(80),
				},
			},
			Type: v1.ServiceTypeNodePort,
		},
	}

	_, err = c.KubeClient.CoreV1().Services(s.ObjectMeta.Namespace).Create(context.TODO(), npService, metav1.CreateOptions{})
	if nil != err {
		klog.Infof("Error creating NodePort service %v/%v", s.ObjectMeta.Namespace, s.ObjectMeta.Name)
		return err
	}

	klog.Infof("Successfully created NodePort service %v/%v", s.ObjectMeta.Namespace, s.ObjectMeta.Name)
	return nil
}

// Delete the NodePort service based on the info passed in, but only if this FVT tester
// created it (check the label we add her)
func deleteNodePortService(c *ibm.Cloud, s *v1.Service) error {
	// Check if there is one to delete
	labelToSearchFor := npFvtTesterLabel + "=" + s.ObjectMeta.Name
	listOptions := metav1.ListOptions{LabelSelector: labelToSearchFor}
	serviceList, err := c.KubeClient.CoreV1().Services(s.ObjectMeta.Namespace).List(context.TODO(), listOptions)
	switch {
	case err != nil:
		return err
	case len(serviceList.Items) < 1 || len(serviceList.Items) > 1:
		klog.Infof("Found %v service items with name %v/%v and created by FVT tester, so nothing to do", len(serviceList.Items), s.ObjectMeta.Namespace, s.ObjectMeta.Name)
	default:
		klog.Infof("Deleting  service %v/%v", s.ObjectMeta.Namespace, s.ObjectMeta.Name)
		err = c.KubeClient.CoreV1().Services(s.ObjectMeta.Namespace).Delete(context.TODO(), s.ObjectMeta.Name, metav1.DeleteOptions{})
		return err
	}
	return nil
}
