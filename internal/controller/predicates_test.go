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

package controller_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/event"

	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/controller"
)

var _ = Describe("SveltosClusterPredicate", func() {
	var sveltosCluster *libsveltosv1beta1.SveltosCluster
	var oldSveltosCluster *libsveltosv1beta1.SveltosCluster

	BeforeEach(func() {
		sveltosCluster = &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  randomString(),
				Name:       randomString(),
				Generation: 1,
			},
		}
		oldSveltosCluster = sveltosCluster.DeepCopy()
	})

	It("Create returns true", func() {
		sveltosClusterPredicate := controller.SveltosClusterPredicate{}

		e := event.CreateEvent{
			Object: sveltosCluster,
		}

		Expect(sveltosClusterPredicate.Create(e)).To(BeTrue())
	})

	It("Delete returns true", func() {
		sveltosClusterPredicate := controller.SveltosClusterPredicate{}

		e := event.DeleteEvent{
			Object: sveltosCluster,
		}

		Expect(sveltosClusterPredicate.Delete(e)).To(BeTrue())
	})

	It("Update returns false when nothing relevant has changed", func() {
		sveltosClusterPredicate := controller.SveltosClusterPredicate{}

		e := event.UpdateEvent{
			ObjectNew: sveltosCluster,
			ObjectOld: oldSveltosCluster,
		}

		Expect(sveltosClusterPredicate.Update(e)).To(BeFalse())
	})

	It("Update returns true when Spec.Paused has changed", func() {
		sveltosClusterPredicate := controller.SveltosClusterPredicate{}
		sveltosCluster.Spec.Paused = true

		e := event.UpdateEvent{
			ObjectNew: sveltosCluster,
			ObjectOld: oldSveltosCluster,
		}

		Expect(sveltosClusterPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when Status.Ready has changed", func() {
		sveltosClusterPredicate := controller.SveltosClusterPredicate{}
		sveltosCluster.Status.Ready = true

		e := event.UpdateEvent{
			ObjectNew: sveltosCluster,
			ObjectOld: oldSveltosCluster,
		}

		Expect(sveltosClusterPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when Status.Version has changed", func() {
		sveltosClusterPredicate := controller.SveltosClusterPredicate{}
		sveltosCluster.Status.Version = randomString()

		e := event.UpdateEvent{
			ObjectNew: sveltosCluster,
			ObjectOld: oldSveltosCluster,
		}

		Expect(sveltosClusterPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when Status.FailureMessage has changed", func() {
		sveltosClusterPredicate := controller.SveltosClusterPredicate{}
		msg := randomString()
		sveltosCluster.Status.FailureMessage = &msg

		e := event.UpdateEvent{
			ObjectNew: sveltosCluster,
			ObjectOld: oldSveltosCluster,
		}

		Expect(sveltosClusterPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when Labels have changed", func() {
		sveltosClusterPredicate := controller.SveltosClusterPredicate{}
		sveltosCluster.Labels = map[string]string{randomString(): randomString()}

		e := event.UpdateEvent{
			ObjectNew: sveltosCluster,
			ObjectOld: oldSveltosCluster,
		}

		Expect(sveltosClusterPredicate.Update(e)).To(BeTrue())
	})
})

var _ = Describe("ClusterStatusPredicate", func() {
	var cluster *clusterv1.Cluster
	var oldCluster *clusterv1.Cluster

	BeforeEach(func() {
		cluster = &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:  randomString(),
				Name:       randomString(),
				Generation: 1,
			},
		}
		oldCluster = cluster.DeepCopy()
	})

	It("Create returns true", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}

		e := event.CreateEvent{
			Object: cluster,
		}

		Expect(clusterStatusPredicate.Create(e)).To(BeTrue())
	})

	It("Delete returns true", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}

		e := event.DeleteEvent{
			Object: cluster,
		}

		Expect(clusterStatusPredicate.Delete(e)).To(BeTrue())
	})

	It("Update returns false when nothing relevant has changed", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}

		e := event.UpdateEvent{
			ObjectNew: cluster,
			ObjectOld: oldCluster,
		}

		Expect(clusterStatusPredicate.Update(e)).To(BeFalse())
	})

	It("Update returns true when ControlPlaneInitializedCondition transitions to true", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}
		cluster.Status.Conditions = []metav1.Condition{
			{
				Type:               clusterv1.ClusterControlPlaneInitializedCondition,
				Status:             metav1.ConditionTrue,
				Reason:             randomString(),
				LastTransitionTime: metav1.Now(),
			},
		}

		e := event.UpdateEvent{
			ObjectNew: cluster,
			ObjectOld: oldCluster,
		}

		Expect(clusterStatusPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when InfrastructureProvisioned transitions to true", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}
		cluster.Status.Initialization.InfrastructureProvisioned = ptr.To(true)

		e := event.UpdateEvent{
			ObjectNew: cluster,
			ObjectOld: oldCluster,
		}

		Expect(clusterStatusPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when ControlPlaneInitialized transitions to true", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}
		cluster.Status.Initialization.ControlPlaneInitialized = ptr.To(true)

		e := event.UpdateEvent{
			ObjectNew: cluster,
			ObjectOld: oldCluster,
		}

		Expect(clusterStatusPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when Phase transitions to Deleting", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}
		cluster.Status.Phase = string(clusterv1.ClusterPhaseDeleting)

		e := event.UpdateEvent{
			ObjectNew: cluster,
			ObjectOld: oldCluster,
		}

		Expect(clusterStatusPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when Labels have changed", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}
		cluster.Labels = map[string]string{randomString(): randomString()}

		e := event.UpdateEvent{
			ObjectNew: cluster,
			ObjectOld: oldCluster,
		}

		Expect(clusterStatusPredicate.Update(e)).To(BeTrue())
	})

	It("Update returns true when Spec.Paused has changed", func() {
		clusterStatusPredicate := controller.ClusterStatusPredicate{}
		cluster.Spec.Paused = ptr.To(true)

		e := event.UpdateEvent{
			ObjectNew: cluster,
			ObjectOld: oldCluster,
		}

		Expect(clusterStatusPredicate.Update(e)).To(BeTrue())
	})
})
