// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package networkinterfaceplugin

import (
	"fmt"

	"github.com/spf13/pflag"

	apinetv1alpha1 "github.com/ironcore-dev/ironcore-net/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/internal/apinetwatcher"
	providernetworkinterface "github.com/ironcore-dev/libvirt-provider/internal/plugins/networkinterface"
	"github.com/ironcore-dev/libvirt-provider/internal/plugins/networkinterface/apinet"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme = runtime.NewScheme()

const PluginAPINet = "apinet"

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apinetv1alpha1.AddToScheme(scheme))
}

type ApinetOptions struct {
	APInetNodeName   string
	ApinetKubeconfig string
	APInetCleanup    bool

	MellanoxVirtFnMetrics bool

	DPSvcMetricsV2Format bool

	watcher apinetwatcher.Watcher
}

func (o *ApinetOptions) SetWatcher(watcher apinetwatcher.Watcher) {
	o.watcher = watcher
}

func (o *ApinetOptions) PluginName() string {
	return PluginAPINet
}

func (o *ApinetOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.APInetNodeName, "apinet-node-name", "", "APInet node name")
	fs.StringVar(&o.ApinetKubeconfig, "apinet-kubeconfig", "", "Path to the kubeconfig file for the apinet-cluster.")
	fs.BoolVar(&o.APInetCleanup, "apinet-cleanup", false, "Cleanup orphan apinet interfaces during startup.")
	fs.BoolVar(&o.MellanoxVirtFnMetrics, "apinet-mellanox-virtfn-metrics", false, "Expose assignment of mellanox virtual functions to machine.")
	fs.BoolVar(&o.DPSvcMetricsV2Format, "apinet-dpservice-metrics-v2-format", true, "Generate virtual function name label in v2 format.")
}

func (o *ApinetOptions) NetworkInterfacePlugin() (providernetworkinterface.Plugin, error) {
	if o.APInetNodeName == "" {
		return nil, fmt.Errorf("must specify apinet-node-name")
	}

	// Check if apinetKubeconfig is provided
	var apinetCfg *rest.Config
	var err error
	if o.ApinetKubeconfig != "" {
		apinetCfg, err = clientcmd.BuildConfigFromFlags("", o.ApinetKubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create config from apinet-kubeconfig: %w", err)
		}
	} else {
		// assuming in-cluster config
		apinetCfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create apinet in-cluster-config: %w", err)
		}
	}

	apinetClient, err := client.New(apinetCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize api-net client: %w", err)
	}

	if o.watcher != nil {
		o.watcher.SetNodeName(o.APInetNodeName)
		o.watcher.SetAPINetConfig(apinetCfg)
	}

	return apinet.NewPlugin(o.APInetNodeName, apinetClient, o.APInetCleanup,
		o.MellanoxVirtFnMetrics, o.DPSvcMetricsV2Format, o.watcher), nil
}

func init() {
	utilruntime.Must(DefaultPluginTypeRegistry.Register(&ApinetOptions{}, 1))
}
