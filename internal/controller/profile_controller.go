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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
	libsveltosset "github.com/projectsveltos/libsveltos/lib/set"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// ProfileReconciler reconciles a Profile object
type ProfileReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	ConcurrentReconciles int
}

//+kubebuilder:rbac:groups=config.projectsveltos.io,resources=profiles,verbs=get;list;watch
//+kubebuilder:rbac:groups=config.projectsveltos.io,resources=profiles/status,verbs=get;list;watch

func (r *ProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.V(logs.LogInfo).Info("Reconciling")

	// Fecth the Profile instance
	profile := &configv1beta1.Profile{}
	if err := r.Get(ctx, req.NamespacedName, profile); err != nil {
		if apierrors.IsNotFound(err) {
			r.removeProfile(req.Namespace, req.Name, logger)
			return reconcile.Result{}, nil
		}
		logger.Error(err, "Failed to fetch Profile")
		return reconcile.Result{}, errors.Wrapf(
			err,
			"Failed to fetch Profile %s",
			req.NamespacedName,
		)
	}

	// Handle deleted Profile
	if !profile.DeletionTimestamp.IsZero() {
		r.removeProfile(profile.Namespace, profile.Name, logger)
	} else {
		// Handle non-deleted Profile
		r.reconcileNormal(profile, logger)
	}

	return reconcile.Result{}, nil
}

func (r *ProfileReconciler) removeProfile(profileNamespace, profileName string, logger logr.Logger) {
	logger.V(logs.LogInfo).Info("Reconciling Profile delete")

	manager := server.GetManagerInstance()

	profileRef := &corev1.ObjectReference{
		Kind:       configv1beta1.ProfileKind,
		APIVersion: configv1beta1.GroupVersion.String(),
		Name:       profileName,
		Namespace:  profileNamespace,
	}

	manager.RemoveProfile(profileRef)

	logger.V(logs.LogInfo).Info("Reconcile delete success")
}

func (r *ProfileReconciler) reconcileNormal(profile *configv1beta1.Profile, logger logr.Logger) {
	logger.V(logs.LogInfo).Info("Reconciling Profile normal")

	manager := server.GetManagerInstance()

	profileRef := &corev1.ObjectReference{
		Kind:       configv1beta1.ProfileKind,
		APIVersion: configv1beta1.GroupVersion.String(),
		Name:       profile.Name,
		Namespace:  profile.Namespace,
	}

	dependencies := &libsveltosset.Set{}
	for i := range profile.Spec.DependsOn {
		dependencies.Insert(&corev1.ObjectReference{
			Kind:       configv1beta1.ProfileKind,
			APIVersion: configv1beta1.GroupVersion.String(),
			Name:       profile.Spec.DependsOn[i],
		})
	}

	manager.AddProfile(profileRef, profile.Spec.ClusterSelector, profile.Spec.Tier, dependencies)

	logger.V(logs.LogInfo).Info("Reconcile normal success")
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1beta1.Profile{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: r.ConcurrentReconciles,
		}).
		Complete(r)
}
