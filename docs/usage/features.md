# Features list

The list describes current state of features/logic in osc/main.
Some features can be merged into osc/main but they aren't production ready.

## Legend

States:

The state represents current usability of a feature.

|  Name   | Description                                                                                           |
| :-----: | ----------------------------------------------------------------------------------------------------- |
| 🟢stable | a feature is production ready, though a bugs can still appear                                         |
|  🟡beta  | a feature or part of feature wasn't tested (e.g., due to HW limitations or other reasons)             |
|  🔴wip   | a feature is still under development (work in progress) and we don't recommend using it in production |

## Features

Last update: 2024-12-11T13:02:40+00:00

Please order features alphabetically A→Z in tables.

### Libvirt-provider related

| Name         | Description                                     | Commit  |  State  | Additional information                                  |
| ------------ | ----------------------------------------------- | ------- | :-----: | ------------------------------------------------------- |
| Healthcheck  | Healtcheck and probes endpoints                 | f8ef1e6 |  🔴wip   | Healtcheck only verifies connection to libvirt-provider |
| IRI events   | Support of events for VM machine reconciliation | f8ef1e6 | 🟢stable |                                                         |
| Metrics      | Exposing internal libvirt-provider metrics      | f8ef1e6 |  🔴wip   | Some important metrics aren't implemented yet           |
| PPROF server | Golang profiling server                         | f8ef1e6 | 🟢stable |                                                         |

### Resource-manager related

| Name             | Description                                                                        | Commit  |  State  | Additional information                                                                    |
| ---------------- | ---------------------------------------------------------------------------------- | ------- | :-----: | ----------------------------------------------------------------------------------------- |
| Overcommit VCPU  | Source CSU can report calculate with more vCPU as physical cpu cores are available | f8ef1e6 | 🟢stable |                                                                                           |
| Resource-manager | Central management of resources and limitation of vm                               | f8ef1e6 | 🟢stable |                                                                                           |
| Source CPU       | Management of cpu resource                                                         | f8ef1e6 | 🟢stable |                                                                                           |
| Source hugepages | Management of hugepages resource                                                   | f8ef1e6 | 🟢stable |                                                                                           |
| Source memory    | Management of memory resource                                                      | f8ef1e6 | 🟢stable |                                                                                           |
| Source PCI       | Management of pci devices                                                          | f8ef1e6 |  🟡beta  | We never tested add multiple pci devices into one vm (our HW doesn't have enough devices) |
| Source SGX       | Management of sgx resource                                                         | f8ef1e6 | 🟢stable |                                                                                           |

### Network related

| Name        | Description                                   | Commit  |  State  | Additional information         |
| ----------- | --------------------------------------------- | ------- | :-----: | ------------------------------ |
| Isolated    | Disables network                              | f8ef1e6 | 🟢stable | Primarily used for development |
| Providernet | Managing network over libvirt daemon networks | f8ef1e6 | 🟢stable |                                |
| APINet      | Managing network over APINet                  | f8ef1e6 | 🟢stable |                                |

### Volume related

| Name          | Description                  | Commit  |  State  | Additional information          |
| ------------- | ---------------------------- | ------- | :-----: | ------------------------------- |
| Volume resize | Automatic resizing of volume | f8ef1e6 | 🟢stable | Only supported for ceph volumes |

### VM related

| Name                   | Description                                        | Commit  |  State  | Additional information                                                  |
| ---------------------- | -------------------------------------------------- | ------- | :-----: | ----------------------------------------------------------------------- |
| Libvirt events         | Support of trigger reconcile loop by libvirt event | f8ef1e6 | 🟢stable | just for machine state events, not all libvirt events are supported yet |
| Preferred domain types | Enabling using of different type of hypervisor     | f8ef1e6 |  🟡beta  | we tested qemu hypervisor only                                          |
| Qemu guest agent       | Enabling communication with qemu guest agent in vm | f8ef1e6 | 🟢stable |                                                                         |
| SGX                    | Support of SGX in VM                               | f8ef1e6 | 🟢stable |                                                                         |
| VM Console             | Support for exposing VM console over websocket     | f8ef1e6 | 🟢stable |                                                                         |
| VM Graceful shutdown   | Support of graceful shutdown of VM                 | f8ef1e6 | 🟢stable |                                                                         |
