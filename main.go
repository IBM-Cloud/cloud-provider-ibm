/*******************************************************************************
* IBM Cloud Kubernetes Service, 5737-D43
* (C) Copyright IBM Corp. 2019, 2022 All Rights Reserved.
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

// References:
// - https://raw.githubusercontent.com/kubernetes/kubernetes/v1.22.17/staging/src/k8s.io/cloud-provider/app/controllermanager.go
// - https://raw.githubusercontent.com/kubernetes/kubernetes/v1.22.17/staging/src/k8s.io/cloud-provider/sample/basic_main.go

package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"cloud.ibm.com/cloud-provider-ibm/ibm"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cloud-provider"
	"k8s.io/cloud-provider/app"
	"k8s.io/cloud-provider/app/config"
	"k8s.io/cloud-provider/options"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/logs"
	_ "k8s.io/component-base/metrics/prometheus/clientgo" // load all the prometheus client-go plugins
	_ "k8s.io/component-base/metrics/prometheus/version"  // for version metric registration
	"k8s.io/component-base/term"
	"k8s.io/klog/v2"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	ccmOptions, err := options.NewCloudControllerManagerOptions()
	if err != nil {
		klog.Fatalf("unable to initialize IBM Cloud controller manager command options: %v", err)
	}

	// set IBM cloud provider name
	ccmOptions.KubeCloudShared.CloudProvider.Name = ibm.ProviderName

	// IBM cloud does not need the "route" implementation
	controllerInitializers := app.DefaultInitFuncConstructors
	delete(controllerInitializers, "route")

	fss := cliflag.NamedFlagSets{}
	command := NewCloudControllerManagerCommand(ccmOptions, IBMCloudInitializer, controllerInitializers, fss, wait.NeverStop)

	// TODO: once we switch everything over to Cobra commands, we can go back to calling
	// utilflag.InitFlags() (by removing its pflag.Parse() call). For now, we have to set the
	// normalize func and add the go flag set by hand.
	// Here is an sample
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	// utilflag.InitFlags()
	logs.InitLogs()
	defer logs.FlushLogs()

	if err := command.Execute(); err != nil {
		os.Exit(1)
	}
}

func NewCloudControllerManagerCommand(s *options.CloudControllerManagerOptions, cloudInitializer app.InitCloudFunc, initFuncConstructor map[string]app.InitFuncConstructor, additionalFlags cliflag.NamedFlagSets, stopCh <-chan struct{}) *cobra.Command {
	cmd := &cobra.Command{
		Use: "ibm-cloud-controller-manager",
		Long: `The IBM Cloud controller manager is a daemon that embeds
the cloud specific control loops shipped with Kubernetes.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ibm.PrintVersionAndExitIfRequested()
			cliflag.PrintFlags(cmd.Flags())

			c, err := s.Config(app.ControllerNames(initFuncConstructor), app.ControllersDisabledByDefault.List())
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				return err
			}

			completedConfig := c.Complete()
			cloud := cloudInitializer(completedConfig)
			controllerInitializers := app.ConstructControllerInitializers(initFuncConstructor, completedConfig, cloud)

			if err := app.Run(completedConfig, cloud, controllerInitializers, stopCh); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
				return err
			}
			return nil
		},
		Args: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if len(arg) > 0 {
					return fmt.Errorf("%q does not take any arguments, got %q", cmd.CommandPath(), args)
				}
			}
			return nil
		},
	}

	fs := cmd.Flags()
	namedFlagSets := s.Flags(app.ControllerNames(initFuncConstructor), app.ControllersDisabledByDefault.List())
	ibm.AddVersionFlag(namedFlagSets.FlagSet("global"))
	globalflag.AddGlobalFlags(namedFlagSets.FlagSet("global"), cmd.Name())

	for _, f := range namedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}
	for _, f := range additionalFlags.FlagSets {
		fs.AddFlagSet(f)
	}

	usageFmt := "Usage:\n  %s\n"
	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		fmt.Fprintf(cmd.OutOrStderr(), usageFmt, cmd.UseLine())
		cliflag.PrintSections(cmd.OutOrStderr(), namedFlagSets, cols)
		return nil
	})
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n\n"+usageFmt, cmd.Long, cmd.UseLine())
		cliflag.PrintSections(cmd.OutOrStdout(), namedFlagSets, cols)
	})

	return cmd
}

func IBMCloudInitializer(config *config.CompletedConfig) cloudprovider.Interface {
	cloudConfig := config.ComponentConfig.KubeCloudShared.CloudProvider

	// initialize cloud provider with the cloud provider name and config file provided
	cloud, err := cloudprovider.InitCloudProvider(cloudConfig.Name, cloudConfig.CloudConfigFile)
	if err != nil {
		klog.Fatalf("IBM Cloud provider could not be initialized: %v", err)
	}
	if cloud == nil {
		klog.Fatalf("IBM Cloud provider is nil")
	}

	return cloud
}
