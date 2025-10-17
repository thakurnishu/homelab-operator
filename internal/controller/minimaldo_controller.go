/*
Copyright 2025.

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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	applicationv1alpha1 "github.com/thakurnishu/homelab-operator/api/v1alpha1"
)

// MinimalDoReconciler reconciles a MinimalDo object
type MinimalDoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=application.homelab.nishantlabs.cloud,resources=minimaldoes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=application.homelab.nishantlabs.cloud,resources=minimaldoes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=application.homelab.nishantlabs.cloud,resources=minimaldoes/finalizers,verbs=update

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=scheduledbackups,verbs=get;list;watch;create;update;patch;delete

func (r *MinimalDoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	minimaldo := &applicationv1alpha1.MinimalDo{}

	if err := r.Get(ctx, req.NamespacedName, minimaldo); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if minimaldo.Spec.Frontend.Replicas == 0 {
		minimaldo.Spec.Frontend.Replicas = 1
	}
	if minimaldo.Spec.Backend.Replicas == 0 {
		minimaldo.Spec.Backend.Replicas = 1
	}

	// create child resources
	objects, err := minimaldo.Construct()
	if err != nil {
		logger.Error(err, "failed to construct objects")
		return ctrl.Result{}, err
	}

	for _, object := range objects {

		// set owner reference for resource
		if err := ctrl.SetControllerReference(minimaldo, object, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}

		// Use Server-side Apply
		if err := r.Client.Patch(ctx, object, client.Apply, client.ForceOwnership, client.FieldOwner("minimaldo-controller")); err != nil {
			logger.Error(err, "failed to apply object",
				"kind", object.GetObjectKind().GroupVersionKind().Kind,
				"name", object.GetName())
			return ctrl.Result{}, err
		}

		logger.Info("object applied successfully",
			"kind", object.GetObjectKind().GroupVersionKind().Kind,
			"name", object.GetName())
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MinimalDoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&applicationv1alpha1.MinimalDo{}).
		Named("minimaldo").
		Complete(r)
}
