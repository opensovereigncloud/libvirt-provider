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

Additionally, you must supply a PCI devices configuration file, which describes the available PCI devices, including details like vendor and Device IDs. The path to this file is specified as follows:

```bash
--resource-manager-pci-devices-file=/path/to/pci_devices.yaml
```

## PCI Devices File Overview

The PCI devices file is written in YAML format and defines the PCI devices available for passthrough. It includes information such as Vendor IDs, Device or Class IDs, Device types, and human-readable names. This file provides flexibility to define multiple PCI devices of different types, making it a general-purpose solution.

Detection of PCI devices is based on [sysfs](https://docs.kernel.org/filesystems/sysfs.html) and we currently support detection in **two formats** for backward compatibility. These formats use different attributes for device detection.

**Field-to-Source Mapping:**

> **Note:** `<DBDF>` = Domain:Bus:Device.Function address of the PCI device (e.g., `0000:ca:00.0`).

| Device Attribute  | Source Path on Host                            | Description                                                 | Class ID Format | Device ID Format |
| ----------------- | ---------------------------------------------- | ----------------------------------------------------------- | :-------------: | :--------------: |
| `class`           | `/sys/bus/pci/devices/<DBDF>/class`            | Broad PCI class (e.g., `0x030200` = 3D Display Controller). |        ✅        |                  |
| `vendor`          | `/sys/bus/pci/devices/<DBDF>/vendor`           | Manufacturer of the chipset (e.g., NVIDIA, Intel).          |        ✅        |        ✅         |
| `device`          | `/sys/bus/pci/devices/<DBDF>/device`           | Specific chipset model identifier.                          |                 |        ✅         |
| `subsystemVendor` | `/sys/bus/pci/devices/<DBDF>/subsystem_vendor` | Manufacturer of the complete card/device (e.g., HP, Dell).  |                 |        ✅         |
| `subsystemDevice` | `/sys/bus/pci/devices/<DBDF>/subsystem_device` | OEM-specific variant/model of the device.                   |                 |        ✅         |
| `revision`        | `/sys/bus/pci/devices/<DBDF>/revision`         | Hardware revision identifier (e.g., `0xa1`, `0xa2`).        |                 |        ✅         |

### Class ID based format (Deprecated)

> ⚠️ **WARNING** ⚠️
>
> Support for this format is **deprecated** and will be removed in the future once the Device ID based format is fully adopted by [Garden Linux](https://github.com/gardenlinux/gardenlinux).

This format uses the `id` field under `devices` to refer to the **Class ID** of a device.

This relies solely on **PCI Class IDs**. While this was sufficient for basic GPU passthrough, it came with **major drawbacks**:

- **Over-broad matching:** Devices with the same class but different chipsets were indistinguishable.
- **No revision awareness:** Could not differentiate hardware revisions of the same chipset.
- **No subsystem differentiation:** Failed to distinguish vendor-specific variants (e.g., Intel chip rebranded by HP or Dell).
- **Risk of driver mismatches:** A device could be matched to an incompatible driver because only the Class ID matched.

**Example:**

```yaml
vendors:
    # Source: /sys/bus/pci/<DBDF>/vendor
  - id: "0x10de"  # Hexadecimal Vendor ID for NVIDIA
    name: nvidia  # Human-readable name for the vendor
    devices:
        # Source: /sys/bus/pci/<DBDF>/class
      - id: "0x030200"        # Hexadecimal Class ID for the NVIDIA GA100 GPU Display Controller - 3D
        name: ga100gl.rev.a1  # Human-readable name for the device
        type: gpu             # Device type (e.g., gpu, network, storage)
```

### Device ID based format (Recommended)

This format uses **exact PCI identifiers** instead of broad class codes, enabling **pin-point hardware matching**:

- **Device-level identification:** Uses the actual PCI Device ID instead of the generic Class ID.
- **Subsystem-level precision:** Adds `subsystemVendor` and `subsystemDevice` to differentiate OEM variants.
- **Revision awareness:** Tracks the `revision` field to avoid subtle hardware mismatches.
- **Better driver matching:** Prevents loading an incorrect driver for partially matching hardware.

**Example:**

```yaml
vendors:
    # Source: /sys/bus/pci/<DBDF>/vendor
  - id: "0x10de"  # Hexadecimal Vendor ID for NVIDIA
    name: nvidia  # Human-readable name for the vendor
    devices:
        # Source: /sys/bus/pci/<DBDF>/device
      - id: "0x20b7"               # Hexadecimal Device ID for specific GPU device model
        # Source: /sys/bus/pci/<DBDF>/subsystemVendor
        subsystemVendor: "0x10de"  # Hexadecimal subsystem Vendor ID
        # Source: /sys/bus/pci/<DBDF>/systemDevice
        subsystemDevice: "0x1532"  # Hexadecimal subsystem Device ID
        # Source: /sys/bus/pci/<DBDF>/revision
        revision: "0xa1"           # Hexadecimal revision ID for further matching specificity
        name: ga100gl.rev.a1       # Human-readable name for the device
        type: gpu                  # Device type (e.g., gpu, network, storage)
```

### Migration: Class ID based format to Device ID based format

- The system still supports Class ID based format entries for backward compatibility.
- Migration mainly means the below changes to all the devices:
    - change the value of `id` under devices from Class ID to Device ID
    - add `subsystemVendor`
    - add `subsystemDevice`
    - add `revision`

**Example:**

*Nvidia card attributes in sysfs:*

- vendor: 0x10de
- class: 0x030200
- device: 0x20b7
- subsystem_vendor: 0x10de
- subsystem_device: 0x1532
- revision: 0xa1

*Class ID based format:*

```yaml
vendors:
  - id: "0x10de"
    name: nvidia
    devices:
      - id: "0x030200"
        name: ga100gl.rev.a1
        type: gpu
```

*Device ID based format:*

```yaml
vendors:
  - id: "0x10de"
    name: nvidia
    devices:
      - id: "0x20b7"
        subsystemVendor: "0x10de"
        subsystemDevice: "0x1532"
        revision: "0xa1"
        name: ga100gl.rev.a1
        type: gpu
```

### Example: Handling Multiple Device Types

The structure allows the resource manager to handle various device types, not just GPUs. For instance:

- GPUs (e.g., NVIDIA GA100)

- Network adapters (e.g., Intel X520)

- Storage controllers (e.g., Intel ICH9 SATA)

Each device is referenced using its vendor and Device IDs, which are crucial for the passthrough mechanism. The `type` field is particularly important for informing the resource manager about the device's function, allowing it to appropriately manage the resources based on their specific characteristics.

### Cross-referencing PCI device information

[pci-ids.ucw.cz](https://pci-ids.ucw.cz/) offers a comprehensive list of vendor and Device IDs, which can be cross-referenced to ensure the correct information.

> *Note:* The `type` field in `pci_devices.yaml` is not part of PCI specifications — choose a value (`gpu`, `network`, `storage`, etc.) that matches how you want the device to be grouped and allocated.

## Device Grouping Behavior

When the resource manager scans PCI devices on the host and loads the configuration from `pci_devices.yaml`, it generates a **resource name** for each device using the format `<device_type>.<vendor_name>/<device_name>`

If **multiple PCI devices** (with different PCI IDs or paths) map to the **same resource name**, they will be **grouped** together and represented as a single available resource with a count.

### Example: Grouped Devices

If you have a config like:

```yaml
vendors:
  - id: "0x10de"
    name: nvidia
    devices:
      - id: "0x20b7"
        subsystemVendor: "0x10de"
        subsystemDevice: "0x1532"
        revision: "0xa1"
        name: ga100gl.rev.a1
        type: gpu
      - id: "0x20b7"
        subsystemVendor: "0x10de"
        subsystemDevice: "0x1532"
        revision: "0xa2" # Different revision ID from above
        name: ga100gl.rev.a1
        type: gpu
```

And both devices are present on the host, they will be grouped under `gpu.nvidia/ga100gl.rev.a1: 2` even if they have different revisions.
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

- **Vendor ID and Device ID Accuracy**: Ensure the vendor and Device IDs are accurate, as these hexadecimal identifiers are used by the system to identify the correct PCI device for passthrough.
- **Device Grouping**: Devices defined in `pci_devices.yaml` that share the same `device type`, `vendor name`, and `device name` will be grouped under a single resource name. This allows you to combine different hardware variants into a single logical capability .

## Conclusion

The `libvirt-provider` offers a robust mechanism for handling PCI device passthrough. By configuring the resource manager to handle PCI sources and providing detailed PCI device information through a YAML file, you can seamlessly manage a variety of PCI devices, including GPUs, network adapters, and storage controllers. This general approach ensures flexibility in handling any PCI device type, making it suitable for a wide range of use cases.
