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

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
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
	// Check for generation changes first, as that's the primary trigger for spec updates.
	if p.GenerationChangedPredicate.Update(e) {
		return true
	}

	// Now, check for status changes, specifically the Ready field.
	// It's important to type-assert the objects to the correct type.
	oldSveltosCluster, ok := e.ObjectOld.(*libsveltosv1beta1.SveltosCluster)
	if !ok {
		// If the old object is not a SveltosCluster, we can't compare statuses.
		return false
	}

	newSveltosCluster, ok := e.ObjectNew.(*libsveltosv1beta1.SveltosCluster)
	if !ok {
		// If the new object is not a SveltosCluster, we can't compare statuses.
		return false
	}

	// Trigger reconciliation if the Ready status has changed.
	if oldSveltosCluster.Status.Ready != newSveltosCluster.Status.Ready {
		return true
	}

	if oldSveltosCluster.Status.Version != newSveltosCluster.Status.Version {
		return true
	}

	// Since FailureMessage is a pointer, you need a nil-safe comparison.
	// You can use reflect.DeepEqual for a more robust check on the pointers.
	if !reflect.DeepEqual(oldSveltosCluster.Status.FailureMessage, newSveltosCluster.Status.FailureMessage) {
		return true
	}

	// If none of the monitored fields have changed, do not trigger a reconciliation.
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

	// Type-assert the old and new objects to the Cluster type.
	oldCluster, ok := e.ObjectOld.(*clusterv1.Cluster)
	if !ok {
		return false
	}
	newCluster, ok := e.ObjectNew.(*clusterv1.Cluster)
	if !ok {
		return false
	}

	// Compare the conditions. Since the order can change, a DeepEqual is necessary.
	// The equality of conditions can be checked by comparing the status of the cluster.
	// NOTE: This check might be too broad and cause excessive reconciles.
	// A more targeted check might be to look for specific condition types or a change in the overall ready state.
	if !reflect.DeepEqual(oldCluster.Status.Conditions, newCluster.Status.Conditions) {
		return true
	}

	// Compare the Initialization status.
	if !reflect.DeepEqual(oldCluster.Status.Initialization, newCluster.Status.Initialization) {
		return true
	}

	// If none of the monitored fields have changed, do not trigger a reconciliation.
	return false
}
