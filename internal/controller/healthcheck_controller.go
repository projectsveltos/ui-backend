/*
Copyright 2026. projectsveltos.io. All rights reserved.

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

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// HealthCheckReconciler reconciles the HealthChecks generated for a ClusterSummary when health
// monitoring is enabled. Only the metadata is watched: the labels and annotations tell which
// ClusterSummary a HealthCheck belongs to, nothing else is needed.
type HealthCheckReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	ConcurrentReconciles int
}

//+kubebuilder:rbac:groups=lib.projectsveltos.io,resources=healthchecks,verbs=get;list;watch

func (r *HealthCheckReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.V(logs.LogInfo).Info("Reconciling")

	healthCheck := &metav1.PartialObjectMetadata{}
	healthCheck.SetGroupVersionKind(libsveltosv1beta1.GroupVersion.WithKind(libsveltosv1beta1.HealthCheckKind))
	if err := r.Get(ctx, req.NamespacedName, healthCheck); err != nil {
		if apierrors.IsNotFound(err) {
			r.removeHealthCheck(req.Name, logger)
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to fetch HealthCheck")
		return reconcile.Result{}, errors.Wrapf(err, "Failed to fetch HealthCheck %s", req.NamespacedName)
	}

	// Like for ClusterSummary: a non-zero DeletionTimestamp only means removal has been requested.
	r.reconcileNormal(healthCheck, logger)

	return reconcile.Result{}, nil
}

func (r *HealthCheckReconciler) removeHealthCheck(name string, logger logr.Logger) {
	manager := server.GetManagerInstance()
	if manager == nil {
		logger.V(logs.LogInfo).Info("Manager not initialized yet, skipping")
		return
	}

	manager.RemoveHealthCheck(name)
}

func (r *HealthCheckReconciler) reconcileNormal(healthCheck metav1.Object, logger logr.Logger) {
	manager := server.GetManagerInstance()
	if manager == nil {
		logger.V(logs.LogInfo).Info("Manager not initialized yet, skipping")
		return
	}

	manager.AddHealthCheck(healthCheck)
}

// SetupWithManager sets up the controller with the Manager.
func (r *HealthCheckReconciler) SetupWithManager(mgr ctrl.Manager) error {
	healthCheck := &metav1.PartialObjectMetadata{}
	healthCheck.SetGroupVersionKind(libsveltosv1beta1.GroupVersion.WithKind(libsveltosv1beta1.HealthCheckKind))

	_, err := ctrl.NewControllerManagedBy(mgr).
		Named("healthcheck").
		For(healthCheck, builder.OnlyMetadata, builder.WithPredicates(GeneratedHealthCheckPredicate{})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "error creating controller")
	}

	return nil
}
