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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/textlogger"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("Events Data", func() {
	It("Collect details of an EventTrigger", func() {
		eventSource := &libsveltosv1beta1.EventSource{
			ObjectMeta: metav1.ObjectMeta{
				Name: randomString(),
			},
			Spec: libsveltosv1beta1.EventSourceSpec{
				ResourceSelectors: []libsveltosv1beta1.ResourceSelector{
					{
						Group:     randomString(),
						Kind:      randomString(),
						Namespace: randomString(),
					},
				},
			},
		}

		cluster1 := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: randomString(),
				Name:      randomString(),
			},
		}

		clusterType := libsveltosv1beta1.ClusterTypeSveltos
		eventReportName1 := libsveltosv1beta1.GetEventReportName(eventSource.Name,
			cluster1.Name, &clusterType)
		eventReport1 := &libsveltosv1beta1.EventReport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster1.Namespace,
				Name:      eventReportName1,
			},
			Spec: libsveltosv1beta1.EventReportSpec{
				MatchingResources: []corev1.ObjectReference{
					{
						Kind: randomString(), APIVersion: randomString(),
						Namespace: randomString(), Name: randomString(),
					},
					{
						Kind: randomString(), APIVersion: randomString(),
						Namespace: randomString(), Name: randomString(),
					},
				},
			},
		}

		clusterRef1 := corev1.ObjectReference{
			Kind:       libsveltosv1beta1.SveltosClusterKind,
			APIVersion: libsveltosv1beta1.GroupVersion.String(),
			Namespace:  cluster1.Namespace,
			Name:       cluster1.Name,
		}

		cluster2 := &clusterv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: randomString(),
				Name:      randomString(),
			},
		}

		clusterType = libsveltosv1beta1.ClusterTypeCapi
		eventReportName2 := libsveltosv1beta1.GetEventReportName(eventSource.Name,
			cluster2.Name, &clusterType)
		eventReport2 := &libsveltosv1beta1.EventReport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cluster2.Namespace,
				Name:      eventReportName2,
			},
			Spec: libsveltosv1beta1.EventReportSpec{
				MatchingResources: []corev1.ObjectReference{
					{
						Kind: randomString(), APIVersion: randomString(),
						Namespace: randomString(), Name: randomString(),
					},
				},
			},
		}

		clusterRef2 := corev1.ObjectReference{
			Kind:       clusterv1.ClusterKind,
			APIVersion: clusterv1.GroupVersion.String(),
			Namespace:  cluster2.Namespace,
			Name:       cluster2.Name,
		}

		initObjects := []client.Object{
			cluster1, cluster2,
			eventReport1, eventReport2,
			eventSource,
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(initObjects...).
			WithObjects(initObjects...).Build()

		logger := textlogger.NewLogger(textlogger.NewConfig())

		sveltosClusters := map[corev1.ObjectReference]server.ClusterInfo{
			clusterRef1: {
				Paused:         false,
				Version:        "v1.35.0",
				Ready:          true,
				Labels:         map[string]string{randomString(): randomString()},
				FailureMessage: nil,
			},
		}

		capiClusters := map[corev1.ObjectReference]server.ClusterInfo{
			clusterRef2: {
				Paused:         false,
				Version:        "v1.35.0",
				Ready:          true,
				Labels:         map[string]string{randomString(): randomString()},
				FailureMessage: nil,
			},
		}

		result, err := server.GetEventClusterDetails(context.TODO(), c, eventSource.Name, &clusterRef1,
			capiClusters, sveltosClusters, logger)
		Expect(err).To(BeNil())
		Expect(result).ToNot(BeNil())
		Expect(result.ClusterNamespace).To(Equal(clusterRef1.Namespace))
		Expect(result.ClusterName).To(Equal(clusterRef1.Name))
		Expect(result.ClusterKind).To(Equal(clusterRef1.Kind))
		Expect(result.Resources).ToNot(BeNil())
		Expect(len(result.Resources)).To(Equal(len(eventReport1.Spec.MatchingResources)))

		result, err = server.GetEventClusterDetails(context.TODO(), c, eventSource.Name, &clusterRef2,
			capiClusters, sveltosClusters, logger)
		Expect(err).To(BeNil())
		Expect(result).ToNot(BeNil())
		Expect(result.ClusterNamespace).To(Equal(clusterRef2.Namespace))
		Expect(result.ClusterName).To(Equal(clusterRef2.Name))
		Expect(result.ClusterKind).To(Equal(clusterRef2.Kind))
		Expect(result.Resources).ToNot(BeNil())
		Expect(len(result.Resources)).To(Equal(len(eventReport2.Spec.MatchingResources)))
	})
})
