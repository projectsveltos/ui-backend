/*
Copyright 2024. projectsveltos.io. All rights reserved.

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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"

	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// SveltosClusterReconciler reconciles a SveltosCluster object
type SveltosClusterReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	ConcurrentReconciles int
}

//+kubebuilder:rbac:groups=lib.projectsveltos.io,resources=sveltosclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=lib.projectsveltos.io,resources=sveltosclusters/status,verbs=get;list;watch

func (r *SveltosClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.V(logs.LogInfo).Info("Reconciling")

	// Fecth the sveltosCluster instance
	sveltosCluster := &libsveltosv1beta1.SveltosCluster{}
	if err := r.Get(ctx, req.NamespacedName, sveltosCluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.removeSveltosCluster(req.Namespace, req.Name, logger)
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to fetch Cluster")
		return reconcile.Result{}, errors.Wrapf(
			err,
			"Failed to fetch Cluster %s",
			req.NamespacedName,
		)
	}

	// Handle deleted SveltosCluster
	if !sveltosCluster.DeletionTimestamp.IsZero() {
		r.removeSveltosCluster(sveltosCluster.Namespace, sveltosCluster.Name, logger)
	} else {
		// Handle non-deleted sveltosCluster
		r.reconcileNormal(sveltosCluster, logger)
	}

	return reconcile.Result{}, nil
}

func (r *SveltosClusterReconciler) removeSveltosCluster(sveltosClusterNamespace, sveltosClusterName string, logger logr.Logger) {
	logger.V(logs.LogInfo).Info("Reconciling SveltosCluster delete")

	manager := server.GetManagerInstance()

	manager.RemoveSveltosCluster(sveltosClusterNamespace, sveltosClusterName)

	logger.V(logs.LogInfo).Info("Reconcile delete success")
}

func (r *SveltosClusterReconciler) reconcileNormal(sveltosCluster *libsveltosv1beta1.SveltosCluster, logger logr.Logger) {
	logger.V(logs.LogInfo).Info("Reconciling SveltosCluster normal")

	manager := server.GetManagerInstance()

	manager.AddSveltosCluster(sveltosCluster)

	logger.V(logs.LogInfo).Info("Reconcile normal success")
}

// SetupWithManager sets up the controller with the Manager.
func (r *SveltosClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&libsveltosv1beta1.SveltosCluster{}, builder.WithPredicates(&SveltosClusterPredicate{})).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "error creating controller")
	}

	return nil
}
