/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	objectv1alpha1 "github.com/Aryanbhargava18/kube-seaweedfs-provisioner/api/v1alpha1"
)

// SeaweedFSInstanceReconciler reconciles a SeaweedFSInstance object
type SeaweedFSInstanceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var (
	seaweedGVK       = schema.GroupVersionKind{Group: "seaweedfs.com", Version: "v1", Kind: "Seaweed"}
	backupStorageGVK = schema.GroupVersionKind{Group: "storage.aryan.dev", Version: "v1alpha1", Kind: "S3BucketMapping"}
)

// +kubebuilder:rbac:groups=storage.aryan.dev,resources=seaweedfsinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.aryan.dev,resources=seaweedfsinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=storage.aryan.dev,resources=seaweedfsinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=seaweedfs.com,resources=seaweeds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=storage.aryan.dev,resources=s3bucketmappings,verbs=get;list;watch;create;update;patch;delete

func (r *SeaweedFSInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var instance objectv1alpha1.SeaweedFSInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 1. Render and apply Seaweed CR
	seaweed := &unstructured.Unstructured{}
	seaweed.SetGroupVersionKind(seaweedGVK)
	seaweed.SetName(instance.Name)
	seaweed.SetNamespace(instance.Namespace)

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, seaweed, func() error {
		if err := controllerutil.SetControllerReference(&instance, seaweed, r.Scheme); err != nil {
			return err
		}
		// Enforce the Filer readiness constraint identified in Issue #2255 research.
		// A Seaweed CR without spec.filer can report isReady=true while dropping S3 traffic.
		spec, ok := seaweed.Object["spec"].(map[string]any)
		if !ok {
			spec = make(map[string]any)
		}
		// Explicitly ensure spec.filer is present to guarantee S3 gateway deployment
		if _, exists := spec["filer"]; !exists {
			spec["filer"] = make(map[string]any)
		}
		seaweed.Object["spec"] = spec
		return nil
	})
	if err != nil {
		log.Error(err, "unable to reconcile Seaweed CR")
		return ctrl.Result{}, err
	}
	if op != controllerutil.OperationResultNone {
		log.Info("reconciled Seaweed CR", "operation", op)
	}

	// 2. Observe Seaweed CR readiness from Kubernetes state
	status, ok := seaweed.Object["status"].(map[string]interface{})
	if !ok {
		// Status not yet populated by the SeaweedFS operator (or test simulation)
		return r.updatePhase(ctx, &instance, "ProvisioningCluster")
	}

	masterStatus, ok := status["masterStatus"].(map[string]interface{})
	if !ok {
		return r.updatePhase(ctx, &instance, "WaitingForMasterStatus")
	}

	replicas := parseInt64(masterStatus["Replicas"])
	readyReplicas := parseInt64(masterStatus["ReadyReplicas"])

	if replicas == 0 || readyReplicas < replicas {
		// Cluster is not yet ready according to Kubernetes state
		return r.updatePhase(ctx, &instance, "WaitingForClusterReady")
	}

	// 3. Render and apply S3BucketMapping once ready
	backupStorage := &unstructured.Unstructured{}
	backupStorage.SetGroupVersionKind(backupStorageGVK)
	backupStorage.SetName(instance.Name)
	backupStorage.SetNamespace(instance.Namespace)

	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, backupStorage, func() error {
		if err := controllerutil.SetControllerReference(&instance, backupStorage, r.Scheme); err != nil {
			return err
		}

		spec, ok := backupStorage.Object["spec"].(map[string]interface{})
		if !ok {
			spec = make(map[string]interface{})
		}

		// Map the endpoint to the constant FilerS3Port (8333) as identified in research
		s3Spec := map[string]interface{}{
			"endpoint": fmt.Sprintf("http://%s-filer.%s.svc.cluster.local:8333", instance.Name, instance.Namespace),
			// In a real implementation, credentialsSecretRef would be populated here
		}
		spec["s3"] = s3Spec
		backupStorage.Object["spec"] = spec
		return nil
	})
	if err != nil {
		log.Error(err, "unable to reconcile S3BucketMapping CR")
		return ctrl.Result{}, err
	}
	if op != controllerutil.OperationResultNone {
		log.Info("reconciled S3BucketMapping CR", "operation", op)
	}

	return r.updatePhase(ctx, &instance, "Ready")
}

func (r *SeaweedFSInstanceReconciler) updatePhase(ctx context.Context, instance *objectv1alpha1.SeaweedFSInstance, phase string) (ctrl.Result, error) {
	if instance.Status.Phase == phase {
		return ctrl.Result{}, nil
	}
	instance.Status.Phase = phase
	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func parseInt64(val interface{}) int64 {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case int32:
		return int64(v)
	case int:
		return int64(v)
	case float64:
		return int64(v)
	}
	return 0
}

// SetupWithManager sets up the controller with the Manager.
func (r *SeaweedFSInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	seaweed := &unstructured.Unstructured{}
	seaweed.SetGroupVersionKind(seaweedGVK)

	backupStorage := &unstructured.Unstructured{}
	backupStorage.SetGroupVersionKind(backupStorageGVK)

	return ctrl.NewControllerManagedBy(mgr).
		For(&objectv1alpha1.SeaweedFSInstance{}).
		Owns(seaweed).
		Owns(backupStorage).
		Named("seaweedfsinstance").
		Complete(r)
}
