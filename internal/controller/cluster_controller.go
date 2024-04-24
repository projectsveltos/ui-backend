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

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// ClusterReconciler reconciles a Cluster object
type ClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/status,verbs=get;list;watch

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.V(logs.LogInfo).Info("Reconciling")

	// Fecth the ClusterAPI powered cluster instance
	cluster := &clusterv1.Cluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.removeCAPICluster(req.Namespace, req.Name, logger)
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to fetch Cluster")
		return reconcile.Result{}, errors.Wrapf(
			err,
			"Failed to fetch Cluster %s",
			req.NamespacedName,
		)
	}

	// Handle deleted cluster
	if !cluster.DeletionTimestamp.IsZero() {
		r.removeCAPICluster(cluster.Namespace, cluster.Name, logger)
	} else {
		// Handle non-deleted cluster
		r.reconcileNormal(cluster, logger)
	}

	return reconcile.Result{}, nil
}

func (r *ClusterReconciler) removeCAPICluster(clusterNamespace, clusterName string, logger logr.Logger) {
	logger.V(logs.LogInfo).Info("Reconciling Cluster delete")

	manager := server.GetManagerInstance()

	manager.RemoveCAPICluster(clusterNamespace, clusterName)

	logger.V(logs.LogInfo).Info("Reconcile delete success")
}

func (r *ClusterReconciler) reconcileNormal(cluster *clusterv1.Cluster, logger logr.Logger) {
	logger.V(logs.LogInfo).Info("Reconciling Cluster normal")

	manager := server.GetManagerInstance()

	manager.AddCAPICluster(cluster)

	logger.V(logs.LogInfo).Info("Reconcile normal success")
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&clusterv1.Cluster{}).
		Complete(r)
}
