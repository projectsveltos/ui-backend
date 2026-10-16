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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/textlogger"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	"github.com/projectsveltos/addon-controller/lib/healthmonitoring"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/controller"
	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("Health monitoring controllers", func() {
	var (
		clusterNamespace string
		clusterName      string
		summary          *configv1beta1.ClusterSummary
		healthCheck      *libsveltosv1beta1.HealthCheck
		report           *libsveltosv1beta1.HealthCheckReport
		logger           logr.Logger
	)

	BeforeEach(func() {
		clusterNamespace = randomString()
		clusterName = randomString()

		summary = &configv1beta1.ClusterSummary{
			ObjectMeta: metav1.ObjectMeta{
				Name:      randomString(),
				Namespace: clusterNamespace,
				Labels: map[string]string{
					configv1beta1.ClusterNameLabel: clusterName,
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
				ClusterNamespace: clusterNamespace,
				ClusterName:      clusterName,
				ClusterType:      libsveltosv1beta1.ClusterTypeCapi,
				ClusterProfileSpec: configv1beta1.Spec{
					HealthMonitoring: &configv1beta1.HealthMonitoring{Enabled: true},
				},
			},
		}

		healthCheck = &libsveltosv1beta1.HealthCheck{
			ObjectMeta: metav1.ObjectMeta{
				Name:   randomString(),
				Labels: map[string]string{configv1beta1.HealthMonitoringClusterSummaryUIDLabel: randomString()},
				Annotations: map[string]string{
					configv1beta1.HealthMonitoringClusterSummaryNamespaceAnnotation: summary.Namespace,
					configv1beta1.HealthMonitoringClusterSummaryNameAnnotation:      summary.Name,
				},
			},
		}

		report = &libsveltosv1beta1.HealthCheckReport{
			ObjectMeta: metav1.ObjectMeta{Name: randomString(), Namespace: clusterNamespace},
			Spec: libsveltosv1beta1.HealthCheckReportSpec{
				ClusterNamespace: clusterNamespace,
				ClusterName:      clusterName,
				ClusterType:      libsveltosv1beta1.ClusterTypeCapi,
				HealthCheckName:  healthCheck.Name,
				ResourceStatuses: []libsveltosv1beta1.ResourceStatus{
					{
						ObjectRef:    corev1.ObjectReference{APIVersion: "apps/v1", Kind: "Deployment", Name: "web"},
						HealthStatus: libsveltosv1beta1.HealthStatusDegraded,
					},
				},
			},
		}

		logger = textlogger.NewLogger(textlogger.NewConfig())
	})

	clusterState := func() healthmonitoring.State {
		return server.GetManagerInstance().ClusterHealthStatus(
			libsveltosv1beta1.ClusterTypeCapi, clusterNamespace, clusterName)
	}

	It("HealthCheckReconciler attributes a report to the ClusterSummary a HealthCheck was generated for", func() {
		initObjects := []client.Object{healthCheck}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initObjects...).Build()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		server.InitializeManagerInstance(ctx, nil, c, scheme, httpPort, logger)
		manager := server.GetManagerInstance()
		manager.AddClusterProfileStatus(summary)
		manager.AddHealthCheckReport(report)

		// no HealthCheck known yet: the report belongs to no profile
		health := manager.GetClusterHealth(libsveltosv1beta1.ClusterTypeCapi, clusterNamespace, clusterName)
		Expect(health.Resources).To(HaveLen(1))
		Expect(health.Resources[0].Profiles).To(BeEmpty())

		reconciler := &controller.HealthCheckReconciler{Client: c, Scheme: scheme}
		_, err := reconciler.Reconcile(context.TODO(), ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(healthCheck)})
		Expect(err).To(BeNil())

		health = manager.GetClusterHealth(libsveltosv1beta1.ClusterTypeCapi, clusterNamespace, clusterName)
		Expect(health.Resources).To(HaveLen(1))
		Expect(health.Resources[0].Profiles).To(HaveLen(1))
		Expect(clusterState()).To(Equal(healthmonitoring.StateDegraded))

		By("deleting the HealthCheck")
		Expect(c.Delete(context.TODO(), healthCheck)).To(Succeed())
		_, err = reconciler.Reconcile(context.TODO(), ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(healthCheck)})
		Expect(err).To(BeNil())

		health = manager.GetClusterHealth(libsveltosv1beta1.ClusterTypeCapi, clusterNamespace, clusterName)
		Expect(health.Resources[0].Profiles).To(BeEmpty())
	})

	It("HealthCheckReportReconciler adds and removes a report", func() {
		initObjects := []client.Object{report}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(initObjects...).Build()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		server.InitializeManagerInstance(ctx, nil, c, scheme, httpPort, logger)
		manager := server.GetManagerInstance()
		manager.AddClusterProfileStatus(summary)
		manager.AddHealthCheck(healthCheck)
		Expect(clusterState()).To(Equal(healthmonitoring.StateNoData))

		reconciler := &controller.HealthCheckReportReconciler{Client: c, Scheme: scheme}
		_, err := reconciler.Reconcile(context.TODO(), ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(report)})
		Expect(err).To(BeNil())
		Expect(clusterState()).To(Equal(healthmonitoring.StateDegraded))

		Expect(c.Delete(context.TODO(), report)).To(Succeed())
		_, err = reconciler.Reconcile(context.TODO(), ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(report)})
		Expect(err).To(BeNil())
		Expect(clusterState()).To(Equal(healthmonitoring.StateNoData))
	})
})

var _ = Describe("GeneratedHealthCheckPredicate", func() {
	var generated, other *libsveltosv1beta1.HealthCheck

	BeforeEach(func() {
		generated = &libsveltosv1beta1.HealthCheck{
			ObjectMeta: metav1.ObjectMeta{
				Name:   randomString(),
				Labels: map[string]string{configv1beta1.HealthMonitoringClusterSummaryUIDLabel: randomString()},
			},
		}
		other = &libsveltosv1beta1.HealthCheck{ObjectMeta: metav1.ObjectMeta{Name: randomString()}}
	})

	It("lets through only the generated HealthChecks", func() {
		p := controller.GeneratedHealthCheckPredicate{}

		Expect(p.Create(event.CreateEvent{Object: generated})).To(BeTrue())
		Expect(p.Create(event.CreateEvent{Object: other})).To(BeFalse())
		Expect(p.Delete(event.DeleteEvent{Object: generated})).To(BeTrue())
		Expect(p.Delete(event.DeleteEvent{Object: other})).To(BeFalse())
		Expect(p.Generic(event.GenericEvent{Object: generated})).To(BeTrue())
		Expect(p.Generic(event.GenericEvent{Object: other})).To(BeFalse())
	})

	It("lets an update through when the old or the new object is a generated HealthCheck", func() {
		p := controller.GeneratedHealthCheckPredicate{}

		Expect(p.Update(event.UpdateEvent{ObjectOld: generated, ObjectNew: generated})).To(BeTrue())
		Expect(p.Update(event.UpdateEvent{ObjectOld: generated, ObjectNew: other})).To(BeTrue())
		Expect(p.Update(event.UpdateEvent{ObjectOld: other, ObjectNew: generated})).To(BeTrue())
		Expect(p.Update(event.UpdateEvent{ObjectOld: other, ObjectNew: other})).To(BeFalse())
	})
})
