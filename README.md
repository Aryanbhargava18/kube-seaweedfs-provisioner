# kube-seaweedfs-provisioner

An experimental Kubernetes controller demonstrating dynamic reconciliation and dependency mapping for SeaweedFS.

## Overview

This project is a standalone `controller-runtime` operator I built to experiment with observing and mapping Kubernetes status across loosely-coupled Custom Resources (CRs). 

Specifically, it demonstrates how to conditionally map an S3 bucket configuration (`S3BucketMapping`) to a SeaweedFS cluster only *after* observing that the cluster's gateway (`Filer`) is fully ready.

## Features

- **Desired state enforcement**: Dynamically renders the unstructured `Seaweed` CR and explicitly injects the mandatory `spec.filer` constraint.
- **External status observation**: Watches the Kubernetes API for the `Seaweed` operator to update `.status.masterStatus`.
- **Conditional state transitions**: Waits for `ReadyReplicas == Replicas` before provisioning the dependent `S3BucketMapping` CR.
- **Idempotency**: Ensures subsequent reconciliations do not duplicate resources or panic on existing ones.
- **Owner references**: Ties the lifecycles of the dependent objects to the provisioner instance.

## Testing

The controller logic is validated via `envtest`, which simulates the Kubernetes API server and etcd to verify the reconciliation loop handles status transitions and race conditions flawlessly.

To run the suite locally:

```bash
make test
```
