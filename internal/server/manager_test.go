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

package server_test

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2/textlogger"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/projectsveltos/addon-controller/api/v1alpha1"
	libsveltosv1alpha1 "github.com/projectsveltos/libsveltos/api/v1alpha1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

const (
	k8sVersion = "v1.29.0"
)

func randomPort() string {
	source := rand.NewSource(time.Now().UnixNano())
	//nolint: gosec // this is just a test
	rng := rand.New(source)

	// Generate a random number between 8080 and 20000
	randomNumber := rng.Intn(20000-8080) + 8080
	return fmt.Sprintf(":%d", randomNumber)
}

func createTestCAPICluster(namespace, name string) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				randomString(): randomString(),
				randomString(): randomString(),
			},
		},
		Spec: clusterv1.ClusterSpec{
			Topology: &clusterv1.Topology{
				Version: k8sVersion,
			},
		},
		Status: clusterv1.ClusterStatus{
			ControlPlaneReady: true,
		},
	}
}

func createTestClusterSummary(
	name, namespace, clusterNamespace, clusterName string,
	featureSummaries []configv1alpha1.FeatureSummary,
) *configv1alpha1.ClusterSummary {

	return &configv1alpha1.ClusterSummary{
		TypeMeta: metav1.TypeMeta{
			Kind: configv1alpha1.ClusterSummaryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{ // TODO: I didn't get where to get this cluster-profile label name (same as in isClusterSummary)...
				"projectsveltos.io/cluster-profile-name": randomString(),
				configv1alpha1.ClusterNameLabel:          clusterName,
				configv1alpha1.ClusterTypeLabel:          randomString(),
			},
		},
		Spec: configv1alpha1.ClusterSummarySpec{
			ClusterNamespace:   clusterNamespace,
			ClusterName:        clusterName,
			ClusterType:        clusterv1.ClusterKind,
			ClusterProfileSpec: configv1alpha1.Spec{},
		},
		Status: configv1alpha1.ClusterSummaryStatus{
			FeatureSummaries: featureSummaries,
		},
	}
}

var _ = Describe("Manager", func() {
	var sveltosCluster *libsveltosv1alpha1.SveltosCluster
	var cluster *clusterv1.Cluster
	var properClusterSummary *configv1alpha1.ClusterSummary
	var invalidClusterSummary *configv1alpha1.ClusterSummary
	var c client.Client
	var scheme *runtime.Scheme
	var logger logr.Logger

	BeforeEach(func() {
		var err error
		scheme, err = setupScheme()
		Expect(err).To(BeNil())

		logger = textlogger.NewLogger(textlogger.NewConfig())

		sveltosCluster = &libsveltosv1alpha1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: randomString(),
				Name:      randomString(),
				Labels: map[string]string{
					randomString(): randomString(),
					randomString(): randomString(),
				},
			},
			Status: libsveltosv1alpha1.SveltosClusterStatus{
				Ready:   true,
				Version: k8sVersion,
			},
		}

		cluster = createTestCAPICluster(randomString(), randomString())

		properClusterSummaryFailureMessage := "just an error message"
		properClusterSummary = createTestClusterSummary(
			"properSummary",
			cluster.Namespace, // is in the same namespace of the cluster
			cluster.Namespace,
			cluster.Name,
			[]configv1alpha1.FeatureSummary{
				{
					FeatureID:      "Helm",
					Status:         configv1alpha1.FeatureStatusProvisioning,
					FailureMessage: &properClusterSummaryFailureMessage,
				},
			},
		)

		invalidClusterSummary = &configv1alpha1.ClusterSummary{
			TypeMeta: metav1.TypeMeta{
				Kind: configv1alpha1.ClusterSummaryKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "summaryWithNoLabels",
			},
			Spec: configv1alpha1.ClusterSummarySpec{
				ClusterNamespace:   cluster.Namespace,
				ClusterName:        cluster.Name,
				ClusterType:        clusterv1.ClusterKind,
				ClusterProfileSpec: configv1alpha1.Spec{},
			},
			Status: configv1alpha1.ClusterSummaryStatus{},
		}
	})

	It("AddSveltosCluster adds SveltosCluster to list of managed clusters", func() {
		clusterRef := &corev1.ObjectReference{
			Namespace:  sveltosCluster.Namespace,
			Name:       sveltosCluster.Name,
			Kind:       libsveltosv1alpha1.SveltosClusterKind,
			APIVersion: libsveltosv1alpha1.GroupVersion.String(),
		}

		clusterInfo := server.ClusterInfo{
			Labels:         sveltosCluster.Labels,
			Ready:          sveltosCluster.Status.Ready,
			Version:        sveltosCluster.Status.Version,
			FailureMessage: sveltosCluster.Status.FailureMessage,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, c, scheme, randomPort(), logger)
		manager := server.GetManagerInstance()
		manager.AddSveltosCluster(sveltosCluster)

		clusters := manager.GetManagedSveltosClusters()
		v, ok := clusters[*clusterRef]
		Expect(ok).To(BeTrue())
		Expect(reflect.DeepEqual(v, clusterInfo)).To(BeTrue())
	})

	It("RemoveSveltosCluster removes SveltosCluster from list of managed clusters", func() {
		clusterRef := &corev1.ObjectReference{
			Namespace:  sveltosCluster.Namespace,
			Name:       sveltosCluster.Name,
			Kind:       libsveltosv1alpha1.SveltosClusterKind,
			APIVersion: libsveltosv1alpha1.GroupVersion.String(),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, c, scheme, randomPort(), logger)
		manager := server.GetManagerInstance()
		manager.AddSveltosCluster(sveltosCluster)

		clusters := manager.GetManagedSveltosClusters()
		_, ok := clusters[*clusterRef]
		Expect(ok).To(BeTrue())

		manager.RemoveSveltosCluster(sveltosCluster.Namespace, sveltosCluster.Name)
		clusters = manager.GetManagedSveltosClusters()
		_, ok = clusters[*clusterRef]
		Expect(ok).To(BeFalse())

		// verify operation is idempotent
		manager.RemoveSveltosCluster(sveltosCluster.Namespace, sveltosCluster.Name)
		clusters = manager.GetManagedSveltosClusters()
		_, ok = clusters[*clusterRef]
		Expect(ok).To(BeFalse())
	})

	It("AddCAPICluster adds ClusterAPI powered Cluster to list of managed clusters", func() {
		clusterRef := &corev1.ObjectReference{
			Namespace:  cluster.Namespace,
			Name:       cluster.Name,
			Kind:       clusterv1.ClusterKind,
			APIVersion: clusterv1.GroupVersion.String(),
		}

		clusterInfo := server.ClusterInfo{
			Labels:         cluster.Labels,
			Ready:          cluster.Status.ControlPlaneReady,
			Version:        cluster.Spec.Topology.Version,
			FailureMessage: cluster.Status.FailureMessage,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, c, scheme, randomPort(), logger)
		manager := server.GetManagerInstance()
		manager.AddCAPICluster(cluster)

		clusters := manager.GetManagedCAPIClusters()
		v, ok := clusters[*clusterRef]
		Expect(ok).To(BeTrue())
		Expect(reflect.DeepEqual(v, clusterInfo)).To(BeTrue())
	})

	It("RemoveCAPICluster removes ClusterAPI powered Cluster from list of managed clusters", func() {
		clusterRef := &corev1.ObjectReference{
			Namespace:  cluster.Namespace,
			Name:       cluster.Name,
			Kind:       clusterv1.ClusterKind,
			APIVersion: clusterv1.GroupVersion.String(),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, c, scheme, randomPort(), logger)
		manager := server.GetManagerInstance()
		manager.AddCAPICluster(cluster)

		clusters := manager.GetManagedCAPIClusters()
		_, ok := clusters[*clusterRef]
		Expect(ok).To(BeTrue())

		manager.RemoveCAPICluster(cluster.Namespace, cluster.Name)
		clusters = manager.GetManagedCAPIClusters()
		_, ok = clusters[*clusterRef]
		Expect(ok).To(BeFalse())

		// verify operation is idempotent
		manager.RemoveCAPICluster(cluster.Namespace, cluster.Name)
		clusters = manager.GetManagedCAPIClusters()
		_, ok = clusters[*clusterRef]
		Expect(ok).To(BeFalse())
	})

	It("AddClusterProfileStatus adds ClusterProfileStatus of a given cluster to a list of cluster profile statuses", func() {
		clusterSummaryRef := &corev1.ObjectReference{
			Namespace:  properClusterSummary.Namespace,
			Name:       properClusterSummary.Name,
			Kind:       configv1alpha1.ClusterSummaryKind,
			APIVersion: configv1alpha1.GroupVersion.String(),
		}

		clusterProfileName := properClusterSummary.Labels["projectsveltos.io/cluster-profile-name"]
		properClusterProfileStatus := server.ClusterProfileStatus{
			Name:        &clusterProfileName,
			Namespace:   &properClusterSummary.Namespace,
			ClusterName: &properClusterSummary.Spec.ClusterName,
			Summary:     properClusterSummary.Status.FeatureSummaries,
		}

		noLabelsClusterSummaryRef := &corev1.ObjectReference{
			Namespace:  invalidClusterSummary.Namespace,
			Name:       invalidClusterSummary.Name,
			Kind:       configv1alpha1.ClusterSummaryKind,
			APIVersion: configv1alpha1.GroupVersion.String(),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, c, scheme, randomPort(), logger)
		manager := server.GetManagerInstance()

		// test it has been added
		manager.AddClusterProfileStatus(properClusterSummary)
		clusterProfileStatuses := manager.GetClusterProfileStatuses()
		v, ok := clusterProfileStatuses[*clusterSummaryRef]
		Expect(ok).To(BeTrue())
		Expect(reflect.DeepEqual(v, properClusterProfileStatus)).To(BeTrue())

		// if I add a cluster summary that has not the proper labels, ignore it (not in the list)
		manager.AddClusterProfileStatus(invalidClusterSummary)
		clusterProfileStatuses = manager.GetClusterProfileStatuses()
		_, ok = clusterProfileStatuses[*noLabelsClusterSummaryRef]
		Expect(ok).To(BeFalse())
	})

	It("RemoveClusterProfileStatus removes ClusterProfileStatus object from the list of cluster profile statuses", func() {
		clusterSummaryRef := &corev1.ObjectReference{
			Namespace:  properClusterSummary.Namespace,
			Name:       properClusterSummary.Name,
			Kind:       configv1alpha1.ClusterSummaryKind,
			APIVersion: configv1alpha1.GroupVersion.String(),
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, c, scheme, randomPort(), logger)
		manager := server.GetManagerInstance()

		// test it has been added
		manager.AddClusterProfileStatus(properClusterSummary)
		manager.RemoveClusterProfileStatus(properClusterSummary.Namespace, properClusterSummary.Name)
		clusterProfileStatuses := manager.GetClusterProfileStatuses()
		_, ok := clusterProfileStatuses[*clusterSummaryRef]
		Expect(ok).To(BeFalse())
	})

	It("GetClusterProfileStatusesByCluster returns a list of ClusterProfileStatus belonging to a cluster given in input", func() {

		additionalClusterSummary := createTestClusterSummary(
			"additionalSummary",
			randomString(),
			randomString(),
			randomString(),
			[]configv1alpha1.FeatureSummary{},
		)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		server.InitializeManagerInstance(ctx, c, scheme, randomPort(), logger)
		manager := server.GetManagerInstance()

		// make sure there's already an existing cluster in the manager
		Expect(len(manager.GetManagedCAPIClusters()) == 1).To(BeTrue())
		manager.AddClusterProfileStatus(properClusterSummary)
		manager.AddClusterProfileStatus(additionalClusterSummary)

		clusterProfileStatuses := manager.GetClusterProfileStatusesByCluster(
			&cluster.Namespace,
			&cluster.Name,
		)

		Expect(len(clusterProfileStatuses) == 1).To(BeTrue())
		// the remaining cluster profile must be the one specified by the proper cluster summary
		// as it is the only one that belongs to the cluster with Namespace cluster.Namespace and
		// Name cluster.Name
		Expect(*clusterProfileStatuses[0].Name == properClusterSummary.Labels["projectsveltos.io/cluster-profile-name"]).To(BeTrue())
	})
})
