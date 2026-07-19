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
	"strings"

	esv1alpha1 "github.com/external-secrets/external-secrets/apis/externalsecrets/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	gsmtagcontrollerv1alpha1 "github.com/theopsdev/gsm-tag-controller/api/v1alpha1"
)

// TagsAnnotation holds the tag short-name pairs to bind to the GSM secret,
// e.g. "environment=prod,team=payments".
const TagsAnnotation = "gsm-tag-controller.io/tags"

// PushSecretReconciler watches ESO PushSecrets and ensures each annotated,
// provisioned PushSecret has a matching GSMSecretTag.
type PushSecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=external-secrets.io,resources=pushsecrets,verbs=get;list;watch

// Reconcile ensures a GSMSecretTag exists for a provisioned PushSecret
// carrying the tags annotation, with spec derived from the annotation and
// the PushSecret's remote key.
func (r *PushSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var ps esv1alpha1.PushSecret
	if err := r.Get(ctx, req.NamespacedName, &ps); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !ps.DeletionTimestamp.IsZero() {
		// The owned GSMSecretTag is garbage-collected via ownerReference.
		return ctrl.Result{}, nil
	}

	raw, ok := ps.Annotations[TagsAnnotation]
	if !ok {
		return ctrl.Result{}, nil
	}

	tags, err := parseTagsAnnotation(raw)
	if err != nil {
		// A malformed annotation won't fix itself; log and wait for an edit.
		log.Error(err, "invalid tags annotation on PushSecret", "annotation", TagsAnnotation)
		return ctrl.Result{}, nil
	}

	if !pushSecretProvisioned(&ps) {
		// ESO hasn't pushed the secret yet; a status change will retrigger.
		return ctrl.Result{}, nil
	}

	remoteKey, err := remoteKeyFor(&ps)
	if err != nil {
		log.Error(err, "cannot determine GSM secret target for PushSecret")
		return ctrl.Result{}, nil
	}

	tag := &gsmtagcontrollerv1alpha1.GSMSecretTag{
		ObjectMeta: metav1.ObjectMeta{Name: ps.Name, Namespace: ps.Namespace},
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, tag, func() error {
		tag.Spec.SecretTarget = remoteKey
		tag.Spec.Tags = tags
		if tag.Spec.DeletionPolicy == "" {
			tag.Spec.DeletionPolicy = gsmtagcontrollerv1alpha1.DeletionPolicyRetain
		}
		return controllerutil.SetControllerReference(&ps, tag, r.Scheme)
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	if op != controllerutil.OperationResultNone {
		log.Info("reconciled GSMSecretTag", "name", tag.Name, "operation", op)
	}
	return ctrl.Result{}, nil
}

// parseTagsAnnotation parses "k1=v1,k2=v2" into a map, trimming whitespace
// around pairs, keys, and values.
func parseTagsAnnotation(raw string) (map[string]string, error) {
	tags := make(map[string]string)
	for pair := range strings.SplitSeq(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, found := strings.Cut(pair, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !found || key == "" || value == "" {
			return nil, fmt.Errorf("malformed tag pair %q, expected key=value", pair)
		}
		tags[key] = value
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("annotation contains no tag pairs")
	}
	return tags, nil
}

// pushSecretProvisioned reports whether ESO has successfully pushed the
// secret, via the PushSecret's Ready condition.
func pushSecretProvisioned(ps *esv1alpha1.PushSecret) bool {
	for _, c := range ps.Status.Conditions {
		if c.Type == esv1alpha1.PushSecretReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// remoteKeyFor extracts the GSM secret name the PushSecret pushes to.
// TODO: support multiple data entries / dataTo bulk pushes; the design
// currently assumes one GSM secret per PushSecret.
func remoteKeyFor(ps *esv1alpha1.PushSecret) (string, error) {
	if len(ps.Spec.Data) == 0 {
		return "", fmt.Errorf("PushSecret has no spec.data entries with a remoteKey")
	}
	return ps.Spec.Data[0].Match.RemoteRef.RemoteKey, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PushSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&esv1alpha1.PushSecret{}).
		Owns(&gsmtagcontrollerv1alpha1.GSMSecretTag{}).
		Named("pushsecret").
		Complete(r)
}
