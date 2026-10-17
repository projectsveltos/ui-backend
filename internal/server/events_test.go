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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/textlogger"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	eventv1beta1 "github.com/projectsveltos/event-manager/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

const (
	kubernetesVersion = "v1.35.0"
	serviceKind       = "Service"
	firstService      = "svc-1"
	secondService     = "svc-2"
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
				Version:        kubernetesVersion,
				Ready:          true,
				Labels:         map[string]string{randomString(): randomString()},
				FailureMessage: nil,
			},
		}

		capiClusters := map[corev1.ObjectReference]server.ClusterInfo{
			clusterRef2: {
				Paused:         false,
				Version:        kubernetesVersion,
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
		Expect(result.MatchingResourcesCount).To(Equal(len(eventReport1.Spec.MatchingResources)))

		result, err = server.GetEventClusterDetails(context.TODO(), c, eventSource.Name, &clusterRef2,
			capiClusters, sveltosClusters, logger)
		Expect(err).To(BeNil())
		Expect(result).ToNot(BeNil())
		Expect(result.ClusterNamespace).To(Equal(clusterRef2.Namespace))
		Expect(result.ClusterName).To(Equal(clusterRef2.Name))
		Expect(result.ClusterKind).To(Equal(clusterRef2.Kind))
		Expect(result.MatchingResourcesCount).To(Equal(len(eventReport2.Spec.MatchingResources)))
	})

	It("Reports no matching resources for a cluster without an EventReport", func() {
		cluster := &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
		}
		clusterRef := corev1.ObjectReference{
			Kind:       libsveltosv1beta1.SveltosClusterKind,
			APIVersion: libsveltosv1beta1.GroupVersion.String(),
			Namespace:  cluster.Namespace,
			Name:       cluster.Name,
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
		sveltosClusters := map[corev1.ObjectReference]server.ClusterInfo{clusterRef: {Ready: true}}

		result, err := server.GetEventClusterDetails(context.TODO(), c, randomString(), &clusterRef,
			nil, sveltosClusters, textlogger.NewLogger(textlogger.NewConfig()))
		Expect(err).To(BeNil())
		Expect(result).ToNot(BeNil())
		Expect(result.MatchingResourcesCount).To(BeZero())
	})
})

var _ = Describe("Resources matching an EventTrigger", func() {
	var (
		eventTrigger *eventv1beta1.EventTrigger
		cluster      *libsveltosv1beta1.SveltosCluster
		logger       = textlogger.NewLogger(textlogger.NewConfig())
	)

	matchingResource := func(kind, namespace, name string) corev1.ObjectReference {
		return corev1.ObjectReference{Kind: kind, APIVersion: "v1", Namespace: namespace, Name: name}
	}

	newClient := func(matchingResources ...corev1.ObjectReference) client.Client {
		eventSource := &libsveltosv1beta1.EventSource{
			ObjectMeta: metav1.ObjectMeta{Name: eventTrigger.Spec.EventSourceName},
		}

		initObjects := []client.Object{eventTrigger, eventSource}
		if len(matchingResources) > 0 {
			clusterType := libsveltosv1beta1.ClusterTypeSveltos
			initObjects = append(initObjects, &libsveltosv1beta1.EventReport{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: cluster.Namespace,
					Name:      libsveltosv1beta1.GetEventReportName(eventSource.Name, cluster.Name, &clusterType),
				},
				Spec: libsveltosv1beta1.EventReportSpec{MatchingResources: matchingResources},
			})
		}

		return fake.NewClientBuilder().WithScheme(scheme).WithObjects(initObjects...).Build()
	}

	getResources := func(c client.Client, filters *server.ResourceFilters, limit, skip int,
	) (*server.EventResourcesResult, error) {

		return server.GetEventMatchingResources(context.TODO(), c, eventTrigger.Name,
			libsveltosv1beta1.ClusterTypeSveltos, cluster.Namespace, cluster.Name, filters, limit, skip, logger)
	}

	names := func(resources []server.Resource) []string {
		result := make([]string, 0, len(resources))
		for i := range resources {
			result = append(result, resources[i].Name)
		}
		return result
	}

	BeforeEach(func() {
		eventTrigger = &eventv1beta1.EventTrigger{
			ObjectMeta: metav1.ObjectMeta{Name: randomString()},
			Spec:       eventv1beta1.EventTriggerSpec{EventSourceName: randomString()},
		}
		cluster = &libsveltosv1beta1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{Namespace: randomString(), Name: randomString()},
		}
	})

	It("returns the resources ordered by kind, namespace and name", func() {
		c := newClient(
			matchingResource(serviceKind, "b", firstService),
			matchingResource("ConfigMap", "b", "cm-2"),
			matchingResource(serviceKind, "a", secondService),
			matchingResource("ConfigMap", "b", "cm-1"),
		)

		result, err := getResources(c, &server.ResourceFilters{}, 10, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(4))
		Expect(names(result.Resources)).To(Equal([]string{"cm-1", "cm-2", secondService, firstService}))
	})

	It("paginates the resources and keeps the total", func() {
		resources := make([]corev1.ObjectReference, 0, 7)
		for i := 0; i < 7; i++ {
			resources = append(resources, matchingResource(serviceKind, "ns", fmt.Sprintf("svc-%d", i)))
		}
		c := newClient(resources...)

		result, err := getResources(c, &server.ResourceFilters{}, 3, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(7))
		Expect(names(result.Resources)).To(Equal([]string{"svc-0", "svc-1", "svc-2"}))

		result, err = getResources(c, &server.ResourceFilters{}, 3, 6)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(7))
		Expect(names(result.Resources)).To(Equal([]string{"svc-6"}))

		By("skipping past the last resource")
		result, err = getResources(c, &server.ResourceFilters{}, 3, 7)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(7))
		Expect(result.Resources).ToNot(BeNil())
		Expect(result.Resources).To(BeEmpty())

		_, err = getResources(c, &server.ResourceFilters{}, 3, -1)
		Expect(err).ToNot(BeNil())
		_, err = getResources(c, &server.ResourceFilters{}, -1, 0)
		Expect(err).ToNot(BeNil())
	})

	It("filters on a case insensitive part of kind, namespace and name, and counts the filtered resources", func() {
		c := newClient(
			matchingResource(serviceKind, "prod", "frontend"),
			matchingResource(serviceKind, "prod", "backend"),
			matchingResource("ConfigMap", "prod", "frontend-config"),
			matchingResource(serviceKind, "staging", "frontend"),
		)

		result, err := getResources(c, &server.ResourceFilters{Kind: "SERV", Namespace: "pro"}, 10, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(2))
		Expect(names(result.Resources)).To(Equal([]string{"backend", "frontend"}))

		result, err = getResources(c, &server.ResourceFilters{Name: "FRONT"}, 1, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(3))
		Expect(result.Resources).To(HaveLen(1))

		result, err = getResources(c, &server.ResourceFilters{Name: randomString()}, 10, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(BeZero())
		Expect(result.Resources).To(BeEmpty())
	})

	It("returns no resources when the cluster has no EventReport", func() {
		result, err := getResources(newClient(), &server.ResourceFilters{}, 10, 0)
		Expect(err).To(BeNil())
		Expect(result).ToNot(BeNil())
		Expect(result.TotalResources).To(BeZero())
		Expect(result.Resources).ToNot(BeNil())
		Expect(result.Resources).To(BeEmpty())
	})

	It("returns nil when the EventTrigger does not exist", func() {
		c := newClient(matchingResource(serviceKind, "ns", "svc"))
		eventTrigger.Name = randomString()

		result, err := getResources(c, &server.ResourceFilters{}, 10, 0)
		Expect(err).To(BeNil())
		Expect(result).To(BeNil())
	})
})
