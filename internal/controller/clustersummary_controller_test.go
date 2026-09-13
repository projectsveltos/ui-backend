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

package controller_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/controller"
	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("ClusterSummaryReconciler", func() {
	// Regression test: a ClusterSummary whose removal is deferred (TransitionFrom successor
	// not yet Provisioned, or a DependsOn dependent still present) gets a non-zero
	// DeletionTimestamp the moment deletion is requested, but is kept alive by its own
	// finalizer, reporting a real status (Blocked), for as long as that condition holds.
	// The reconciler must not drop it from the dashboard's cache at that point: only once the
	// object is genuinely gone (a real NotFound) should it disappear.

	var clusterSummary *configv1beta1.ClusterSummary
	var logger logr.Logger

	BeforeEach(func() {
		clusterSummary = &configv1beta1.ClusterSummary{
			ObjectMeta: metav1.ObjectMeta{
				Name:       randomString(),
				Namespace:  randomString(),
				Finalizers: []string{"test.projectsveltos.io/finalizer"},
				Labels: map[string]string{
					configv1beta1.ClusterNameLabel: randomString(),
					configv1beta1.ClusterTypeLabel: string(libsveltosv1beta1.ClusterTypeCapi),
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: configv1beta1.GroupVersion.String(),
						Kind:       configv1beta1.ClusterProfileKind,
						Name:       randomString(),
					},
				},
			},
			Spec: configv1beta1.ClusterSummarySpec{
				ClusterNamespace: randomString(),
				ClusterName:      randomString(),
				ClusterType:      libsveltosv1beta1.ClusterTypeCapi,
			},
			Status: configv1beta1.ClusterSummaryStatus{
				FeatureSummaries: []configv1beta1.FeatureSummary{
					{
						FeatureID: libsveltosv1beta1.FeatureHelm,
						Status:    libsveltosv1beta1.FeatureStatusProvisioned,
					},
				},
			},
		}

		logger = textlogger.NewLogger(textlogger.NewConfig())
	})

	It("keeps a deferred (Blocked) ClusterSummary visible, and only drops it once truly gone", func() {
		initObjects := []client.Object{clusterSummary}

		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(initObjects...).
			WithObjects(initObjects...).Build()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		server.InitializeManagerInstance(ctx, nil, c, scheme, httpPort, logger)

		reconciler := &controller.ClusterSummaryReconciler{
			Client: c,
			Scheme: scheme,
		}

		csKey := client.ObjectKey{Namespace: clusterSummary.Namespace, Name: clusterSummary.Name}
		csRef := &corev1.ObjectReference{
			Namespace:  clusterSummary.Namespace,
			Name:       clusterSummary.Name,
			Kind:       configv1beta1.ClusterSummaryKind,
			APIVersion: configv1beta1.GroupVersion.String(),
		}

		manager := server.GetManagerInstance()

		// Not yet being deleted: present, as normal.
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: csKey})
		Expect(err).ToNot(HaveOccurred())
		statuses := manager.GetClusterProfileStatuses()
		_, ok := statuses[*csRef]
		Expect(ok).To(BeTrue())

		// Simulate addon-controller deferring teardown: the ClusterProfile no longer matches,
		// so the ClusterSummary is marked for deletion, but its own finalizer (standing in for
		// the real TransitionFrom/DependsOn deferral) keeps it alive with a Blocked status.
		Expect(c.Delete(ctx, clusterSummary)).To(Succeed())

		blocked := &configv1beta1.ClusterSummary{}
		Expect(c.Get(ctx, csKey, blocked)).To(Succeed())
		Expect(blocked.DeletionTimestamp.IsZero()).To(BeFalse())
		blocked.Status.FeatureSummaries[0].Status = libsveltosv1beta1.FeatureStatusBlocked
		Expect(c.Status().Update(ctx, blocked)).To(Succeed())

		// The object still exists (DeletionTimestamp set, Blocked): must still be reflected,
		// not dropped from the cache the instant deletion was requested.
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: csKey})
		Expect(err).ToNot(HaveOccurred())
		statuses = manager.GetClusterProfileStatuses()
		entry, ok := statuses[*csRef]
		Expect(ok).To(BeTrue())
		Expect(entry.Summary[0].Status).To(Equal(libsveltosv1beta1.FeatureStatusBlocked))

		// The deferral condition clears (e.g. the TransitionFrom successor reaches
		// Provisioned): the finalizer is removed, and the fake client (matching real API
		// server semantics for an object already marked for deletion) finalizes the delete.
		blocked.Finalizers = nil
		Expect(c.Update(ctx, blocked)).To(Succeed())
		Expect(apierrors.IsNotFound(c.Get(ctx, csKey, &configv1beta1.ClusterSummary{}))).To(BeTrue())

		// Only now, once the object is genuinely gone, must it disappear from the cache.
		// (Real Delete events always reach Reconcile regardless of the predicate's Update
		// override, since only Update is customized: Create/Delete/Generic are inherited as
		// always-true from the embedded GenerationChangedPredicate.)
		_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: csKey})
		Expect(err).ToNot(HaveOccurred())
		statuses = manager.GetClusterProfileStatuses()
		_, ok = statuses[*csRef]
		Expect(ok).To(BeFalse())
	})
})
