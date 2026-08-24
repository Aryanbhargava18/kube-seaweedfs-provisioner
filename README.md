# kube-seaweedfs-provisioner

A Kubernetes controller built using `controller-runtime` demonstrating dynamic provisioning, status observation, and conditional dependency mapping for SeaweedFS S3 object storage topologies.

```text
       +---------------------------------------------+
       |         SeaweedFSInstance Resource          |
       |  (api: storage.aryan.dev/v1alpha1)          |
       +----------------------+----------------------+
                              |
                     [ Reconcile Loop ]
                              |
       1. Inject spec.filer & spec.s3
                              v
       +---------------------------------------------+
       |         Seaweed CR (Unstructured)           |
       |  (seaweedfs.com/v1 - Managed by Operator)   |
       +----------------------+----------------------+
                              |
       2. Observe status.s3.readyReplicas == replicas
                              v
             +----------------+----------------+
             |                                 |
             | S3 Not Ready                    | S3 Ready (Phase: Ready)
             v                                 v
   [ Phase: WaitingForClusterReady ]  [ 3. Apply S3BucketMapping CR ]
   (Yield & Watch Owned CR)           (Endpoint: http://<name>-s3:8333)
```

---

## The Problem: S3 Compatibility Readiness

When integrating SeaweedFS via the official `seaweedfs.com/v1` operator into automated database-as-a-service platforms (such as OpenEverest backup storage engines), a subtle lifecycle gap exists:

* **Conditional Operator Logic:** In `seaweed_controller.go`, the upstream operator only evaluates S3 readiness conditions if `Spec.S3 != nil`. A Seaweed cluster without explicit S3 configuration can report generic cluster readiness (`isReady=true`) while silently rejecting S3 backup traffic.
* **Pre-Readiness Gating:** Creating dependent backup resources (`BackupStorage` / bucket bindings) before the standalone S3 gateway pods are in `Ready` state causes backup tasks to fail with immediate connection timeouts.

`kube-seaweedfs-provisioner` implements an automated, idempotent controller pattern to resolve this gap.

---

## Controller Reconciler State Machine

The reconciler executes a deterministic multi-stage lifecycle:

| Phase | Reconciler Action | Transition Trigger |
|---|---|---|
| `ProvisioningCluster` | Renders unstructured `Seaweed` CR with injected `spec.filer` and `spec.s3`, sets `ControllerReference`. | CR successfully applied to API server. |
| `WaitingForMasterStatus` | Watches child `Seaweed` CR status subresource. | Operator reports master/filer topology. |
| `WaitingForClusterReady` | Evaluates `status.s3.readyReplicas == status.s3.replicas`. Requeues if S3 gateway is pending. | All S3 gateway replica pods reach `Ready`. |
| `Ready` | Renders dependent `S3BucketMapping` CR pointing to port `8333` S3 gateway service (`http://<name>-s3.<ns>.svc:8333`). | Endpoint verified; controller enters steady state. |

---

## Architectural Boundaries

### What this repository validates:
* **Dynamic CR Rendering:** Reconciler injects explicit `spec.filer` and `spec.s3` constraints into child unstructured objects without tight compile-time Go package coupling on upstream operator internal types.
* **Status-Gated Dependency Mapping:** Dependent bucket resources are withheld until exact S3 gateway pod readiness is achieved.
* **Idempotency & Lifecycle Safety:** Repeated reconciliation runs produce zero mutation churn and retain `OwnerReference` trees for garbage collection.

### Explicit Scope Clarification:
* `S3BucketMapping` is a test double representing OpenEverest's `BackupStorage` dependency. The prototype validates controller reconciliation behavior and readiness gating; it does not claim to validate the internal OpenEverest `provider-runtime` multi-cluster interface.

---

## Local Development & Testing

### Test Suite (`envtest`)

The test suite uses `sigs.k8s.io/controller-runtime/pkg/envtest` (a local control plane with `etcd` and `kube-apiserver`) to simulate operator status changes dynamically during reconciliation:

```bash
# Run Ginkgo / Gomega controller integration suite
make test
```

### Running Locally on a Cluster

```bash
# 1. Install Custom Resource Definitions
make install

# 2. Run controller against local kubeconfig
make run

# 3. Apply sample instance
kubectl apply -f config/samples/object_v1alpha1_seaweedfsinstance.yaml
```

---

## Project Structure

```text
├── api/v1alpha1/                  # CRD Schema definitions for SeaweedFSInstance
├── internal/controller/           # Core Reconciler & status observation logic
├── config/
│   ├── crd/                       # Generated Kubebuilder CRDs
│   ├── rbac/                      # RBAC role bindings for Seaweed & Bucket CRs
│   └── test-crds/                 # Mock CRD schemas for envtest lifecycle simulation
└── Makefile                       # Build, test, and code generation targets
```

---

## License

Apache 2.0
