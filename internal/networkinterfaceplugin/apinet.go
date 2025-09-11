// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package networkinterfaceplugin

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/pflag"

	apinetv1alpha1 "github.com/ironcore-dev/ironcore-net/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/apinetwatcher"
	providernetworkinterface "github.com/ironcore-dev/libvirt-provider/internal/plugins/networkinterface"
	"github.com/ironcore-dev/libvirt-provider/internal/plugins/networkinterface/apinet"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme = runtime.NewScheme()

const PluginAPINet = "apinet"

func init() {
	utilruntime.Must(apinetv1alpha1.AddToScheme(scheme))
	utilruntime.Must(DefaultPluginTypeRegistry.Register(&ApinetOptions{}, 1))
}

type ApinetOptions struct {
	APInetNodeName         string
	ApinetKubeconfig       string
	APInetCleanup          bool
	APInetCacheSyncTimeout time.Duration

	MellanoxVirtFnMetrics bool

	DPSvcMetricsV2Format bool
}

func (o *ApinetOptions) PluginName() string {
	return PluginAPINet
}

func (o *ApinetOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.APInetNodeName, "apinet-node-name", "", "APInet node name")
	fs.StringVar(&o.ApinetKubeconfig, "apinet-kubeconfig", "", "Path to the kubeconfig file for the apinet-cluster.")
	fs.BoolVar(&o.APInetCleanup, "apinet-cleanup", false, "Cleanup orphan apinet interfaces during startup.")
	fs.DurationVar(&o.APInetCacheSyncTimeout, "apinet-cache-sync-timeout", 30*time.Second, "Timeout for apinet kubernetes client cache synchronization. Set this based on the expected cache size.")
	fs.BoolVar(&o.MellanoxVirtFnMetrics, "apinet-mellanox-virtfn-metrics", false, "Expose assignment of mellanox virtual functions to machine.")
	fs.BoolVar(&o.DPSvcMetricsV2Format, "apinet-dpservice-metrics-v2-format", true, "Generate virtual function name label in v2 format.")
}

func (o *ApinetOptions) NetworkInterfacePlugin() (providernetworkinterface.Plugin, apinetwatcher.Watcher, error) {
	if o.APInetNodeName == "" {
		return nil, nil, fmt.Errorf("must specify apinet-node-name")
	}

	// Check if apinetKubeconfig is provided
	var apinetCfg *rest.Config
	var err error
	if o.ApinetKubeconfig != "" {
		apinetCfg, err = clientcmd.BuildConfigFromFlags("", o.ApinetKubeconfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create config from apinet-kubeconfig: %w", err)
		}
	} else {
		// assuming in-cluster config
		apinetCfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create apinet in-cluster-config: %w", err)
		}
	}

	apinetClient, cache, err := o.createAPINetClientAndCache(apinetCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create apinet client and cache: %w", err)
	}

	watcher, err := apinetwatcher.NewWatcher(context.Background(), cache, o.APInetCacheSyncTimeout)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create apinet watcher: %w", err)
	}

	return apinet.NewPlugin(
			o.APInetNodeName,
			apinetClient,
			o.APInetCleanup,
			o.MellanoxVirtFnMetrics,
			o.DPSvcMetricsV2Format,
		),
		watcher,
		nil
}

func (o *ApinetOptions) createAPINetClientAndCache(apinetCfg *rest.Config) (client.Client, cache.Cache, error) {
	c, err := cache.New(apinetCfg, cache.Options{
		Scheme:                      scheme,
		DefaultEnableWatchBookmarks: ptr.To(false),
		DefaultLabelSelector: labels.SelectorFromSet(labels.Set{
			api.LabelLibvirtProviderHostname: o.APInetNodeName,
		}),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cache: %w", err)
	}

	apinetClient, err := client.New(apinetCfg, client.Options{Scheme: scheme, Cache: &client.CacheOptions{Reader: c}})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize api-net client: %w", err)
	}

	return apinetClient, c, nil
}
