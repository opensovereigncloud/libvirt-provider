// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	goflag "flag"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/ironcore-dev/ironcore-image/oci/remote"
	"github.com/ironcore-dev/ironcore/broker/common"
	commongrpc "github.com/ironcore-dev/ironcore/broker/common/grpc"
	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/console"
	"github.com/ironcore-dev/libvirt-provider/internal/controllers"
	"github.com/ironcore-dev/libvirt-provider/internal/event"
	"github.com/ironcore-dev/libvirt-provider/internal/event/machineevent"
	"github.com/ironcore-dev/libvirt-provider/internal/healthcheck"
	"github.com/ironcore-dev/libvirt-provider/internal/host"
	"github.com/ironcore-dev/libvirt-provider/internal/libvirt/guest"
	libvirtutils "github.com/ironcore-dev/libvirt-provider/internal/libvirt/utils"
	"github.com/ironcore-dev/libvirt-provider/internal/mcr"
	"github.com/ironcore-dev/libvirt-provider/internal/networkinterfaceplugin"
	"github.com/ironcore-dev/libvirt-provider/internal/oci"
	volumeplugin "github.com/ironcore-dev/libvirt-provider/internal/plugins/volume"
	"github.com/ironcore-dev/libvirt-provider/internal/plugins/volume/ceph"
	"github.com/ironcore-dev/libvirt-provider/internal/plugins/volume/emptydisk"
	"github.com/ironcore-dev/libvirt-provider/internal/qcow2"
	"github.com/ironcore-dev/libvirt-provider/internal/raw"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/manager"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/sources"
	"github.com/ironcore-dev/libvirt-provider/internal/server"
	"github.com/ironcore-dev/libvirt-provider/internal/strategy"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	homeDir string
)

func init() {
	homeDir, _ = os.UserHomeDir()
}

type Options struct {
	Address          string
	StreamingAddress string
	BaseURL          string

	Servers ServersOptions

	RootDir string

	PathSupportedMachineClasses string
	ResyncIntervalVolumeSize    time.Duration

	GuestAgent GuestAgentOption

	Libvirt   LibvirtOptions
	NicPlugin *networkinterfaceplugin.Options

	GCVMGracefulShutdownTimeout    time.Duration
	ResyncIntervalGarbageCollector time.Duration

	ResourceManagerOptions sources.Options

	MachineEventStore machineevent.EventStoreOptions

	VolumeCachePolicy string
}

type HTTPServerOptions struct {
	Addr            string
	GracefulTimeout time.Duration
}

type ServersOptions struct {
	Metrics     HTTPServerOptions
	HealthCheck HTTPServerOptions
	PPROF       HTTPServerOptions
}

type LibvirtOptions struct {
	Socket  string
	Address string
	URI     string

	PreferredDomainTypes  []string
	PreferredMachineTypes []string

	Qcow2Type string
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Address, "address", "/var/run/iri-machinebroker.sock", "Address to listen on.")
	fs.StringVar(&o.RootDir, "libvirt-provider-dir", filepath.Join(homeDir, ".libvirt-provider"), "Path to the directory libvirt-provider manages its content at.")

	fs.StringVar(&o.PathSupportedMachineClasses, "supported-machine-classes", o.PathSupportedMachineClasses, "File containing supported machine classes.")
	fs.DurationVar(&o.ResyncIntervalVolumeSize, "volume-size-resync-interval", 1*time.Minute, "Interval to determine volume size changes.")

	fs.StringVar(&o.StreamingAddress, "streaming-address", ":20251", "Address to run the streaming server on")
	fs.StringVar(&o.BaseURL, "base-url", "", "The base url to construct urls for streaming from. If empty it will be "+
		"constructed from the streaming-address")

	fs.StringVar(&o.Servers.Metrics.Addr, "servers-metrics-address", "", "Address to listen on exposing of metrics. If address isn't set, server is disabled.")
	fs.DurationVar(&o.Servers.Metrics.GracefulTimeout, "servers-metrics-gracefultimeout", 2*time.Second, "Graceful timeout for shutdown metrics server.")

	fs.StringVar(&o.Servers.HealthCheck.Addr, "servers-health-check-address", ":8181", "Address to listen on health check liveness.")
	fs.DurationVar(&o.Servers.HealthCheck.GracefulTimeout, "servers-health-check-gracefultimeout", 2*time.Second, "Graceful timeout for shutdown health check server.")

	fs.StringVar(&o.Servers.PPROF.Addr, "servers-pprof-address", "", "Address to listen on exposing of pprof. If address isn't set, server is disabled.")
	fs.DurationVar(&o.Servers.PPROF.GracefulTimeout, "servers-pprof-gracefultimeout", 2*time.Second, "Graceful timeout for shutdown pprof server.")

	fs.Var(&o.GuestAgent, "guest-agent-type", fmt.Sprintf("Guest agent implementation to use. Available: %v", guestAgentOptionAvailable()))

	// LibvirtOptions
	fs.StringVar(&o.Libvirt.Socket, "libvirt-socket", o.Libvirt.Socket, "Path to the libvirt socket to use.")
	fs.StringVar(&o.Libvirt.Address, "libvirt-address", o.Libvirt.Address, "Address of a RPC libvirt socket to connect to.")
	fs.StringVar(&o.Libvirt.URI, "libvirt-uri", o.Libvirt.URI, "URI to connect to inside the libvirt system.")

	// Guest Capabilities
	fs.StringSliceVar(&o.Libvirt.PreferredDomainTypes, "preferred-domain-types", []string{"kvm", "qemu"}, "Ordered list of preferred domain types to use.")
	fs.StringSliceVar(&o.Libvirt.PreferredMachineTypes, "preferred-machine-types", []string{"pc-q35"}, "Ordered list of preferred machine types to use.")

	fs.StringVar(&o.Libvirt.Qcow2Type, "qcow2-type", qcow2.Default(), fmt.Sprintf("qcow2 implementation to use. Available: %v", qcow2.Available()))

	fs.DurationVar(&o.GCVMGracefulShutdownTimeout, "gc-vm-graceful-shutdown-timeout", 5*time.Minute, "Duration to wait for the VM to gracefully shut down. If the VM does not shut down within this period, it will be forcibly destroyed by garbage collector.")
	fs.DurationVar(&o.ResyncIntervalGarbageCollector, "gc-resync-interval", 1*time.Minute, "Interval for resynchronizing the garbage collector.")

	fs.StringSliceVar(&o.ResourceManagerOptions.Sources, "resource-manager-sources", []string{"cpu", "memory"}, fmt.Sprintf("Sources for loading resources. Available: %v", manager.GetSourcesAvailable()))
	fs.Float64Var(&o.ResourceManagerOptions.OvercommitVCPU, "resource-manager-overcommit-vcpu", 1.0, "Sets the overcommit ratio for vCPUs, enabling higher VM density per CPU core.")
	fs.Uint64Var(&o.ResourceManagerOptions.BlockedHugepages, "resource-manager-blocked-hugepages", 0, "Count of hugepages which aren't use for vms. Effective only if hugepages source is set")
	fs.Var(&o.ResourceManagerOptions.ReservedMemorySize, "resource-manager-reserved-memory-size", "Size of memory which aren't use for vms in human-readable format. Effective only if memory source is set")
	fs.Uint64Var(&o.ResourceManagerOptions.VMLimit, "resource-manager-vm-limit", 0, "Maximum number of the VMs to be created on the host")
	fs.StringVar(&o.ResourceManagerOptions.PCIDevicesFile, "resource-manager-pci-devices-file", "", "yaml file with list of supported pci devices for pci source.")

	// Machine event store options
	fs.IntVar(&o.MachineEventStore.MachineEventMaxEvents, "machine-event-max-events", 100, "Maximum number of machine events that can be stored.")
	fs.DurationVar(&o.MachineEventStore.MachineEventTTL, "machine-event-ttl", 5*time.Minute, "Time to live for machine events.")
	fs.DurationVar(&o.MachineEventStore.MachineEventResyncInterval, "machine-event-resync-interval", 1*time.Minute, "Interval for resynchronizing the machine events.")

	// Volume cache policy option
	fs.StringVar(&o.VolumeCachePolicy, "volume-cache-policy", "none",
		`Policy to use when creating a remote disk. (one of 'none', 'writeback', 'writethrough', 'directsync', 'unsafe').
Note: The available options may depend on the hypervisor and libvirt version in use.
Please refer to the official documentation for more details: https://libvirt.org/formatdomain.html#hard-drives-floppy-disks-cdroms.`)

	o.NicPlugin = networkinterfaceplugin.NewDefaultOptions()
	o.NicPlugin.AddFlags(fs)
}

func (o *Options) MarkFlagsRequired(cmd *cobra.Command) {
	_ = cmd.MarkFlagRequired("supported-machine-classes")
}

func Command() *cobra.Command {
	var (
		zapOpts = zap.Options{Development: true}
		opts    Options
	)

	cmd := &cobra.Command{
		Use: "libvirt-provider",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logger := zap.New(zap.UseFlagOptions(&zapOpts))
			ctrl.SetLogger(logger)
			cmd.SetContext(ctrl.LoggerInto(cmd.Context(), ctrl.Log))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			//flag parsing is done therefore we can silence the usage message
			cmd.SilenceUsage = true
			//error logging is done in the main
			cmd.SilenceErrors = true
			return Run(cmd.Context(), opts)
		},
	}

	goFlags := goflag.NewFlagSet("", 0)
	zapOpts.BindFlags(goFlags)
	cmd.PersistentFlags().AddGoFlagSet(goFlags)

	opts.AddFlags(cmd.Flags())
	opts.MarkFlagsRequired(cmd)

	return cmd
}

func Run(ctx context.Context, opts Options) error {
	log := ctrl.LoggerFrom(ctx)
	setupLog := log.WithName("setup")

	// Setup Libvirt Client
	libvirt, err := libvirtutils.GetLibvirt(opts.Libvirt.Socket, opts.Libvirt.Address, opts.Libvirt.URI)
	if err != nil {
		setupLog.Error(err, "failed to initialize libvirt")
		return err
	}
	defer func() {
		if err := libvirt.ConnectClose(); err != nil {
			setupLog.Error(err, "failed to close libvirt connection")
		}
	}()

	baseURL := opts.BaseURL
	if baseURL == "" {
		u := &url.URL{
			Scheme: "http",
			Host:   opts.StreamingAddress,
		}
		baseURL = u.String()
	}

	providerHost, err := host.NewLibvirtAt(opts.RootDir, libvirt)
	if err != nil {
		setupLog.Error(err, "failed to initialize provider host")
		return err
	}

	reg, err := remote.DockerRegistry(nil)
	if err != nil {
		setupLog.Error(err, "failed to initialize registry")
		return err
	}

	imgCache, err := oci.NewLocalCache(log, reg, providerHost.OCIStore())
	if err != nil {
		setupLog.Error(err, "failed to initialize oci manager")
		return err
	}

	qcow2Inst, err := qcow2.Instance(opts.Libvirt.Qcow2Type)
	if err != nil {
		setupLog.Error(err, "failed to initialize qcow2 instance")
		return err
	}

	rawInst, err := raw.Instance(raw.Default())
	if err != nil {
		setupLog.Error(err, "failed to initialize raw instance")
		return err
	}

	// Detect Guest Capabilities
	caps, err := guest.DetectCapabilities(libvirt, guest.CapabilitiesOptions{
		PreferredDomainTypes:  opts.Libvirt.PreferredDomainTypes,
		PreferredMachineTypes: opts.Libvirt.PreferredMachineTypes,
	})
	if err != nil {
		setupLog.Error(err, "failed to detect guest capabilities")
		return err
	}

	volumePlugins := volumeplugin.NewPluginManager()
	if err := volumePlugins.InitPlugins(providerHost, []volumeplugin.Plugin{
		ceph.NewPlugin(),
		emptydisk.NewPlugin(qcow2Inst, rawInst),
	}); err != nil {
		setupLog.Error(err, "failed to initialize volume plugin manager")
		return err
	}

	nicPlugin, nicPluginCleanup, err := opts.NicPlugin.NetworkInterfacePlugin()
	if err != nil {
		setupLog.Error(err, "failed to initialize network plugin")
		return err
	}
	if nicPluginCleanup != nil {
		defer nicPluginCleanup()
	}

	setupLog.Info("Initializing network interface plugin")

	if err := nicPlugin.Init(providerHost); err != nil {
		setupLog.Error(err, "failed to initialize network plugin")
		return err
	}

	setupLog.Info("Configuring machine store", "Directory", providerHost.MachineStoreDir())
	machineStore, err := host.NewStore(host.Options[*api.Machine]{
		NewFunc:        func() *api.Machine { return &api.Machine{} },
		CreateStrategy: strategy.MachineStrategy,
		Dir:            providerHost.MachineStoreDir(),
	})
	if err != nil {
		setupLog.Error(err, "failed to initialize machine store")
		return err
	}

	errs := machineStore.CleanupSwapFiles()
	// these errors don't affect bussines logic
	if len(errs) > 0 {
		for _, err := range errs {
			setupLog.Error(err, "failed to remove all swap files from machine store")
		}

		return fmt.Errorf("failed to cleanup machine store")
	}

	err = initResourceManager(ctx, opts.ResourceManagerOptions, machineStore, opts.PathSupportedMachineClasses)
	if err != nil {
		setupLog.Error(err, "failed to initialize resource manager")
		return err
	}

	machineClasses, err := mcr.NewMachineClassRegistry(manager.GetIRIMachineClasses())
	if err != nil {
		setupLog.Error(err, "failed to initialize machine class registry")
		return err
	}

	machineEvents, err := event.NewListWatchSource[*api.Machine](
		machineStore.List,
		machineStore.Watch,
		event.ListWatchSourceOptions{},
	)
	if err != nil {
		setupLog.Error(err, "failed to initialize machine events")
		return err
	}

	eventStore := machineevent.NewEventStore(log, opts.MachineEventStore)

	machineReconciler, err := controllers.NewMachineReconciler(
		log.WithName("machine-reconciler"),
		libvirt,
		machineStore,
		machineEvents,
		eventStore,
		controllers.MachineReconcilerOptions{
			GuestCapabilities:              caps,
			ImageCache:                     imgCache,
			Raw:                            rawInst,
			Host:                           providerHost,
			VolumePluginManager:            volumePlugins,
			NetworkInterfacePlugin:         nicPlugin,
			ResyncIntervalVolumeSize:       opts.ResyncIntervalVolumeSize,
			ResyncIntervalGarbageCollector: opts.ResyncIntervalGarbageCollector,
			GCVMGracefulShutdownTimeout:    opts.GCVMGracefulShutdownTimeout,
			VolumeCachePolicy:              opts.VolumeCachePolicy,
		},
	)
	if err != nil {
		setupLog.Error(err, "failed to initialize machine controller")
		return err
	}

	srv, err := server.New(server.Options{
		BaseURL:        baseURL,
		Libvirt:        libvirt,
		MachineStore:   machineStore,
		EventStore:     eventStore,
		MachineClasses: machineClasses,
		VolumePlugins:  volumePlugins,
		NetworkPlugins: nicPlugin,
		GuestAgent:     opts.GuestAgent.GetAPIGuestAgent(),
	})
	if err != nil {
		setupLog.Error(err, "failed to initialize server")
		return err
	}

	healthCheck := healthcheck.HealthCheck{
		Libvirt: libvirt,
		Log:     log.WithName("health-check"),
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return runMetricsServer(ctx, setupLog, opts.Servers.Metrics)
	})

	g.Go(func() error {
		setupLog.Info("Starting oci cache")
		if err := imgCache.Start(ctx); err != nil {
			setupLog.Error(err, "failed to start oci cache")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting machine reconciler")
		if err := machineReconciler.Start(ctx); err != nil {
			setupLog.Error(err, "failed to start machine reconciler")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting machine events")
		if err := machineEvents.Start(ctx); err != nil {
			setupLog.Error(err, "failed to start machine events")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting machine events garbage collector")
		eventStore.Start(ctx)
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting grpc server")
		if err := runGRPCServer(ctx, setupLog, log, srv, opts); err != nil {
			setupLog.Error(err, "failed to start grpc server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting streaming server")
		if err := runStreamingServer(ctx, setupLog, log, srv, opts); err != nil {
			setupLog.Error(err, "failed to start streaming server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting health check server")
		if err := runHealthCheckServer(ctx, setupLog, healthCheck, opts.Servers.HealthCheck); err != nil {
			setupLog.Error(err, "failed to start health check server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting pprof server")
		if err := runPPROFServer(ctx, setupLog, opts.Servers.PPROF); err != nil {
			setupLog.Error(err, "failed to start pprof server")
			return err
		}
		return nil
	})

	return g.Wait()
}

func runGRPCServer(ctx context.Context, setupLog logr.Logger, log logr.Logger, srv *server.Server, opts Options) error {
	setupLog.V(1).Info("Cleaning up any previous socket")
	if err := common.CleanupSocketIfExists(opts.Address); err != nil {
		return fmt.Errorf("error cleaning up socket: %w", err)
	}

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			commongrpc.InjectLogger(log.WithName("iri-server")),
			commongrpc.LogRequest,
		),
	)
	iri.RegisterMachineRuntimeServer(grpcSrv, srv)

	setupLog.V(1).Info("Start listening on unix socket", "Address", opts.Address)
	l, err := net.Listen("unix", opts.Address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	setupLog.Info("Starting grpc server", "Address", l.Addr().String())
	go func() {
		<-ctx.Done()
		setupLog.Info("Shutting down grpc server")
		grpcSrv.GracefulStop()
		setupLog.Info("GRPC server is shutdown")
	}()
	if err := grpcSrv.Serve(l); err != nil {
		return fmt.Errorf("error serving grpc: %w", err)
	}
	return nil
}

func runStreamingServer(ctx context.Context, setupLog, log logr.Logger, srv *server.Server, opts Options) error {
	httpHandler := console.NewHandler(srv, console.HandlerOptions{
		Log: log.WithName("streaming-server"),
	})

	httpSrv := &http.Server{
		Addr:    opts.StreamingAddress,
		Handler: httpHandler,
	}

	go func() {
		<-ctx.Done()
		setupLog.Info("Shutting down streaming server")
		_ = httpSrv.Close()
		setupLog.Info("Streaming server is shutdown")
	}()

	setupLog.V(1).Info("Starting streaming server", "Address", opts.StreamingAddress)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error listening / serving streaming server: %w", err)
	}
	return nil
}

func runMetricsServer(ctx context.Context, setupLog logr.Logger, opts HTTPServerOptions) error {
	if opts.Addr == "" {
		setupLog.Info("Metrics server address isn't configured. Metrics server is disabled.")
		return nil
	}

	setupLog.Info("Starting metrics server on " + opts.Addr)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := http.Server{
		Addr:    opts.Addr,
		Handler: mux,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		setupLog.Info("Shutting down metrics server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.GracefulTimeout)
		defer cancel()
		locErr := srv.Shutdown(shutdownCtx)
		if locErr != nil {
			setupLog.Error(locErr, "metrics server wasn't shutdown properly")
		} else {
			setupLog.Info("Metrics server is shutdown")
		}
	}()

	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error listening / serving metrics server: %w", err)
	}

	setupLog.Info("Metrics server stopped serve new connections")

	wg.Wait()

	return nil
}

func runPPROFServer(ctx context.Context, setupLog logr.Logger, opts HTTPServerOptions) error {
	if opts.Addr == "" {
		setupLog.Info("pprof server address isn't configured. pprof server is disabled.")
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	srv := http.Server{
		Addr:    opts.Addr,
		Handler: mux,
	}

	setupLog.Info("Starting pprof server on " + opts.Addr)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		setupLog.Info("Shutting down pprof server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.GracefulTimeout)
		defer cancel()
		locErr := srv.Shutdown(shutdownCtx)
		if locErr != nil {
			setupLog.Error(locErr, "pprof server wasn't shutdown properly")
		} else {
			setupLog.Info("pprof server is shutdown")
		}
	}()

	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error listening / serving pprof server: %w", err)
	}

	setupLog.Info("pprof server stopped serve new connections")

	wg.Wait()

	return nil
}

func runHealthCheckServer(ctx context.Context, setupLog logr.Logger, healthCheck healthcheck.HealthCheck, opts HTTPServerOptions) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthCheck.HealthCheckHandler)

	srv := http.Server{
		Addr:    opts.Addr,
		Handler: mux,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		setupLog.Info("Shutting down health check server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.GracefulTimeout)
		defer cancel()
		locErr := srv.Shutdown(shutdownCtx)
		if locErr != nil {
			setupLog.Error(locErr, "health check server wasn't shutdown properly")
		} else {
			setupLog.Info("Health check server is shutdown")
		}
	}()

	setupLog.V(1).Info("Starting health check server", "Address", opts.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error listening / serving health check server: %w", err)
	}

	wg.Wait()

	return nil
}

func initResourceManager(ctx context.Context, opts sources.Options, machineStore *host.Store[*api.Machine], filename string) error {
	err := manager.ValidateOptions(opts)
	if err != nil {
		return err
	}

	for _, sourceName := range opts.Sources {
		source, err := manager.GetSource(sourceName, opts)
		if err != nil {
			return err
		}

		err = manager.AddSource(source)
		if err != nil {
			return err
		}
	}

	err = manager.SetMachineClassesFilename(filename)
	if err != nil {
		return err
	}

	err = manager.SetVMLimit(opts.VMLimit)
	if err != nil {
		return err
	}

	err = manager.SetLogger(ctrl.Log)
	if err != nil {
		return err
	}

	return manager.Initialize(ctx, machineStore.List)
}
