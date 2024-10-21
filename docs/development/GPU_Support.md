# GPU Support in libvirt-provider

The `libvirt-provider` allows for GPU resource management by integrating GPU support into the resource manager. This document outlines the necessary steps to enable and configure GPU support using PCI devices.

## Enabling GPU Support

To enable GPU support in the `libvirt-provider`, you must configure the resource manager to include the pci source. This can be done by adding the `pci` source to the resource manager's sources.

### Command-Line Configuration

You need to start the resource manager with the following command-line options:

```bash
--resource-manager-sources=xx,xx,pci
```

This option specifies that the resource manager will handle PCI resources, including GPUs, along with other specified sources.

Additionally, you must provide a path to a PCI devices file that defines the GPU resources. Use the following command-line option:

```bash
--resource-manager-pci-devices-file=/path/to/pci.yaml
```

### Example PCI Devices File

The PCI devices file is a YAML configuration that specifies the vendors and devices available for GPU allocation. Below is an example of a PCI devices file named `pci.yaml`:

```yaml
vendors:
  - id: "0x10de"  # NVIDIA vendor ID
    name: nvidia
    devices:
      - id: "0x030200"  # Device ID for the NVIDIA GA100 GPU
        name: ga100gl.rev.a1
        type: gpu
```

### Explanation of the PCI Devices File Structure

- **vendors**: This is a list of vendors providing GPU resources. Each vendor must have a unique `id` and a `name`.
    - **id**: The vendor's hexadecimal ID (e.g., "0x10de" for NVIDIA).
    - **name**: The name of the vendor (e.g., "nvidia").
    - **devices**: This is a list of devices associated with the vendor.
        - **id**: The device's hexadecimal ID (e.g., "0x030200" for a VGA-Compatible Controller).
        - **name**: A human-readable name for the device (e.g., "ga100gl.rev.a1").
        - **type**: Specifies the type of the device (e.g., "gpu").

## Conclusion

By following the steps outlined above, you can successfully enable GPU support in the `libvirt-provider`. This integration allows for efficient management and allocation of GPU resources within your environment.
