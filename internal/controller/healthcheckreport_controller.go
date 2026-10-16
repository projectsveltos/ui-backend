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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// HealthCheckReportReconciler reconciles HealthCheckReports: the results of the health monitoring.
type HealthCheckReportReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	ConcurrentReconciles int
}

//+kubebuilder:rbac:groups=lib.projectsveltos.io,resources=healthcheckreports,verbs=get;list;watch
//+kubebuilder:rbac:groups=lib.projectsveltos.io,resources=healthcheckreports/status,verbs=get;list;watch

func (r *HealthCheckReportReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.V(logs.LogInfo).Info("Reconciling")

	report := &libsveltosv1beta1.HealthCheckReport{}
	if err := r.Get(ctx, req.NamespacedName, report); err != nil {
		if apierrors.IsNotFound(err) {
			r.removeHealthCheckReport(req.Namespace, req.Name, logger)
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to fetch HealthCheckReport")
		return reconcile.Result{}, errors.Wrapf(err, "Failed to fetch HealthCheckReport %s", req.NamespacedName)
	}

	// Like for ClusterSummary: a non-zero DeletionTimestamp only means removal has been requested.
	r.reconcileNormal(report, logger)

	return reconcile.Result{}, nil
}

func (r *HealthCheckReportReconciler) removeHealthCheckReport(namespace, name string, logger logr.Logger) {
	manager := server.GetManagerInstance()
	if manager == nil {
		logger.V(logs.LogInfo).Info("Manager not initialized yet, skipping")
		return
	}

	manager.RemoveHealthCheckReport(namespace, name)
}

func (r *HealthCheckReportReconciler) reconcileNormal(report *libsveltosv1beta1.HealthCheckReport,
	logger logr.Logger) {

	manager := server.GetManagerInstance()
	if manager == nil {
		logger.V(logs.LogInfo).Info("Manager not initialized yet, skipping")
		return
	}

	manager.AddHealthCheckReport(report)
}

// SetupWithManager sets up the controller with the Manager.
func (r *HealthCheckReportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&libsveltosv1beta1.HealthCheckReport{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "error creating controller")
	}

	return nil
}
