// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package apinet

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	apinetv1alpha1 "github.com/ironcore-dev/ironcore-net/api/core/v1alpha1"
	apinet "github.com/ironcore-dev/ironcore-net/apimachinery/api/net"
	"github.com/ironcore-dev/ironcore-net/apinetlet/provider"
	"github.com/ironcore-dev/libvirt-provider/api"
	providerhost "github.com/ironcore-dev/libvirt-provider/internal/host"
	"github.com/ironcore-dev/libvirt-provider/internal/metrics"
	providernetworkinterface "github.com/ironcore-dev/libvirt-provider/internal/plugins/networkinterface"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"
	"github.com/prometheus/client_golang/prometheus"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	fieldOwner = client.FieldOwner("networking.ironcore.dev/" + api.MachineManager)

	defaultAPINetConfigFile = "api-net.json"

	labelMachineDownwardRootMachineName      = "downward-api.machinepoollet.ironcore.dev/root-machine-name"
	labelMachineDownwardRootMachineNamespace = "downward-api.machinepoollet.ironcore.dev/root-machine-namespace"

	permFile   = 0o640
	permFolder = 0o750

	pluginAPInet = "apinet"

	mellanoxVirtFnMaxCount = 128

	logKeyNic = "nic"
)

var (
	ErrWaitingForNetworkInterface = errors.New("waiting for apinet network interface readiness or deletion")
	ErrMetricRetypeToGauge        = errors.New("failed to retype to prometheus.Gauge")
	ErrMetricNoDeleted            = errors.New("no metric deleted")
)

type VirtualFunction struct {
	ID         string
	ParentAddr string
}

type Plugin struct {
	nodeName                      string
	host                          providerhost.LibvirtHost
	apinetClient                  client.Client
	enableCleanup                 bool
	mellanoxVirtFnMetricsDisabled bool
	virtualFunctions              map[string]VirtualFunction
	generateVirtFnLabel           func(VirtualFunction) string
	metricsRegister               sync.Map
}

func NewPlugin(nodeName string, client client.Client,
	cleanup, mellanoxVirtFnMetrics, dpSvcMetricsV2Format bool) providernetworkinterface.Plugin {
	generateFunc := generateDPSvcFormatV1Label
	if dpSvcMetricsV2Format {
		generateFunc = generateDPSvcFormatV2Label
	}

	return &Plugin{
		nodeName:                      nodeName,
		apinetClient:                  client,
		enableCleanup:                 cleanup,
		mellanoxVirtFnMetricsDisabled: !mellanoxVirtFnMetrics,
		generateVirtFnLabel:           generateFunc,
	}
}

func GetAPInetPlugin() *Plugin {
	return &Plugin{}
}

func NICName(machineID, networkInterfaceName string) string {
	return uuid.NewHash(sha256.New(), uuid.Nil, []byte(fmt.Sprintf("%s/%s", machineID, networkInterfaceName)), 5).String()
}

func (p *Plugin) Init(ctx context.Context, host providerhost.LibvirtHost) error {
	p.host = host
	err := p.loadVirtFnAddress()
	if err != nil {
		return err
	}

	return p.cleanup(ctx)
}

func (p *Plugin) cleanup(ctx context.Context) error {
	if !p.enableCleanup {
		return nil
	}

	log := ctrl.Log.WithName(pluginAPInet)
	log.Info("starting apinet cleanup")

	selector, err := labels.ValidatedSelectorFromSet(p.getLibvirtProviderLabel())
	if err != nil {
		return fmt.Errorf("failed to create selector for list nics: %w", err)
	}
	nicsList := apinetv1alpha1.NetworkInterfaceList{}
	err = p.apinetClient.List(ctx, &nicsList, &client.ListOptions{Namespace: metav1.NamespaceAll, LabelSelector: selector})
	if err != nil {
		return err
	}

	if len(nicsList.Items) == 0 {
		return nil
	}

	nicNames, err := p.loadInterfaces()
	if err != nil {
		return err
	}

	log.V(1).Info(fmt.Sprintf("totally loaded local/remote nics: %d/%d", len(nicNames), len(nicsList.Items)))

	var deleteErrs error
	for _, nic := range nicsList.Items {
		if nicNames.Has(nic.GetName()) {
			continue
		}

		log.Info("deleting nic " + nic.GetNamespace() + "/" + nic.GetName())
		err = p.apinetClient.Delete(ctx, &nic)
		if err != nil {
			deleteErrs = errors.Join(deleteErrs, fmt.Errorf("failed to delete nic %s/%s: %w", nic.GetNamespace(), nic.GetName(), err))
		}
	}

	return deleteErrs
}

func (p *Plugin) loadInterfaces() (sets.Set[string], error) {
	nicsNames := sets.New[string]()
	machineDirs, err := os.ReadDir(p.host.MachinesDir())
	if err != nil {
		return nil, fmt.Errorf("failed to load machines dir: %w", err)
	}

	for _, machineDir := range machineDirs {
		if !machineDir.IsDir() {
			continue
		}

		infDirs, err := os.ReadDir(p.host.MachineNetworkInterfacesDir(machineDir.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to load interfaces for machine %s: %w", machineDir.Name(), err)
		}

		for _, infDir := range infDirs {
			if infDir.IsDir() {
				nicsNames.Insert(NICName(machineDir.Name(), infDir.Name()))
			}
		}
	}

	return nicsNames, nil
}

func ironcoreIPsToAPInetIPs(ips []string) []apinet.IP {
	res := make([]apinet.IP, len(ips))
	for i, ip := range ips {
		res[i] = apinet.MustParseIP(ip)
	}
	return res
}

type apiNetNetworkInterfaceConfig struct {
	Namespace string `json:"namespace"`
}

func (p *Plugin) apiNetNetworkInterfaceConfigFile(machineID, networkInterfaceName string) string {
	return filepath.Join(p.host.MachineNetworkInterfaceDir(machineID, networkInterfaceName), defaultAPINetConfigFile)
}

func (p *Plugin) writeAPINetNetworkInterfaceConfig(machineID, networkInterfaceName string, cfg *apiNetNetworkInterfaceConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(p.apiNetNetworkInterfaceConfigFile(machineID, networkInterfaceName), data, permFile)
}

func (p *Plugin) readAPINetNetworkInterfaceConfig(machineID, networkInterfaceName string) (*apiNetNetworkInterfaceConfig, error) {
	data, err := os.ReadFile(p.apiNetNetworkInterfaceConfigFile(machineID, networkInterfaceName))
	if err != nil {
		return nil, err
	}

	cfg := &apiNetNetworkInterfaceConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (p *Plugin) Apply(ctx context.Context, spec *api.NetworkInterfaceSpec, machine *api.Machine) (*providernetworkinterface.NetworkInterface, error) {
	log := ctrl.LoggerFrom(ctx).WithValues(logKeyNic, spec.Name)

	apinetNamespace, apinetNetworkName, _, _, err := provider.ParseNetworkID(spec.NetworkId)
	if err != nil {
		return nil, fmt.Errorf("error parsing ApiNet NetworkID %s: %w", spec.NetworkId, err)
	}

	nicName := NICName(machine.ID, spec.Name)

	providerNic := &providernetworkinterface.NetworkInterface{
		Handle: provider.GetNetworkInterfaceID(
			apinetNamespace,
			nicName,
			p.nodeName,
			types.UID(""),
		),
	}

	log.V(1).Info("Writing network interface dir")
	if err := os.MkdirAll(p.host.MachineNetworkInterfaceDir(machine.ID, spec.Name), permFolder); err != nil {
		return providerNic, err
	}

	log.V(1).Info("Writing APINet network interface config file")
	if err := p.writeAPINetNetworkInterfaceConfig(machine.ID, spec.Name, &apiNetNetworkInterfaceConfig{
		Namespace: apinetNamespace,
	}); err != nil {
		return providerNic, err
	}

	apinetNic := &apinetv1alpha1.NetworkInterface{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apinetv1alpha1.SchemeGroupVersion.String(),
			Kind:       "NetworkInterface",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: apinetNamespace,
			Name:      nicName,
		},
	}

	apinetNicKey := client.ObjectKeyFromObject(apinetNic)

	err = p.apinetClient.Get(ctx, apinetNicKey, apinetNic)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get existing NIC: %w", err)
		}

		apinetNic.Labels = p.getLibvirtProviderLabel()
		apinetNic.Spec = apinetv1alpha1.NetworkInterfaceSpec{
			NetworkRef: corev1.LocalObjectReference{
				Name: apinetNetworkName,
			},
			NodeRef: corev1.LocalObjectReference{
				Name: p.nodeName,
			},
			IPs: ironcoreIPsToAPInetIPs(spec.Ips),
		}

		log.V(1).Info("Creating apinet nic")
		if err := p.apinetClient.Create(ctx, apinetNic, fieldOwner); err != nil {
			return providerNic, fmt.Errorf("error applying apinet network interface: %w", err)
		}

		err = p.resetNicMetrics(machine.ID, spec.Name)
		if err != nil {
			log.Error(err, "failed to reset metrics")
		}

		// nic won't be ready immediately after creation
		return providerNic, ErrWaitingForNetworkInterface
	}

	providerNic.Handle += string(apinetNic.UID)

	if apinetNic.Status.State != apinetv1alpha1.NetworkInterfaceStateReady {
		log.V(1).Info("APINet NIC is not ready. Reconciliation will resume when watcher requeues the machine")
		return providerNic, ErrWaitingForNetworkInterface
	}

	hostDev, direct, err := getHostDevice(apinetNic)
	if err != nil {
		return providerNic, fmt.Errorf("error getting host device: %w", err)
	}

	if hostDev != nil {
		log.V(1).Info("Host device is ready", "HostDevice", hostDev)
		providerNic.HostDevice = hostDev
		err = p.activateNicMetrics(
			machine.ID,
			machine.Labels[labelMachineDownwardRootMachineName],
			machine.Labels[labelMachineDownwardRootMachineNamespace],
			spec.Name,
			hostDev)
		// this error is not business critical
		if err != nil {
			log.Error(err, "failed to update nic virtual function metric")
		}
		return providerNic, nil
	}

	if direct != nil {
		log.V(1).Info("Direct device is ready", "Direct", direct)
		providerNic.Direct = direct
	}

	return providerNic, nil
}

func getHostDevice(apinetNic *apinetv1alpha1.NetworkInterface) (*providernetworkinterface.HostDevice, *providernetworkinterface.Direct, error) {
	switch apinetNic.Status.State {
	case apinetv1alpha1.NetworkInterfaceStateReady:

		switch {
		case apinetNic.Status.PCIAddress == nil && apinetNic.Status.TAPDevice == nil:
			return nil, nil, fmt.Errorf("apinet network interface: PCIAddress and TAPDevice not set")
		case apinetNic.Status.PCIAddress == nil && apinetNic.Status.TAPDevice != nil:
			tapDevice := apinetNic.Status.TAPDevice
			return nil, &providernetworkinterface.Direct{
				Dev: tapDevice.Name,
			}, nil
		case apinetNic.Status.PCIAddress != nil && apinetNic.Status.TAPDevice == nil:
			pciDevice := apinetNic.Status.PCIAddress
			domain, err := strconv.ParseUint(pciDevice.Domain, 16, strconv.IntSize)
			if err != nil {
				return nil, nil, fmt.Errorf("error parsing pci device domain %q: %w", pciDevice.Domain, err)
			}

			bus, err := strconv.ParseUint(pciDevice.Bus, 16, strconv.IntSize)
			if err != nil {
				return nil, nil, fmt.Errorf("error parsing pci device bus %q: %w", pciDevice.Bus, err)
			}

			slot, err := strconv.ParseUint(pciDevice.Slot, 16, strconv.IntSize)
			if err != nil {
				return nil, nil, fmt.Errorf("error parsing pci device slot %q: %w", pciDevice.Slot, err)
			}

			function, err := strconv.ParseUint(pciDevice.Function, 16, strconv.IntSize)
			if err != nil {
				return nil, nil, fmt.Errorf("error parsing pci device function %q: %w", pciDevice.Function, err)
			}

			return &providernetworkinterface.HostDevice{
				Domain:   uint(domain),
				Bus:      uint(bus),
				Slot:     uint(slot),
				Function: uint(function),
			}, nil, nil
		default:
			return nil, nil, fmt.Errorf("apinet network interface: PCIAddress and TAPDevice should not be set at the same time")
		}
	case apinetv1alpha1.NetworkInterfaceStatePending:
		return nil, nil, nil
	case apinetv1alpha1.NetworkInterfaceStateError:
		return nil, nil, fmt.Errorf("apinet network interface is in state error")
	default:
		return nil, nil, nil
	}
}

func (p *Plugin) Delete(ctx context.Context, computeNicName, machineID string) error {
	log := ctrl.LoggerFrom(ctx).WithValues(logKeyNic, computeNicName)

	log.V(1).Info("Reading APINet network interface config file")
	cfg, err := p.readAPINetNetworkInterfaceConfig(machineID, computeNicName)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("error reading namespace file: %w", err)
		}

		log.V(1).Info("No namespace file found, deleting network interface dir")
		return os.RemoveAll(p.host.MachineNetworkInterfaceDir(machineID, computeNicName))
	}

	apinetNicKey := client.ObjectKey{
		Namespace: cfg.Namespace,
		Name:      NICName(machineID, computeNicName),
	}
	log = log.WithValues("APInetNetworkInterfaceKey", apinetNicKey)

	if err := p.apinetClient.Delete(ctx, &apinetv1alpha1.NetworkInterface{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: apinetNicKey.Namespace,
			Name:      apinetNicKey.Name,
		},
	}); err != nil {
		if !apierrors.IsNotFound(err) {
			p.deactivateNicMetrics(&log, machineID, computeNicName)
			return fmt.Errorf("error deleting apinet network interface %s: %w", apinetNicKey, err)
		}

		log.V(1).Info("APInet network interface is already gone, removing network interface directory")
		return os.RemoveAll(p.host.MachineNetworkInterfaceDir(machineID, computeNicName))
	}

	p.deactivateNicMetrics(&log, machineID, computeNicName)

	return nil
}

func (p *Plugin) Name() string {
	return pluginAPInet
}

func (p *Plugin) getLibvirtProviderLabel() map[string]string {
	return map[string]string{api.LabelLibvirtProviderHostname: p.nodeName}
}

func (p *Plugin) loadVirtFnAddress() error {
	log := ctrl.Log.WithName(pluginAPInet)

	if p.mellanoxVirtFnMetricsDisabled {
		log.Info("mellanox virtual function metrics are disabled")
		return nil
	}

	// we don't support more than 128 virtual functions
	p.virtualFunctions = make(map[string]VirtualFunction, mellanoxVirtFnMaxCount)
	const virtFnNamePrefix = "virtfn"
	const virtFnNamePrefixLen = len(virtFnNamePrefix)
	const mellanoxVendor = "0x15b3"
	const mellanoxDevice = "0x101f"

	dirEntries, err := os.ReadDir(internalutils.FolderSysPCIDevices)
	if err != nil {
		return fmt.Errorf("error reading PCI devices: %w", err)
	}

	for _, dir := range dirEntries {
		if dir.Type()&fs.ModeSymlink == 0 {
			continue
		}

		parentAddr := dir.Name()

		devicePath := filepath.Join(internalutils.FolderSysPCIDevices, parentAddr)

		vendorID, err := internalutils.ReadPCIAttribute(&log, devicePath, "vendor")
		if err != nil {
			return fmt.Errorf("failed to load vendor id for pci addr %s: %w", parentAddr, err)
		}

		if vendorID != mellanoxVendor {
			continue
		}

		deviceID, err := internalutils.ReadPCIAttribute(&log, devicePath, "device")
		if err != nil {
			return fmt.Errorf("failed to load device id for pci addr %s: %w", parentAddr, err)
		}

		if deviceID != mellanoxDevice {
			continue
		}

		pciDeviceContent, err := os.ReadDir(devicePath)
		if err != nil {
			return fmt.Errorf("failed to load list of object for pci addr %s: %w", parentAddr, err)
		}

		log.V(1).Info("mellanox card is detected", "pciAddr", parentAddr)

		for _, item := range pciDeviceContent {
			if item.Type()&fs.ModeSymlink == 0 {
				continue
			}

			if !strings.HasPrefix(item.Name(), virtFnNamePrefix) {
				continue
			}

			virtFnPath, err := filepath.EvalSymlinks(filepath.Join(devicePath, item.Name()))
			if err != nil {
				return fmt.Errorf("failed to eval symlinks for virtual function %s of pci device %s: %w", item.Name(), parentAddr, err)
			}

			log.V(1).Info("virtual function is detected", "virtFn", item.Name())
			p.virtualFunctions[filepath.Base(virtFnPath)] = VirtualFunction{ID: item.Name()[virtFnNamePrefixLen:], ParentAddr: parentAddr}
		}
	}

	return nil
}

func (p *Plugin) activateNicMetrics(machineID, rootMachineName, rootMachineNamespace, nicName string,
	hostDev *providernetworkinterface.HostDevice) error {
	if p.mellanoxVirtFnMetricsDisabled {
		return nil
	}

	var metricVirtFn prometheus.Gauge

	metricID := getMetricID(machineID, nicName)
	m, ok := p.metricsRegister.Load(metricID)
	if ok {
		metricVirtFn, ok = m.(prometheus.Gauge)
		if !ok {
			return generateRetypeError(metricID)
		}
	} else {
		nicVF, err := p.getLabelVirtFn(hostDev)
		if err != nil {
			return err
		}

		if rootMachineName == "" {
			rootMachineName = metrics.LabelValueUnknown
		}

		if rootMachineNamespace == "" {
			rootMachineNamespace = metrics.LabelValueUnknown
		}

		metricVirtFn, err = metrics.APINetNicsVirtFn.GetMetricWith(
			prometheus.Labels{
				metrics.LabelName:                 nicVF,
				metrics.LabelMachineID:            machineID,
				metrics.LabelRootMachineName:      rootMachineName,
				metrics.LabelRootMachineNamespace: rootMachineNamespace,
				metrics.LabelNic:                  nicName,
			})
		if err != nil {
			return err
		}

		p.metricsRegister.Store(metricID, metricVirtFn)
	}

	metricVirtFn.Set(1)
	return nil
}

func (p *Plugin) resetNicMetrics(machineID, nicName string) error {
	if p.mellanoxVirtFnMetricsDisabled {
		return nil
	}

	metricID := getMetricID(machineID, nicName)

	m, ok := p.metricsRegister.Load(metricID)
	// metric doesn't exist, we don't need reset it
	if !ok {
		return nil
	}

	metric, ok := m.(prometheus.Gauge)
	if !ok {
		return generateRetypeError(metricID)
	}

	metric.Set(0)
	return nil
}

func (p *Plugin) deactivateNicMetrics(log *logr.Logger, machineID, nicName string) {
	if p.mellanoxVirtFnMetricsDisabled {
		return
	}

	metricID := getMetricID(machineID, nicName)

	_, ok := p.metricsRegister.Load(metricID)
	// metric doesn't exist and it doesn't make sense recreate it
	if !ok {
		return
	}

	// metric has to be deleted because group_left operation require unique labels
	count := metrics.APINetNicsVirtFn.DeletePartialMatch(prometheus.Labels{metrics.LabelMachineID: machineID, metrics.LabelNic: nicName})
	switch count {
	case 0:
		log.Error(ErrMetricNoDeleted, "failed to delete apinet virtual function metric properly")
		return
	case 1:
	default:
		log.Error(fmt.Errorf("invalid count of metrics was deleted %d, expected: 1", count), "failed to delete apinet virtual function metric properly")
	}

	p.metricsRegister.Delete(metricID)
}

func (p *Plugin) getLabelVirtFn(pciAddr *providernetworkinterface.HostDevice) (string, error) {
	pciAddrStr := fmt.Sprintf("%04x:%02x:%02x.%1x", pciAddr.Domain, pciAddr.Bus, pciAddr.Slot, pciAddr.Function)
	virtFn, ok := p.virtualFunctions[pciAddrStr]
	if !ok {
		return "", fmt.Errorf("pci addr %s for virtual function wasn't found in internal registry", pciAddrStr)
	}

	return p.generateVirtFnLabel(virtFn), nil
}

func generateDPSvcFormatV2Label(vf VirtualFunction) string {
	return vf.ParentAddr + "_representor_c0pf0vf" + vf.ID
}

func generateDPSvcFormatV1Label(vf VirtualFunction) string {
	return vf.ParentAddr + "_representor_vf" + vf.ID
}

func getMetricID(machineID, nicName string) string {
	return machineID + nicName
}

func generateRetypeError(metricID string) error {
	return fmt.Errorf("failed to get metric under key %s: %w", metricID, ErrMetricRetypeToGauge)
}
