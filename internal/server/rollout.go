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

package server

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosset "github.com/projectsveltos/libsveltos/lib/set"
)

// RolloutState tells where a matching cluster is in the rollout of the latest profile version.
// A profile with maxUpdate set is not deployed to all matching clusters at once: only maxUpdate
// clusters are updated in parallel, the others wait for a free slot.
type RolloutState string

const (
	// RolloutStateUpdated: the cluster runs the latest version of the profile
	RolloutStateUpdated RolloutState = "updated"
	// RolloutStateUpdating: the latest version of the profile is being deployed to the cluster
	RolloutStateUpdating RolloutState = "updating"
	// RolloutStateWaiting: the cluster is queued, maxUpdate clusters are already being updated
	RolloutStateWaiting RolloutState = "waiting"
	// RolloutStatePaused: the cluster is paused, the rollout skips it
	RolloutStatePaused RolloutState = "paused"
	// RolloutStateNotReady: the cluster is not ready to be configured, the rollout skips it
	RolloutStateNotReady RolloutState = "notReady"
)

// RolloutSummary counts the matching clusters in each rollout state. It is only reported while
// a rollout is pending.
type RolloutSummary struct {
	// MaxUpdate is the number of clusters that can be updated in parallel. 0 when maxUpdate is not set.
	MaxUpdate int `json:"maxUpdate"`
	Updated   int `json:"updated"`
	Updating  int `json:"updating"`
	Waiting   int `json:"waiting"`
	Paused    int `json:"paused"`
	NotReady  int `json:"notReady"`
}

// rolloutTracker computes the rollout state of the matching clusters of a profile
// from the profile status.
type rolloutTracker struct {
	updated  *libsveltosset.Set
	updating *libsveltosset.Set
}

func newRolloutTracker(status *configv1beta1.Status) *rolloutTracker {
	tracker := &rolloutTracker{
		updated:  &libsveltosset.Set{},
		updating: &libsveltosset.Set{},
	}

	for i := range status.UpdatedClusters.Clusters {
		tracker.updated.Insert(&status.UpdatedClusters.Clusters[i])
	}
	for i := range status.UpdatingClusters.Clusters {
		tracker.updating.Insert(&status.UpdatingClusters.Clusters[i])
	}

	return tracker
}

// isPending returns true when a rollout is in progress. The profile controller resets both
// lists once every matching cluster has the latest version.
func (r *rolloutTracker) isPending() bool {
	return r.updated.Len() > 0 || r.updating.Len() > 0
}

// stateOf returns the rollout state of cluster. clusterInfo is the cached information about the
// cluster, found is false when the cluster is not in the cache.
// An empty state is returned when there is nothing to report about the cluster.
func (r *rolloutTracker) stateOf(cluster *corev1.ObjectReference, clusterInfo ClusterInfo,
	found bool) RolloutState {

	// A paused cluster is never updated by the rollout, whatever its place in the queue.
	if found && clusterInfo.Paused {
		return RolloutStatePaused
	}

	if !r.isPending() {
		return ""
	}

	if r.updated.Has(cluster) {
		return RolloutStateUpdated
	}
	if r.updating.Has(cluster) {
		return RolloutStateUpdating
	}

	if found && !clusterInfo.Ready {
		return RolloutStateNotReady
	}

	return RolloutStateWaiting
}

// summarize counts the states. It returns nil when no rollout is pending.
func (r *rolloutTracker) summarize(states []RolloutState, maxUpdate int) *RolloutSummary {
	if !r.isPending() {
		return nil
	}

	summary := &RolloutSummary{MaxUpdate: maxUpdate}
	for _, state := range states {
		switch state {
		case RolloutStateUpdated:
			summary.Updated++
		case RolloutStateUpdating:
			summary.Updating++
		case RolloutStateWaiting:
			summary.Waiting++
		case RolloutStatePaused:
			summary.Paused++
		case RolloutStateNotReady:
			summary.NotReady++
		}
	}

	return summary
}

// getMaxUpdate returns the number of clusters the profile can update in parallel.
// A percentage is applied to the number of matching clusters and rounded up, as the profile
// controller does. 0 is returned when maxUpdate is not set.
func getMaxUpdate(spec *configv1beta1.Spec, matchingClusterCount int) int {
	if spec.MaxUpdate == nil {
		return 0
	}

	value, err := intstr.GetScaledValueFromIntOrPercent(spec.MaxUpdate, matchingClusterCount, true)
	if err != nil {
		return 0
	}

	return value
}
