/*
Copyright 2025. projectsveltos.io. All rights reserved.

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
	"reflect"

	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

// SveltosClusterPredicate is a custom predicate that triggers reconciliation on generation changes
// or when the SveltosCluster's Status.Ready field changes.
type SveltosClusterPredicate struct {
	predicate.GenerationChangedPredicate
}

// Update implements Predicate.
func (p SveltosClusterPredicate) Update(e event.UpdateEvent) bool {
	if p.GenerationChangedPredicate.Update(e) {
		return true
	}

	oldSveltosCluster, ok := e.ObjectOld.(*libsveltosv1beta1.SveltosCluster)
	if !ok {
		return true
	}

	newSveltosCluster, ok := e.ObjectNew.(*libsveltosv1beta1.SveltosCluster)
	if !ok {
		return false
	}

	if oldSveltosCluster.Spec.Paused != newSveltosCluster.Spec.Paused {
		return true
	}

	if oldSveltosCluster.Status.Ready != newSveltosCluster.Status.Ready {
		return true
	}

	if oldSveltosCluster.Status.Version != newSveltosCluster.Status.Version {
		return true
	}

	if !reflect.DeepEqual(oldSveltosCluster.Status.FailureMessage, newSveltosCluster.Status.FailureMessage) {
		return true
	}

	return false
}

// ClusterStatusPredicate is a custom predicate that triggers reconciliation on generation changes
// or when the Cluster's status.conditions or status.initialization fields change.
type ClusterStatusPredicate struct {
	predicate.GenerationChangedPredicate
}

// Update implements Predicate.
func (p ClusterStatusPredicate) Update(e event.UpdateEvent) bool {
	// A generation change indicates a change in the spec, which should always trigger a reconcile.
	if p.GenerationChangedPredicate.Update(e) {
		return true
	}

	oldCluster, ok := e.ObjectOld.(*clusterv1.Cluster)
	if !ok {
		return false
	}
	newCluster, ok := e.ObjectNew.(*clusterv1.Cluster)
	if !ok {
		return false
	}

	if !conditions.IsTrue(oldCluster, clusterv1.ClusterControlPlaneInitializedCondition) &&
		conditions.IsTrue(newCluster, clusterv1.ClusterControlPlaneInitializedCondition) {

		return true
	}

	if !ptr.Deref(oldCluster.Status.Initialization.InfrastructureProvisioned, false) &&
		ptr.Deref(newCluster.Status.Initialization.InfrastructureProvisioned, false) {

		return true
	}

	if !ptr.Deref(oldCluster.Status.Initialization.ControlPlaneInitialized, false) &&
		ptr.Deref(newCluster.Status.Initialization.ControlPlaneInitialized, false) {

		return true
	}

	if oldCluster.Status.Phase != string(clusterv1.ClusterPhaseDeleting) &&
		newCluster.Status.Phase == string(clusterv1.ClusterPhaseDeleting) {

		return true
	}

	oldPaused := ptr.Deref(oldCluster.Spec.Paused, false)
	newPaused := ptr.Deref(newCluster.Spec.Paused, false)
	return oldPaused != newPaused
}
