# PCI Device Support in `libvirt-provider`

The `libvirt-provider` facilitates PCI device passthrough, enabling resource management for various PCI devices, not limited to GPUs. This document provides a detailed guide on configuring PCI passthrough support, making it adaptable to any PCI device type, such as GPUs, network adapters, storage controllers, etc.

**Status**: Draft / Proof of Concept
*Note: The current solution may experience issues upon restart, potentially affecting device availability and passthrough functionality. Testing and further development are in progress to address these limitations.*

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

## Finding PCI Vendor and Device IDs

For the PCI passthrough to function correctly, the IDs of each device must be provided. Vendor and Device IDs can be found using system files and online repositories. Here are some options:

- **Linux system files**: You can find PCI IDs in `/sys/bus/pci/devices/`. Each subdirectory represents a PCI device, where files like `vendor` and `device` provide the necessary identifiers.
- **Online PCI ID repository**: [pci-ids.ucw.cz](https://pci-ids.ucw.cz/) offers a comprehensive list of vendor and device IDs, which can be cross-referenced to ensure the correct information.

## PCI Devices File Overview

The PCI devices file is written in YAML format and defines the PCI devices available for passthrough. It includes information such as vendor IDs, device IDs, device types, and human-readable names. This file provides flexibility to define multiple PCI devices of different types, making it a general-purpose solution.

Below is an example of a `pci_devices.yaml` file:

```yaml
vendors:
  - id: "0x10de"  # NVIDIA vendor ID
    name: nvidia
    devices:
      - id: "0x030200"  # Device ID for the NVIDIA GA100 GPU
        name: ga100gl.rev.a1
        type: gpu
  - id: "0x8086"  # Intel vendor ID
    name: intel
    devices:
      - id: "0x100f"  # Device ID for an Intel network adapter
        name: x520-da2
        type: network
```

### PCI Devices File Structure

The PCI devices file contains a hierarchical structure to organize vendors and their associated devices:

- **vendors**: A list of vendors providing PCI devices. Each vendor entry consists of:
    - **id**: A unique hexadecimal identifier for the vendor (e.g., "0x10de" for NVIDIA, "0x8086" for Intel).
    - **name**: A human-readable name for the vendor (e.g., "nvidia", "intel").
    - **devices**: A list of devices provided by the vendor. Each device entry consists of:
        - **id**: A unique hexadecimal identifier for the device (e.g., "0x030200" for a GPU, "0x100f" for a network adapter).
        - **name**: A human-readable name for the device (e.g., "ga100gl.rev.a1", "x520-da2").
        - **type**: The type of the PCI device, which helps categorize it (e.g., "gpu", "network", "storage").

#### Example: Handling Multiple Device Types

The structure allows the resource manager to handle various device types, not just GPUs. For instance:

- GPUs (e.g., NVIDIA GA100)

- Network adapters (e.g., Intel X520)

- Storage controllers (e.g., Intel ICH9 SATA)

Each device is referenced using its vendor and device IDs, which are crucial for the passthrough mechanism. The `type` field is particularly important for informing the resource manager about the device's function, allowing it to appropriately manage the resources based on their specific characteristics.

## Integration with `libvirt-provider`

When PCI passthrough is enabled, `libvirt-provider` ensures that the specified PCI devices are allocated to the appropriate virtual machines. The information provided in the `pci_devices.yaml` file is passed through the `libvirt-provider`, allowing seamless passthrough of resources to the VMs.

## Best Practices for PCI Device Management

- **Vendor ID and Device ID Accuracy**: Ensure the vendor and device IDs are accurate, as these hexadecimal identifiers are used by the system to identify the correct PCI device for passthrough.
- **Device Grouping**: Group devices based on their type (e.g., GPU, network, storage) for better organization and maintainability of the `pci_devices.yaml` file.

## Conclusion

The `libvirt-provider` offers a robust mechanism for handling PCI device passthrough. By configuring the resource manager to handle PCI sources and providing detailed PCI device information through a YAML file, you can seamlessly manage a variety of PCI devices, including GPUs, network adapters, and storage controllers. This general approach ensures flexibility in handling any PCI device type, making it suitable for a wide range of use cases.
