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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	objectv1alpha1 "github.com/Aryanbhargava18/kube-seaweedfs-provisioner/api/v1alpha1"
)

var _ = Describe("SeaweedFSInstance Controller", func() {

	const (
		InstanceName      = "test-instance"
		InstanceNamespace = "default"
		timeout           = time.Second * 10
		duration          = time.Second * 10
		interval          = time.Millisecond * 250
	)

	Context("When reconciling a resource", func() {
		It("should successfully execute the dynamic lifecycle", func() {
			ctx := context.Background()
			lookupKey := types.NamespacedName{Name: InstanceName, Namespace: InstanceNamespace}

			By("Creating a new SeaweedFSInstance")
			instance := &objectv1alpha1.SeaweedFSInstance{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "storage.aryan.dev/v1alpha1",
					Kind:       "SeaweedFSInstance",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      InstanceName,
					Namespace: InstanceNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, instance)).Should(Succeed())

			By("Checking if the Seaweed CR was created with spec.filer")
			seaweed := &unstructured.Unstructured{}
			seaweed.SetGroupVersionKind(seaweedGVK)

			Eventually(func() error {
				return k8sClient.Get(ctx, lookupKey, seaweed)
			}, timeout, interval).Should(Succeed())

			spec, ok := seaweed.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())
			_, filerExists := spec["filer"]
			Expect(filerExists).To(BeTrue(), "spec.filer should be explicitly injected by the controller")

			// Check OwnerReference
			owners := seaweed.GetOwnerReferences()
			Expect(owners).To(HaveLen(1))
			Expect(owners[0].Kind).To(Equal("SeaweedFSInstance"))

			By("Verifying S3BucketMapping is NOT created before readiness")
			backupStorage := &unstructured.Unstructured{}
			backupStorage.SetGroupVersionKind(backupStorageGVK)
			Consistently(func() bool {
				err := k8sClient.Get(ctx, lookupKey, backupStorage)
				return errors.IsNotFound(err)
			}, time.Second*2, interval).Should(BeTrue())

			By("Simulating Kubernetes status update to mark Seaweed as ready")
			seaweed.Object["status"] = map[string]any{
				"masterStatus": map[string]any{
					"Replicas":      int64(3),
					"ReadyReplicas": int64(3),
				},
			}
			// In envtest, we can just update the status subresource
			Expect(k8sClient.Status().Update(ctx, seaweed)).Should(Succeed())

			By("Verifying S3BucketMapping is created after Seaweed becomes ready")
			Eventually(func() error {
				return k8sClient.Get(ctx, lookupKey, backupStorage)
			}, timeout, interval).Should(Succeed())

			// Verify the endpoint is mapped correctly to port 8333
			bsSpec, ok := backupStorage.Object["spec"].(map[string]any)
			Expect(ok).To(BeTrue())
			s3Spec, ok := bsSpec["s3"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(s3Spec["endpoint"]).To(Equal(fmt.Sprintf("http://%s-filer.%s.svc.cluster.local:8333", InstanceName, InstanceNamespace)))

			By("Verifying idempotency: updating the instance does not recreate children")
			// Trigger a reconciliation by patching the instance
			Eventually(func() error {
				if err := k8sClient.Get(ctx, lookupKey, instance); err != nil {
					return err
				}
				if instance.Annotations == nil {
					instance.Annotations = make(map[string]string)
				}
				instance.Annotations["test"] = "idempotency"
				return k8sClient.Update(ctx, instance)
			}, timeout, interval).Should(Succeed())

			// Wait a bit to let reconciliation happen
			time.Sleep(time.Second * 2)

			// Fetch Seaweed again, resource version should theoretically be stable if we didn't recreate,
			// but envtest/controller-runtime might increment it on patches.
			// The key is that no error occurred and the object still exists.
			seaweedCheck := &unstructured.Unstructured{}
			seaweedCheck.SetGroupVersionKind(seaweedGVK)
			Expect(k8sClient.Get(ctx, lookupKey, seaweedCheck)).Should(Succeed())
			Expect(seaweedCheck.GetUID()).To(Equal(seaweed.GetUID())) // Same exact object
		})
	})
})
