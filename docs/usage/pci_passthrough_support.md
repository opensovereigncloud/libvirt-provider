# PCI Device Support in `libvirt-provider`

The `libvirt-provider` facilitates PCI device passthrough, enabling resource management for various PCI devices, not limited to GPUs. This document provides a detailed guide on configuring PCI passthrough support, making it adaptable to any PCI device type, such as GPUs, network adapters, storage controllers, etc.

**Status:** Draft / Proof of Concept

**Note:** *The current solution may experience issues upon restart, potentially affecting device availability and passthrough functionality. Testing and further development are in progress to address these limitations.*

## Enabling PCI Passthrough Support

To enable PCI passthrough for any device in the `libvirt-provider`, the resource manager must be configured to handle PCI devices. This is achieved by adding the `pci` source to the resource manager’s configuration.

### Command-Line Configuration

The resource manager needs to be started with specific command-line options to enable PCI resource handling. Below is an example of how to configure the resource manager:

```bash
--resource-manager-sources=xx,xx,pci
```

This configuration adds the `pci` source to the list of resource manager sources, making it capable of managing PCI devices alongside any other specified sources.

Additionally, you must supply a PCI devices configuration file, which describes the available PCI devices, including details like vendor and device IDs. The path to this file is specified as follows:

```bash
--resource-manager-pci-devices-file=/path/to/pci_devices.yaml
```

## PCI Devices File Overview

The PCI devices file is written in YAML format and defines the PCI devices available for passthrough. It includes information such as vendor IDs, device or class IDs, device types, and human-readable names. This file provides flexibility to define multiple PCI devices of different types, making it a general-purpose solution.

### Format Support

We currently support **two formats** for backward compatibility:

- **Old format**: Uses the `id` field to refer to the **class ID** of a device.
- **New format**: Uses the actual **PCI device ID**, along with fields like `subsystemVendor`, `subsystemDevice`, and `revision`.

> ⚠️ **WARNING** ⚠️
>
> Support for the old format is **deprecated** and will be removed in the future once the new format is fully adopted by Gardener Linux.

### Example: Mixed Format YAML

```yaml
vendors:
  # Old format using class ID (deprecated or backward-compatible format)
  - id: "0x10de"  # Vendor ID for NVIDIA
    name: nvidia  # Human-readable name for the vendor
    devices:
      - id: "0x030200"        # Class ID for the NVIDIA GA100 GPU (Display Controller - 3D)
        name: ga100gl.rev.a1  # Human-readable name for the device
        type: gpu             # Device type (e.g., gpu, network, storage)

  # New format using actual PCI device ID (recommended format)
  - id: "0x15b3"    # Vendor ID for Mellanox (NVIDIA Networking)
    name: mellanox  # Human-readable name for the vendor
    devices:
      - id: "0x1017"               # Device ID for a Mellanox network adapter (e.g., ConnectX-5)
        subsystemVendor: "0x15b3"  # Subsystem vendor ID
        subsystemDevice: "0x0097"  # Subsystem device ID
        revision: "0x00"           # Revision ID for further matching specificity
        name: cx5-rev00            # Human-readable name for the device
        type: network              # Device type (e.g., gpu, network, storage)
```

### Example: Handling Multiple Device Types

The structure allows the resource manager to handle various device types, not just GPUs. For instance:

- GPUs (e.g., NVIDIA GA100)

- Network adapters (e.g., Intel X520)

- Storage controllers (e.g., Intel ICH9 SATA)

Each device is referenced using its vendor and device IDs, which are crucial for the passthrough mechanism. The `type` field is particularly important for informing the resource manager about the device's function, allowing it to appropriately manage the resources based on their specific characteristics.

## Device Grouping Behavior

When the resource manager scans PCI devices on the host and loads the configuration from `pci_devices.yaml`, it generates a **resource name** for each device using the format `<device_type>.<vendor>/<device_name>`

If **multiple PCI devices** (with different PCI IDs or paths) map to the **same resource name**, they will be **grouped** together and represented as a single available resource with a count.

### Example: Grouped Devices

If you have a config like:

```yaml
- id: "0x15b3"
  name: mellanox
  devices:
  - id: "0x1017"
    subsystemVendor: "0x15b3"
    subsystemDevice: "0x0097"
    revision: "0x00"
    name: cx5
    type: network
  - id: "0x1017"
    subsystemVendor: "0x15b3"
    subsystemDevice: "0x0097"
    revision: "0x01"
    name: cx5
    type: network
```

And both devices are present on the host, they will be grouped under `network.mellanox/cx5: 2`
This allows grouping different devices under the same resource name, as long as they share:

- the same type
- the same vendor name
- the same device name

If any of the above values of the devices varies, they will be exposed as separate resources.

> ⚠️ **Grouping Caution**
>
> This feature allows you to group hardware variants (e.g., different revisions) into a single logical resource,
> which is useful when treating compatible devices as interchangeable.
>
> However, **be cautious** — unintentional grouping due to duplicate names can lead to unexpected behavior.

## Collecting PCI Device Information for the YAML File

As described above, the `pci_devices.yaml` requires several identifiers for each PCI device. These values can be obtained from the Linux system where the device is installed, and optionally verified through online repositories.

### From the Linux system

Each PCI device has a directory under `/sys/bus/pci/devices/`. For example, for `0000:af:00.0`:

```bash
cd /sys/bus/pci/devices/0000:af:00.0
cat vendor            # e.g., 0x15b3
cat device            # e.g., 0x1017
cat subsystem_vendor  # e.g., 0x15b3
cat subsystem_device  # e.g., 0x0097
cat revision          # e.g., 0x00
```

### Cross-referencing

[pci-ids.ucw.cz](https://pci-ids.ucw.cz/) offers a comprehensive list of vendor and device IDs, which can be cross-referenced to ensure the correct information.

> *Note:* The `type` field in `pci_devices.yaml` is not part of PCI specifications — choose a value (`gpu`, `network`, `storage`, etc.) that matches how you want the device to be grouped and allocated.

## MachineClass Configuration for PCI Devices

In order to match workloads with hosts capable of providing PCI passthrough resources, the relevant PCI devices must also be declared as capabilities within the MachineClass definitions. This ensures the resource manager can consider the available hardware when allocating resources.

For every PCI device, there must be a MachineClass that explicitly references the device using the format: `<type>.<vendor>/<device_name>`. For example, for the NVIDIA device in the `pci_devices.yaml` defined above, the corresponding MachineClass will look like:

```json
[
  {
    "name": "t3-small-gpu",
    "capabilities": {
      "cpu": 2,
      "memory": 2147483648,
      "gpu.nvidia/ga100gl.rev.a1": 1  // Entry for the NVIDIA device mentioned above
    }
  }
]
```

**Note:** *It is critical that the capability key in the MachineClass exactly matches the format `type.vendor/device_name` as defined in the pci_devices.yaml. Any mismatch will result in the device not being allocated correctly.*

## Integration with `libvirt-provider`

When PCI passthrough is enabled, `libvirt-provider` ensures that the specified PCI devices are allocated to the appropriate virtual machines. The information provided in the `pci_devices.yaml` file is passed through the `libvirt-provider`, allowing seamless passthrough of resources to the VMs.

## Best Practices for PCI Device Management

- **Vendor ID and Device ID Accuracy**: Ensure the vendor and device IDs are accurate, as these hexadecimal identifiers are used by the system to identify the correct PCI device for passthrough.
- **Device Grouping**: Devices defined in `pci_devices.yaml` that share the same `device type`, `vendor name`, and `device name` will be grouped under a single resource name. This allows you to combine different hardware variants into a single logical capability .

## Conclusion

The `libvirt-provider` offers a robust mechanism for handling PCI device passthrough. By configuring the resource manager to handle PCI sources and providing detailed PCI device information through a YAML file, you can seamlessly manage a variety of PCI devices, including GPUs, network adapters, and storage controllers. This general approach ensures flexibility in handling any PCI device type, making it suitable for a wide range of use cases.
