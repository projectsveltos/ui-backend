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
	"reflect"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2/textlogger"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	libsveltosv1alpha1 "github.com/projectsveltos/libsveltos/api/v1alpha1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

const (
	k8sVersion = "v1.29.0"
	httpPort   = ":8080"
)

var _ = Describe("Manager", func() {
	var sveltosCluster *libsveltosv1alpha1.SveltosCluster
	var cluster *clusterv1.Cluster
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

		cluster = &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: randomString(),
				Name:      randomString(),
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
	})

	It("AddSveltosCluster add SveltosCluster to list of managed clusters", func() {
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

		server.InitializeManagerInstance(c, scheme, httpPort, logger)
		manager := server.GetManagerInstance()
		manager.AddSveltosCluster(sveltosCluster)

		clusters := manager.GetManagedSveltosClusters()
		v, ok := clusters[*clusterRef]
		Expect(ok).To(BeTrue())
		Expect(reflect.DeepEqual(v, clusterInfo)).To(BeTrue())
	})

	It("RemoveSveltosCluster remove SveltosCluster from list of managed clusters", func() {
		clusterRef := &corev1.ObjectReference{
			Namespace:  sveltosCluster.Namespace,
			Name:       sveltosCluster.Name,
			Kind:       libsveltosv1alpha1.SveltosClusterKind,
			APIVersion: libsveltosv1alpha1.GroupVersion.String(),
		}

		server.InitializeManagerInstance(c, scheme, httpPort, logger)
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

	It("AddCAPICluster add ClusterAPI powered Cluster to list of managed clusters", func() {
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

		server.InitializeManagerInstance(c, scheme, httpPort, logger)
		manager := server.GetManagerInstance()
		manager.AddCAPICluster(cluster)

		clusters := manager.GetManagedCAPIClusters()
		v, ok := clusters[*clusterRef]
		Expect(ok).To(BeTrue())
		Expect(reflect.DeepEqual(v, clusterInfo)).To(BeTrue())
	})

	It("RemoveCAPICluster remove ClusterAPI powered Cluster from list of managed clusters", func() {
		clusterRef := &corev1.ObjectReference{
			Namespace:  cluster.Namespace,
			Name:       cluster.Name,
			Kind:       clusterv1.ClusterKind,
			APIVersion: clusterv1.GroupVersion.String(),
		}

		server.InitializeManagerInstance(c, scheme, httpPort, logger)
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
})
