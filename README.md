# kube-seaweedfs-provisioner

![Go Version](https://img.shields.io/badge/go-1.21-blue.svg)
![Kubernetes Compatibility](https://img.shields.io/badge/kubernetes-1.28+-blue.svg)
![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)

A Kubernetes operator built using `controller-runtime` to explore dynamic provisioning of SeaweedFS topologies and conditional dependency mapping.

## Motivation & Problem Statement

SeaweedFS is a highly scalable distributed file system. When deploying it on Kubernetes (via the official SeaweedFS operator), you interact with the `seaweeds.seaweedfs.com/v1` Custom Resource.

However, a critical operational gap exists when integrating SeaweedFS into broader infrastructure platforms (like automated database-as-a-service or automated backup platforms): **S3 Compatibility Readiness**.

A generic SeaweedFS Ready condition is not sufficient for an S3 provider because the operator only evaluates the S3 component when `spec.s3` is configured. The prototype therefore makes both the Filer backend and standalone S3 gateway explicit and waits for the S3 component's readiness before creating the dependent resource.

This project (`kube-seaweedfs-provisioner`) was built to demonstrate a strict, automated mitigation to this problem.

## Architecture

This controller manages a higher-level CR (`SeaweedFSInstance`) which orchestrates the deployment.

### 1. Desired State Enforcement
When a `SeaweedFSInstance` is created, the reconciler generates an unstructured `Seaweed` CR. It explicitly injects the mandatory `spec.filer` and `spec.s3` constraints to guarantee that the standalone S3 gateway will be scheduled:

```yaml
spec:
  filer: {} # Injected by the provisioner
  s3: {}    # Injected by the provisioner
```

### 2. External Status Observation
Instead of blindly waiting for a generic cluster ready state, the provisioner observes the upstream `Seaweed` CR's detailed S3 status. It watches for:
`status.s3.readyReplicas == status.s3.replicas` 

### 3. Conditional Dependency Mapping
Only after the strict S3 readiness conditions are met will the provisioner apply the dependent `S3BucketMapping` CR. 

`S3BucketMapping` is only a test double for the dependency represented by OpenEverest's `BackupStorage`. The prototype validates controller behavior; it does not claim to validate the real OpenEverest `provider-runtime` integration.

```text
SeaweedFSInstance
        |
        v
controller-runtime
        |
        v
Seaweed CR
  ├── spec.filer
  └── spec.s3
        |
        v
SeaweedFS operator
        |
        +── Filer
        |
        +── S3 Gateway
              |
              v
        <cluster>-s3:8333
              |
              v
       S3BucketMapping
```

## Verification Boundary

**What this repository proves:**
- `spec.filer` + `spec.s3` are rendered by the controller.
- dependent resource is withheld before required readiness.
- S3 readiness causes dependent-resource creation.
- repeated reconciliation is idempotent.
- owner references/watch behavior works as implemented.

**What it does not prove:**
- the real SeaweedFS operator produces the expected status under every deployment condition.
- real S3 API compatibility.
- OpenEverest `provider-runtime` integration.
- real `BackupStorage` behavior.

## Getting Started

### Prerequisites
- `go` version v1.21.0+
- `docker` version 17.03+.
- `kubectl` version v1.11.3+.
- A local Kubernetes cluster (e.g., `kind` or `minikube`).

### Local Development

1. **Install CRDs** into your cluster:
   ```sh
   make install
   ```

2. **Run the operator** outside the cluster (for debugging/development):
   ```sh
   make run
   ```

3. **Deploy a test instance**:
   ```sh
   kubectl apply -f config/samples/object_v1alpha1_seaweedfsinstance.yaml
   ```

## Testing Strategy

This repository employs `envtest` to validate the complex status-observation loop. Because the upstream SeaweedFS operator is not running during unit tests, the test suite simulates the upstream operator by dynamically patching the `.status.s3` of the unstructured `Seaweed` CR during the reconciliation loop.

Run the test suite:
```sh
make test
```

## Project Structure

- `api/v1alpha1/`: Contains the schema definitions for `SeaweedFSInstance`.
- `internal/controller/`: The core reconciliation logic. Unstructured clients are used here to avoid hard Go dependencies on the upstream operator.
- `config/`: Kubebuilder YAML definitions for CRDs, RBAC, and Webhooks.
- `config/test-crds/`: Minimal mock CRDs for `Seaweed` and `S3BucketMapping` required by `envtest`.

## License

Copyright 2026 Aryan Bhargava.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
