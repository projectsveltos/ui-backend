package controller

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
	"github.com/projectsveltos/ui-backend/internal/server"

	configv1alpha1 "github.com/projectsveltos/addon-controller/api/v1alpha1"
)

type ClusterSummaryReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	ConcurrentReconciles int
}

func (r *ClusterSummaryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.V(logs.LogInfo).Info("Reconciling")

	clusterSummary := &configv1alpha1.ClusterSummary{}
	if err := r.Get(ctx, req.NamespacedName, clusterSummary); err != nil {
		if apierrors.IsNotFound(err) {
			r.removeClusterSummary(req.Namespace, req.Name, logger)
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to fetch ClusterSummary")
		return reconcile.Result{}, errors.Wrapf(
			err,
			"Failed to fetch ClusterSummary %s",
			req.NamespacedName,
		)
	}

	// Handle deleted ClusterSummary
	if !clusterSummary.DeletionTimestamp.IsZero() {
		r.removeClusterSummary(clusterSummary.Namespace, clusterSummary.Name, logger)
	} else {
		// Handle non-deleted ClusterSummary
		r.reconcileNormal(clusterSummary, logger)
	}

	return reconcile.Result{}, nil
}

func (r *ClusterSummaryReconciler) removeClusterSummary(clusterSummaryNamespace, clusterSummaryName string, logger logr.Logger) {
	logger.V(logs.LogInfo).Info("Reconciling Cluster delete")

	manager := server.GetManagerInstance()

	manager.RemoveClusterProfileStatus(clusterSummaryNamespace, clusterSummaryName)

	logger.V(logs.LogInfo).Info("Reconcile delete success")
}

func (r *ClusterSummaryReconciler) reconcileNormal(clusterSummary *configv1alpha1.ClusterSummary, logger logr.Logger) {
	logger.V(logs.LogInfo).Info("Reconciling new ClusterSummary")

	manager := server.GetManagerInstance()

	manager.AddClusterProfileStatus(clusterSummary)

	logger.V(logs.LogInfo).Info("Reconciling new ClusterSummary success")
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterSummaryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.ClusterSummary{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "error creating controller")
	}

	return nil
}
