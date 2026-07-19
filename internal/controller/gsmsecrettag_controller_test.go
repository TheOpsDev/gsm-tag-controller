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
	"maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gsmtagcontrollerv1alpha1 "github.com/theopsdev/gsm-tag-controller/api/v1alpha1"
)

// fakeTagBinder records calls and reports the desired tags as bound.
type fakeTagBinder struct {
	syncCalls   int
	removeCalls int
	lastSecret  string
	err         error
}

func (f *fakeTagBinder) SyncTags(_ context.Context, secretName string, desired map[string]string) (map[string]string, error) {
	f.syncCalls++
	f.lastSecret = secretName
	if f.err != nil {
		return nil, f.err
	}
	return maps.Clone(desired), nil
}

func (f *fakeTagBinder) RemoveTags(_ context.Context, secretName string) error {
	f.removeCalls++
	f.lastSecret = secretName
	return f.err
}

var _ = Describe("GSMSecretTag Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		var binder *fakeTagBinder
		var reconciler *GSMSecretTagReconciler

		BeforeEach(func() {
			binder = &fakeTagBinder{}
			reconciler = &GSMSecretTagReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Tags:   binder,
			}

			By("creating the custom resource for the Kind GSMSecretTag")
			resource := &gsmtagcontrollerv1alpha1.GSMSecretTag{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err != nil && errors.IsNotFound(err) {
				resource = &gsmtagcontrollerv1alpha1.GSMSecretTag{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: gsmtagcontrollerv1alpha1.GSMSecretTagSpec{
						SecretTarget: "my-gsm-secret",
						Tags: map[string]string{
							"environment": "prod",
							"team":        "payments",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &gsmtagcontrollerv1alpha1.GSMSecretTag{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if errors.IsNotFound(err) {
				return
			}
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance GSMSecretTag")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			// The finalizer holds the resource until a reconcile handles the
			// deletion.
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should sync tags and record them in status", func() {
			By("Reconciling the created resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(binder.syncCalls).To(Equal(1))
			Expect(binder.lastSecret).To(Equal("my-gsm-secret"))

			resource := &gsmtagcontrollerv1alpha1.GSMSecretTag{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			Expect(resource.Finalizers).To(ContainElement(Finalizer))
			Expect(resource.Status.ObservedTags).To(Equal(resource.Spec.Tags))

			ready := meta.FindStatusCondition(resource.Status.Conditions, ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should skip GCP calls when status already matches spec", func() {
			By("Reconciling twice")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(binder.syncCalls).To(Equal(1))
		})
	})
})
