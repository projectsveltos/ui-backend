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

package server_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("Stats", func() {
	var c client.Client
	var logger logr.Logger
	var sc *runtime.Scheme

	BeforeEach(func() {
		var err error
		sc, err = setupScheme()
		Expect(err).To(BeNil())
		logger = textlogger.NewLogger(textlogger.NewConfig())
	})

	It("CountClusters returns separate CAPI and Sveltos totals", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		initial, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())

		sc1 := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
			Status:     libsveltosv1beta1.SveltosClusterStatus{Ready: true},
		}
		sc2 := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
		}
		capiCluster := createTestCAPICluster(randomString(), randomString())

		manager.AddSveltosCluster(sc1)
		manager.AddSveltosCluster(sc2)
		manager.AddCAPICluster(capiCluster)
		defer func() {
			manager.RemoveSveltosCluster(sc1.Namespace, sc1.Name)
			manager.RemoveSveltosCluster(sc2.Namespace, sc2.Name)
			manager.RemoveCAPICluster(capiCluster.Namespace, capiCluster.Name)
		}()

		counts, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())
		Expect(counts.CAPITotal()).To(Equal(initial.CAPITotal() + 1))
		Expect(counts.SveltosTotal()).To(Equal(initial.SveltosTotal() + 2))
	})

	It("CountClusters tracks not-ready Sveltos clusters", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		initial, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())

		readyCluster := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
			Status:     libsveltosv1beta1.SveltosClusterStatus{Ready: true},
		}
		notReadyCluster := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
			Status:     libsveltosv1beta1.SveltosClusterStatus{Ready: false},
		}

		manager.AddSveltosCluster(readyCluster)
		manager.AddSveltosCluster(notReadyCluster)
		defer func() {
			manager.RemoveSveltosCluster(readyCluster.Namespace, readyCluster.Name)
			manager.RemoveSveltosCluster(notReadyCluster.Namespace, notReadyCluster.Name)
		}()

		counts, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())
		Expect(counts.SveltosTotal()).To(BeNumerically(">=", 2))
		Expect(counts.SveltosNotReady()).To(Equal(initial.SveltosNotReady() + 1))
	})

	It("CountClusters counts pull-mode Sveltos clusters", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		initial, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())

		pullCluster := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
			Spec:       libsveltosv1beta1.SveltosClusterSpec{PullMode: true},
		}
		normalCluster := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
		}

		manager.AddSveltosCluster(pullCluster)
		manager.AddSveltosCluster(normalCluster)
		defer func() {
			manager.RemoveSveltosCluster(pullCluster.Namespace, pullCluster.Name)
			manager.RemoveSveltosCluster(normalCluster.Namespace, normalCluster.Name)
		}()

		counts, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())
		Expect(counts.PullMode()).To(Equal(initial.PullMode() + 1))
	})

	It("CountClusters decreases when a cluster is removed", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		sveltosCluster := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
		}
		manager.AddSveltosCluster(sveltosCluster)
		// no defer needed — removal is the point of this test

		before, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())

		manager.RemoveSveltosCluster(sveltosCluster.Namespace, sveltosCluster.Name)

		after, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())
		Expect(after.SveltosTotal()).To(Equal(before.SveltosTotal() - 1))
	})

	It("CountProfilesByKind correctly separates ClusterProfiles from Profiles", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		initialCP, initialP, err := manager.CountProfilesByKind(ctx, true, true, randomString())
		Expect(err).To(BeNil())

		tier := int32(100)

		cp1 := &corev1.ObjectReference{
			Name:       randomString(),
			Kind:       configv1beta1.ClusterProfileKind,
			APIVersion: configv1beta1.GroupVersion.String(),
		}
		cp2 := &corev1.ObjectReference{
			Name:       randomString(),
			Kind:       configv1beta1.ClusterProfileKind,
			APIVersion: configv1beta1.GroupVersion.String(),
		}
		p1 := &corev1.ObjectReference{
			Namespace:  randomString(),
			Name:       randomString(),
			Kind:       configv1beta1.ProfileKind,
			APIVersion: configv1beta1.GroupVersion.String(),
		}

		manager.AddProfile(cp1, libsveltosv1beta1.Selector{}, tier, configv1beta1.SyncModeContinuous, nil)
		manager.AddProfile(cp2, libsveltosv1beta1.Selector{}, tier, configv1beta1.SyncModeContinuous, nil)
		manager.AddProfile(p1, libsveltosv1beta1.Selector{}, tier, configv1beta1.SyncModeContinuous, nil)
		defer func() {
			manager.RemoveProfile(cp1)
			manager.RemoveProfile(cp2)
			manager.RemoveProfile(p1)
		}()

		cpCount, pCount, err := manager.CountProfilesByKind(ctx, true, true, randomString())
		Expect(err).To(BeNil())
		Expect(cpCount).To(Equal(initialCP + 2))
		Expect(pCount).To(Equal(initialP + 1))
	})

	It("CountProfilesByKind decreases when a profile is removed", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		cp := &corev1.ObjectReference{
			Name:       randomString(),
			Kind:       configv1beta1.ClusterProfileKind,
			APIVersion: configv1beta1.GroupVersion.String(),
		}
		manager.AddProfile(cp, libsveltosv1beta1.Selector{}, 100, configv1beta1.SyncModeContinuous, nil)
		// no defer needed — removal is the point of this test

		beforeRemove, _, err := manager.CountProfilesByKind(ctx, true, true, randomString())
		Expect(err).To(BeNil())

		manager.RemoveProfile(cp)

		afterRemove, _, err := manager.CountProfilesByKind(ctx, true, true, randomString())
		Expect(err).To(BeNil())
		Expect(afterRemove).To(Equal(beforeRemove - 1))
	})

	It("CountClusterSummaries returns the number of valid ClusterSummaries in the cache", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		initialCount := manager.CountClusterSummaries()

		cluster := createTestCAPICluster(randomString(), randomString())
		cs1 := createTestClusterSummary("stats-summary1-"+randomString(), cluster.Namespace,
			cluster.Namespace, cluster.Name, nil)
		cs2 := createTestClusterSummary("stats-summary2-"+randomString(), cluster.Namespace,
			cluster.Namespace, cluster.Name, nil)

		manager.AddClusterProfileStatus(cs1)
		manager.AddClusterProfileStatus(cs2)
		defer func() {
			manager.RemoveClusterProfileStatus(cs1.Namespace, cs1.Name)
			manager.RemoveClusterProfileStatus(cs2.Namespace, cs2.Name)
		}()

		Expect(manager.CountClusterSummaries()).To(Equal(initialCount + 2))
	})

	It("CountClusterSummaries decreases when a ClusterSummary is removed", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		cluster := createTestCAPICluster(randomString(), randomString())
		cs := createTestClusterSummary("stats-removal-"+randomString(), cluster.Namespace,
			cluster.Namespace, cluster.Name, nil)

		manager.AddClusterProfileStatus(cs)
		// no defer needed — removal is the point of this test

		beforeRemove := manager.CountClusterSummaries()

		manager.RemoveClusterProfileStatus(cs.Namespace, cs.Name)
		Expect(manager.CountClusterSummaries()).To(Equal(beforeRemove - 1))
	})

	It("cluster and profile counts are independent of each other", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, nil, c, sc, randomPort(), logger)
		manager := server.GetManagerInstance()

		initialClusters, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())
		initialCP, initialP, err := manager.CountProfilesByKind(ctx, true, true, randomString())
		Expect(err).To(BeNil())

		sveltosCluster := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
		}
		cp := &corev1.ObjectReference{
			Name:       randomString(),
			Kind:       configv1beta1.ClusterProfileKind,
			APIVersion: configv1beta1.GroupVersion.String(),
		}

		manager.AddSveltosCluster(sveltosCluster)
		manager.AddProfile(cp, libsveltosv1beta1.Selector{}, 100, configv1beta1.SyncModeContinuous, nil)
		defer func() {
			manager.RemoveSveltosCluster(sveltosCluster.Namespace, sveltosCluster.Name)
			manager.RemoveProfile(cp)
		}()

		afterClusters, err := manager.CountClusters(ctx, true, true, randomString())
		Expect(err).To(BeNil())
		cpCount, pCount, err := manager.CountProfilesByKind(ctx, true, true, randomString())
		Expect(err).To(BeNil())

		Expect(afterClusters.SveltosTotal()).To(Equal(initialClusters.SveltosTotal() + 1))
		Expect(cpCount).To(Equal(initialCP + 1))
		Expect(pCount).To(Equal(initialP))
	})
})
