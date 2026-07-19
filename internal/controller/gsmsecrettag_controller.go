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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gsmtagcontrollerv1alpha1 "github.com/theopsdev/gsm-tag-controller/api/v1alpha1"
	"github.com/theopsdev/gsm-tag-controller/internal/gcp"
)

const (
	// Finalizer guards GCP-side cleanup for deletionPolicy Delete.
	Finalizer = "gsm-tag-controller.io/finalizer"

	// ConditionReady reports whether the GSM secret's tag bindings match spec.
	ConditionReady = "Ready"
)

// GSMSecretTagReconciler reconciles a GSMSecretTag object against GCP.
type GSMSecretTagReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Tags   gcp.TagBinder
}

// +kubebuilder:rbac:groups=gsm-tag-controller.io,resources=gsmsecrettags,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gsm-tag-controller.io,resources=gsmsecrettags/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gsm-tag-controller.io,resources=gsmsecrettags/finalizers,verbs=update

// Reconcile drives the GSM secret's Resource Manager tag bindings toward
// spec.tags, recording the bound tags in status.observedTags. Errors,
// including tag keys or values missing in GCP, are surfaced on the Ready
// condition and requeued with backoff.
func (r *GSMSecretTagReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var tag gsmtagcontrollerv1alpha1.GSMSecretTag
	if err := r.Get(ctx, req.NamespacedName, &tag); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !tag.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tag)
	}

	if controllerutil.AddFinalizer(&tag, Finalizer) {
		if err := r.Update(ctx, &tag); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Actual state matches desired state: nothing to do, no GCP API calls.
	if maps.Equal(tag.Spec.Tags, tag.Status.ObservedTags) {
		if r.setReady(&tag, metav1.ConditionTrue, "Synced", "GSM secret tags match desired state") {
			return ctrl.Result{}, r.updateStatus(ctx, &tag)
		}
		return ctrl.Result{}, nil
	}

	observed, err := r.Tags.SyncTags(ctx, tag.Spec.SecretTarget, tag.Spec.Tags)
	if err != nil {
		log.Error(err, "failed to sync tags on GSM secret", "secretTarget", tag.Spec.SecretTarget)
		if r.setReady(&tag, metav1.ConditionFalse, "SyncFailed", err.Error()) {
			if statusErr := r.updateStatus(ctx, &tag); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
		}
		// Returning the error requeues with backoff; GCP-side state (e.g. a
		// missing tag key) can change without a cluster event.
		return ctrl.Result{}, err
	}

	tag.Status.ObservedTags = observed
	r.setReady(&tag, metav1.ConditionTrue, "Synced", "GSM secret tags match desired state")
	return ctrl.Result{}, r.updateStatus(ctx, &tag)
}

// reconcileDelete honors spec.deletionPolicy before releasing the finalizer.
func (r *GSMSecretTagReconciler) reconcileDelete(ctx context.Context, tag *gsmtagcontrollerv1alpha1.GSMSecretTag) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(tag, Finalizer) {
		return ctrl.Result{}, nil
	}

	if tag.Spec.DeletionPolicy == gsmtagcontrollerv1alpha1.DeletionPolicyDelete {
		if err := r.Tags.RemoveTags(ctx, tag.Spec.SecretTarget); err != nil {
			return ctrl.Result{}, err
		}
	}

	controllerutil.RemoveFinalizer(tag, Finalizer)
	if err := r.Update(ctx, tag); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	return ctrl.Result{}, nil
}

// setReady stages the Ready condition and reports whether it changed.
func (r *GSMSecretTagReconciler) setReady(tag *gsmtagcontrollerv1alpha1.GSMSecretTag, status metav1.ConditionStatus, reason, message string) bool {
	return meta.SetStatusCondition(&tag.Status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: tag.Generation,
	})
}

func (r *GSMSecretTagReconciler) updateStatus(ctx context.Context, tag *gsmtagcontrollerv1alpha1.GSMSecretTag) error {
	err := r.Status().Update(ctx, tag)
	if apierrors.IsConflict(err) {
		// A conflicting update retriggers reconciliation; don't surface it
		// as a failure.
		return nil
	}
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *GSMSecretTagReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gsmtagcontrollerv1alpha1.GSMSecretTag{}).
		Named("gsmsecrettag").
		Complete(r)
}
