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

package server_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2/textlogger"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("Rollout state", func() {
	newCluster := func() *corev1.ObjectReference {
		return &corev1.ObjectReference{
			Kind:       libsveltosv1beta1.SveltosClusterKind,
			APIVersion: libsveltosv1beta1.GroupVersion.String(),
			Namespace:  randomString(),
			Name:       randomString(),
		}
	}

	readyCluster := server.ClusterInfo{Ready: true}

	statusWith := func(updated, updating []corev1.ObjectReference) *configv1beta1.Status {
		return &configv1beta1.Status{
			UpdatedClusters:  configv1beta1.Clusters{Clusters: updated},
			UpdatingClusters: configv1beta1.Clusters{Clusters: updating},
		}
	}

	Context("stateOf", func() {
		It("reports updated, updating and waiting clusters while a rollout is pending", func() {
			updated, updating, waiting := newCluster(), newCluster(), newCluster()
			tracker := server.NewRolloutTracker(statusWith(
				[]corev1.ObjectReference{*updated}, []corev1.ObjectReference{*updating}))

			Expect(tracker.StateOf(updated, readyCluster, true)).To(Equal(server.RolloutStateUpdated))
			Expect(tracker.StateOf(updating, readyCluster, true)).To(Equal(server.RolloutStateUpdating))
			Expect(tracker.StateOf(waiting, readyCluster, true)).To(Equal(server.RolloutStateWaiting))
		})

		It("reports nothing when no rollout is pending", func() {
			tracker := server.NewRolloutTracker(statusWith(nil, nil))

			Expect(tracker.StateOf(newCluster(), readyCluster, true)).To(Equal(server.RolloutState("")))
		})

		It("reports a paused cluster even when no rollout is pending", func() {
			tracker := server.NewRolloutTracker(statusWith(nil, nil))

			Expect(tracker.StateOf(newCluster(), server.ClusterInfo{Ready: true, Paused: true}, true)).
				To(Equal(server.RolloutStatePaused))
		})

		It("reports a paused cluster as paused even if it is in the updated or updating list", func() {
			updated, updating := newCluster(), newCluster()
			tracker := server.NewRolloutTracker(statusWith(
				[]corev1.ObjectReference{*updated}, []corev1.ObjectReference{*updating}))
			pausedCluster := server.ClusterInfo{Ready: true, Paused: true}

			Expect(tracker.StateOf(updated, pausedCluster, true)).To(Equal(server.RolloutStatePaused))
			Expect(tracker.StateOf(updating, pausedCluster, true)).To(Equal(server.RolloutStatePaused))
		})

		It("reports a cluster that is not ready and not yet updated as notReady", func() {
			updated := newCluster()
			tracker := server.NewRolloutTracker(statusWith([]corev1.ObjectReference{*updated}, nil))
			notReady := server.ClusterInfo{Ready: false}

			Expect(tracker.StateOf(newCluster(), notReady, true)).To(Equal(server.RolloutStateNotReady))
			// A cluster that already has the latest version stays updated
			Expect(tracker.StateOf(updated, notReady, true)).To(Equal(server.RolloutStateUpdated))
		})

		It("reports a cluster missing from the cache as waiting", func() {
			tracker := server.NewRolloutTracker(statusWith([]corev1.ObjectReference{*newCluster()}, nil))

			Expect(tracker.StateOf(newCluster(), server.ClusterInfo{}, false)).To(Equal(server.RolloutStateWaiting))
		})
	})

	Context("summarize", func() {
		It("counts clusters per state", func() {
			tracker := server.NewRolloutTracker(statusWith([]corev1.ObjectReference{*newCluster()}, nil))

			summary := tracker.Summarize([]server.RolloutState{
				server.RolloutStateUpdated, server.RolloutStateUpdated,
				server.RolloutStateUpdating,
				server.RolloutStateWaiting, server.RolloutStateWaiting, server.RolloutStateWaiting,
				server.RolloutStatePaused,
				server.RolloutStateNotReady,
			}, 1)

			Expect(summary).ToNot(BeNil())
			Expect(*summary).To(Equal(server.RolloutSummary{
				MaxUpdate: 1, Updated: 2, Updating: 1, Waiting: 3, Paused: 1, NotReady: 1,
			}))
		})

		It("returns nil when no rollout is pending", func() {
			tracker := server.NewRolloutTracker(statusWith(nil, nil))

			Expect(tracker.Summarize([]server.RolloutState{server.RolloutStatePaused}, 0)).To(BeNil())
		})
	})

	Context("getMaxUpdate", func() {
		It("returns 0 when maxUpdate is not set", func() {
			Expect(server.GetMaxUpdate(&configv1beta1.Spec{}, 10)).To(Equal(0))
		})

		It("returns the value when maxUpdate is a number", func() {
			maxUpdate := intstr.FromInt32(3)
			Expect(server.GetMaxUpdate(&configv1beta1.Spec{MaxUpdate: &maxUpdate}, 10)).To(Equal(3))
		})

		It("applies a percentage to the matching clusters, rounding up", func() {
			maxUpdate := intstr.FromString("25%")
			Expect(server.GetMaxUpdate(&configv1beta1.Spec{MaxUpdate: &maxUpdate}, 10)).To(Equal(3))
		})

		It("returns 0 for an invalid value", func() {
			maxUpdate := intstr.FromString("many")
			Expect(server.GetMaxUpdate(&configv1beta1.Spec{MaxUpdate: &maxUpdate}, 10)).To(Equal(0))
		})
	})

	Context("getClusterInfo", func() {
		It("returns the cached information about SveltosClusters", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sc, err := setupScheme()
			Expect(err).To(BeNil())
			logger := textlogger.NewLogger(textlogger.NewConfig())
			server.InitializeManagerInstance(ctx, nil, nil, sc, randomPort(), logger)
			manager := server.GetManagerInstance()

			sveltosCluster := &libsveltosv1beta1.SveltosCluster{
				ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
				Spec:       libsveltosv1beta1.SveltosClusterSpec{Paused: true},
				Status:     libsveltosv1beta1.SveltosClusterStatus{Ready: true},
			}
			manager.AddSveltosCluster(sveltosCluster)

			ref := &corev1.ObjectReference{
				Kind:       libsveltosv1beta1.SveltosClusterKind,
				APIVersion: libsveltosv1beta1.GroupVersion.String(),
				Namespace:  sveltosCluster.Namespace,
				Name:       sveltosCluster.Name,
			}
			info, found := manager.GetClusterInfo(ref)
			Expect(found).To(BeTrue())
			Expect(info.Paused).To(BeTrue())
			Expect(info.Ready).To(BeTrue())

			_, found = manager.GetClusterInfo(newCluster())
			Expect(found).To(BeFalse())
		})
	})
})
