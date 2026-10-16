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
	"fmt"
	"math/rand"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	"github.com/projectsveltos/addon-controller/lib/healthmonitoring"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

const (
	capi           = libsveltosv1beta1.ClusterTypeCapi
	deploymentKind = "Deployment"

	// the actions a test can apply to the manager, see healthFixture.apply
	addSummary = "summary"
	addCheck   = "check"
	addReport  = "report"
)

// healthFixture is a cluster with one profile with health monitoring enabled, the HealthCheck
// generated for it and the report of that HealthCheck.
type healthFixture struct {
	clusterNamespace string
	clusterName      string
	profileName      string
	summary          *configv1beta1.ClusterSummary
	healthCheck      *libsveltosv1beta1.HealthCheck
	report           *libsveltosv1beta1.HealthCheckReport
}

func newHealthFixture(resources ...libsveltosv1beta1.ResourceStatus) *healthFixture {
	return newHealthFixtureOnCluster(randomString(), randomString(), resources...)
}

// newHealthFixtureOnCluster is like newHealthFixture for a given cluster: fixtures built for
// the same cluster are different profiles on it.
func newHealthFixtureOnCluster(clusterNamespace, clusterName string,
	resources ...libsveltosv1beta1.ResourceStatus) *healthFixture {

	f := &healthFixture{
		clusterNamespace: clusterNamespace,
		clusterName:      clusterName,
		profileName:      randomString(),
	}

	f.summary = createTestClusterSummary(randomString(), f.clusterNamespace, f.clusterNamespace, f.clusterName, nil)
	f.summary.OwnerReferences[0].Name = f.profileName
	f.summary.Spec.ClusterProfileSpec.HealthMonitoring = &configv1beta1.HealthMonitoring{Enabled: true}

	f.healthCheck = &libsveltosv1beta1.HealthCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:   randomString(),
			Labels: map[string]string{configv1beta1.HealthMonitoringClusterSummaryUIDLabel: randomString()},
			Annotations: map[string]string{
				configv1beta1.HealthMonitoringClusterSummaryNamespaceAnnotation: f.summary.Namespace,
				configv1beta1.HealthMonitoringClusterSummaryNameAnnotation:      f.summary.Name,
			},
		},
	}

	f.report = &libsveltosv1beta1.HealthCheckReport{
		ObjectMeta: metav1.ObjectMeta{Name: randomString(), Namespace: f.clusterNamespace},
		Spec: libsveltosv1beta1.HealthCheckReportSpec{
			ClusterNamespace: f.clusterNamespace,
			ClusterName:      f.clusterName,
			ClusterType:      capi,
			HealthCheckName:  f.healthCheck.Name,
			ResourceStatuses: resources,
		},
	}

	return f
}

func (f *healthFixture) apply(manager interface {
	AddClusterProfileStatus(*configv1beta1.ClusterSummary)
	AddHealthCheck(metav1.Object)
	AddHealthCheckReport(*libsveltosv1beta1.HealthCheckReport)
}, action string) {

	switch action {
	case addSummary:
		manager.AddClusterProfileStatus(f.summary)
	case addCheck:
		manager.AddHealthCheck(f.healthCheck)
	case addReport:
		manager.AddHealthCheckReport(f.report)
	}
}

// runForEachFixture runs action for every fixture, each in its own goroutine, and waits for all of them.
func runForEachFixture(fixtures []*healthFixture, action func(f *healthFixture)) {
	var wg sync.WaitGroup
	for i := range fixtures {
		wg.Add(1)
		go func(f *healthFixture) {
			defer GinkgoRecover()
			defer wg.Done()
			action(f)
		}(fixtures[i])
	}
	wg.Wait()
}

func resourceStatus(kind, name string, status libsveltosv1beta1.HealthStatus, message string,
) libsveltosv1beta1.ResourceStatus {

	return libsveltosv1beta1.ResourceStatus{
		ObjectRef:    corev1.ObjectReference{APIVersion: "apps/v1", Kind: kind, Namespace: "d", Name: name},
		HealthStatus: status,
		Message:      message,
	}
}

var _ = Describe("Health monitoring", func() {
	var manager interface {
		AddClusterProfileStatus(*configv1beta1.ClusterSummary)
		RemoveClusterProfileStatus(string, string)
		AddHealthCheck(metav1.Object)
		RemoveHealthCheck(string)
		AddHealthCheckReport(*libsveltosv1beta1.HealthCheckReport)
		RemoveHealthCheckReport(string, string)
		ClusterHealthStatus(libsveltosv1beta1.ClusterType, string, string) healthmonitoring.State
		GetClusterHealth(libsveltosv1beta1.ClusterType, string, string) healthmonitoring.ClusterHealth
		GetClusterHealthResult(libsveltosv1beta1.ClusterType, string, string, string, string, string, int, int,
		) (*server.ClusterHealthResult, error)
		SetHealthStatusOnProfileStatuses(libsveltosv1beta1.ClusterType, string, string, []server.ProfileStatusResult)
	}

	BeforeEach(func() {
		logger := textlogger.NewLogger(textlogger.NewConfig())
		s, err := setupScheme()
		Expect(err).To(BeNil())
		c := fake.NewClientBuilder().WithScheme(s).Build()

		ctx, cancel := context.WithCancel(context.Background())
		DeferCleanup(cancel)
		server.InitializeManagerInstance(ctx, nil, c, s, randomPort(), logger)
		manager = server.GetManagerInstance()
	})

	It("reports no health status when no profile has health monitoring enabled", func() {
		f := newHealthFixture()
		f.summary.Spec.ClusterProfileSpec.HealthMonitoring = nil
		manager.AddClusterProfileStatus(f.summary)

		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).To(BeEmpty())
		Expect(manager.ClusterHealthStatus(capi, randomString(), randomString())).To(BeEmpty())
	})

	It("forgets a ClusterSummary when health monitoring is switched off", func() {
		f := newHealthFixture(resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusDegraded, ""))
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		f.apply(manager, addReport)
		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).
			To(Equal(healthmonitoring.StateDegraded))

		f.summary.Spec.ClusterProfileSpec.HealthMonitoring.Enabled = false
		f.apply(manager, addSummary)

		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).To(BeEmpty())
		Expect(manager.GetClusterHealth(capi, f.clusterNamespace, f.clusterName).Profiles).To(BeEmpty())

		By("switching it on again")
		f.summary.Spec.ClusterProfileSpec.HealthMonitoring.Enabled = true
		f.apply(manager, addSummary)
		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).
			To(Equal(healthmonitoring.StateDegraded))
	})

	It("reports NoData when monitoring is enabled and no report exists", func() {
		f := newHealthFixture()
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)

		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).
			To(Equal(healthmonitoring.StateNoData))
	})

	It("reports the worst status of the resources of the generated HealthCheck, whatever the order of events", func() {
		orders := [][]string{
			{addSummary, addCheck, addReport}, {addSummary, addReport, addCheck},
			{addCheck, addSummary, addReport}, {addCheck, addReport, addSummary},
			{addReport, addSummary, addCheck}, {addReport, addCheck, addSummary},
		}

		for _, order := range orders {
			f := newHealthFixture(
				resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusProgressing, "rolling"),
				resourceStatus("StatefulSet", "db", libsveltosv1beta1.HealthStatusDegraded, "no replicas"))
			for _, action := range order {
				f.apply(manager, action)
			}

			By(fmt.Sprintf("order %v", order))
			Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).
				To(Equal(healthmonitoring.StateDegraded))

			result, err := manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName, "", "", "", 10, 0)
			Expect(err).To(BeNil())
			Expect(result.State).To(Equal(healthmonitoring.StateDegraded))
			Expect(result.TotalResources).To(Equal(2))
			Expect(result.Resources[0].Kind).To(Equal("StatefulSet"))
			Expect(result.Resources[0].Group).To(Equal("apps"))
			Expect(result.Resources[0].Version).To(Equal("v1"))
			Expect(result.Resources[0].Profiles).To(HaveLen(1))
			Expect(result.Resources[0].Profiles[0].Name).To(Equal(f.profileName))
			Expect(result.Resources[0].HealthCheckName).To(BeEmpty())
		}
	})

	It("moves to NoData when the report is removed and to Healthy when the report has no resources", func() {
		f := newHealthFixture(resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusDegraded, ""))
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		f.apply(manager, addReport)

		manager.RemoveHealthCheckReport(f.report.Namespace, f.report.Name)
		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).
			To(Equal(healthmonitoring.StateNoData))

		f.report.Spec.ResourceStatuses = nil
		f.apply(manager, addReport)
		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).
			To(Equal(healthmonitoring.StateHealthy))
	})

	It("lists the resources as unclaimed when the generated HealthCheck is removed", func() {
		f := newHealthFixture(resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusDegraded, "bad"))
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		f.apply(manager, addReport)

		manager.RemoveHealthCheck(f.healthCheck.Name)

		result, err := manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName, "", "", "", 10, 0)
		Expect(err).To(BeNil())
		Expect(result.Resources).To(HaveLen(1))
		Expect(result.Resources[0].Profiles).To(BeEmpty())
		Expect(result.Resources[0].HealthCheckName).To(Equal(f.healthCheck.Name))
		// the unclaimed resource still counts for the cluster
		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).
			To(Equal(healthmonitoring.StateDegraded))
	})

	It("forgets a HealthCheck that is not a generated one anymore", func() {
		f := newHealthFixture(resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusDegraded, ""))
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		f.apply(manager, addReport)

		f.healthCheck.Labels = nil
		f.apply(manager, addCheck)

		result, err := manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName, "", "", "", 10, 0)
		Expect(err).To(BeNil())
		Expect(result.Resources[0].Profiles).To(BeEmpty())
	})

	It("clears the health status when the ClusterSummary is removed", func() {
		f := newHealthFixture(resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusDegraded, ""))
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		f.apply(manager, addReport)

		manager.RemoveClusterProfileStatus(f.summary.Namespace, f.summary.Name)
		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).To(BeEmpty())
	})

	It("recomputes both clusters when a report moves to another cluster", func() {
		f := newHealthFixture(resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusDegraded, ""))
		other := newHealthFixture()
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		other.apply(manager, addSummary)
		other.apply(manager, addCheck)
		f.apply(manager, addReport)

		f.report.Spec.ClusterNamespace = other.clusterNamespace
		f.report.Spec.ClusterName = other.clusterName
		f.report.Spec.HealthCheckName = other.healthCheck.Name
		f.apply(manager, addReport)

		Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).
			To(Equal(healthmonitoring.StateNoData))
		Expect(manager.ClusterHealthStatus(capi, other.clusterNamespace, other.clusterName)).
			To(Equal(healthmonitoring.StateDegraded))
	})

	It("filters and paginates the resources of a cluster", func() {
		f := newHealthFixture(
			resourceStatus(deploymentKind, "a", libsveltosv1beta1.HealthStatusDegraded, ""),
			resourceStatus(deploymentKind, "b", libsveltosv1beta1.HealthStatusProgressing, ""),
			resourceStatus("StatefulSet", "c", libsveltosv1beta1.HealthStatusDegraded, ""))
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		f.apply(manager, addReport)

		result, err := manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName, "", "", "", 2, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(3))
		Expect(result.Resources).To(HaveLen(2))

		result, err = manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName, "", "", "", 2, 2)
		Expect(err).To(BeNil())
		Expect(result.Resources).To(HaveLen(1))
		Expect(result.Resources[0].Name).To(Equal("b"))

		result, err = manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName, "", "", "stateful", 10, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(1))
		Expect(result.Resources[0].Name).To(Equal("c"))

		result, err = manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName,
			configv1beta1.ClusterProfileKind, f.profileName, "", 10, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(Equal(3))

		result, err = manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName,
			configv1beta1.ClusterProfileKind, randomString(), "", 10, 0)
		Expect(err).To(BeNil())
		Expect(result.TotalResources).To(BeZero())
		Expect(result.Resources).ToNot(BeNil())

		_, err = manager.GetClusterHealthResult(capi, f.clusterNamespace, f.clusterName, "", "", "", 10, -1)
		Expect(err).ToNot(BeNil())
	})

	It("returns the state NotEnabled and no resource for a cluster nothing is known about", func() {
		result, err := manager.GetClusterHealthResult(capi, randomString(), randomString(), "", "", "", 10, 0)
		Expect(err).To(BeNil())
		Expect(result.State).To(Equal(healthmonitoring.StateNotEnabled))
		Expect(result.Resources).To(BeEmpty())
	})

	It("sets the health status on the rows of the profiles with health monitoring enabled", func() {
		f := newHealthFixture(resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusDegraded, ""))
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		f.apply(manager, addReport)

		rows := []server.ProfileStatusResult{
			{ProfileName: f.profileName, ProfileType: configv1beta1.ClusterProfileKind},
			{ProfileName: randomString(), ProfileType: configv1beta1.ClusterProfileKind},
			{ProfileName: f.profileName, ProfileType: configv1beta1.ProfileKind},
		}
		manager.SetHealthStatusOnProfileStatuses(capi, f.clusterNamespace, f.clusterName, rows)

		Expect(rows[0].HealthStatus).To(Equal(healthmonitoring.StateDegraded))
		Expect(rows[1].HealthStatus).To(BeEmpty())
		Expect(rows[2].HealthStatus).To(BeEmpty())
	})

	It("sets the health status on the cluster list", func() {
		f := newHealthFixture(resourceStatus(deploymentKind, "web", libsveltosv1beta1.HealthStatusSuspended, ""))
		f.apply(manager, addSummary)
		f.apply(manager, addCheck)
		f.apply(manager, addReport)

		withoutMonitoring := newHealthFixture()
		withoutMonitoring.summary.Spec.ClusterProfileSpec.HealthMonitoring = nil
		withoutMonitoring.apply(manager, addSummary)

		clusters := map[corev1.ObjectReference]server.ClusterInfo{
			{Namespace: f.clusterNamespace, Name: f.clusterName}:                                 {},
			{Namespace: withoutMonitoring.clusterNamespace, Name: withoutMonitoring.clusterName}: {},
		}
		data := server.GetManagedClusterDataNoFilters(clusters, server.GetManagerInstance(), capi)
		Expect(data).To(HaveLen(2))
		for i := range data {
			if data[i].Name == f.clusterName {
				Expect(data[i].HealthStatus).To(Equal(healthmonitoring.StateSuspended))
			} else {
				Expect(data[i].HealthStatus).To(BeEmpty())
			}
		}
	})

	It("keeps 100 clusters with 10 profiles each apart", func() {
		const clusters, profiles = 100, 10

		fixtures := make([]*healthFixture, 0, clusters)
		for i := 0; i < clusters; i++ {
			clusterNamespace, clusterName := randomString(), randomString()
			for j := 0; j < profiles; j++ {
				status := libsveltosv1beta1.HealthStatusHealthy
				if i%2 == 0 && j == 0 {
					status = libsveltosv1beta1.HealthStatusDegraded
				}
				f := newHealthFixtureOnCluster(clusterNamespace, clusterName,
					resourceStatus(deploymentKind, "web", status, ""))
				for _, action := range []string{addSummary, addCheck, addReport} {
					f.apply(manager, action)
				}
				if j == 0 {
					fixtures = append(fixtures, f)
				}
			}
		}

		for i, f := range fixtures {
			expected := healthmonitoring.StateHealthy
			if i%2 == 0 {
				expected = healthmonitoring.StateDegraded
			}
			Expect(manager.ClusterHealthStatus(capi, f.clusterNamespace, f.clusterName)).To(Equal(expected))
			Expect(manager.GetClusterHealth(capi, f.clusterNamespace, f.clusterName).Profiles).To(HaveLen(profiles))
		}
	})

	It("stays consistent when profiles of the same cluster are reported concurrently, with readers running", func() {
		// Meaningful as a race check only with -race. Without it, it still verifies that the result does not
		// depend on how the events of the profiles interleave.
		const profiles = 20

		clusterNamespace, clusterName := randomString(), randomString()
		fixtures := make([]*healthFixture, 0, profiles)
		for i := 0; i < profiles; i++ {
			status := libsveltosv1beta1.HealthStatusHealthy
			if i%2 == 0 {
				status = libsveltosv1beta1.HealthStatusDegraded
			}
			fixtures = append(fixtures, newHealthFixtureOnCluster(clusterNamespace, clusterName,
				resourceStatus(deploymentKind, fmt.Sprintf("web-%d", i), status, "")))
		}

		stopReaders := make(chan struct{})
		var readers sync.WaitGroup
		for i := 0; i < 4; i++ {
			readers.Add(1)
			go func() {
				defer GinkgoRecover()
				defer readers.Done()
				for {
					select {
					case <-stopReaders:
						return
					default:
						_ = manager.ClusterHealthStatus(capi, clusterNamespace, clusterName)
						_ = manager.GetClusterHealth(capi, clusterNamespace, clusterName)
						_, err := manager.GetClusterHealthResult(capi, clusterNamespace, clusterName, "", "", "", 100, 0)
						Expect(err).To(BeNil())
					}
				}
			}()
		}

		By("adding the summary, the HealthCheck and the report of every profile at the same time, in a random order")
		runForEachFixture(fixtures, func(f *healthFixture) {
			actions := []string{addSummary, addCheck, addReport}
			//nolint: gosec // this is just a test
			rand.Shuffle(len(actions), func(i, j int) { actions[i], actions[j] = actions[j], actions[i] })
			for _, action := range actions {
				f.apply(manager, action)
			}
		})

		close(stopReaders)
		readers.Wait()

		Expect(manager.ClusterHealthStatus(capi, clusterNamespace, clusterName)).To(Equal(healthmonitoring.StateDegraded))

		health := manager.GetClusterHealth(capi, clusterNamespace, clusterName)
		Expect(health.Profiles).To(HaveLen(profiles))
		Expect(health.Resources).To(HaveLen(profiles / 2))
		for i := range health.Resources {
			// each unhealthy resource is attributed to the one profile whose report lists it
			Expect(health.Resources[i].Profiles).To(HaveLen(1))
			Expect(health.Resources[i].HealthCheckName).To(BeEmpty())
		}
		// only the profiles at an even index have a degraded resource
		expectedState := make(map[string]healthmonitoring.State, profiles)
		for i := range fixtures {
			expectedState[fixtures[i].profileName] = healthmonitoring.StateHealthy
			if i%2 == 0 {
				expectedState[fixtures[i].profileName] = healthmonitoring.StateDegraded
			}
		}
		for i := range health.Profiles {
			Expect(health.Profiles[i].State).To(Equal(expectedState[health.Profiles[i].Profile.Name]))
		}

		By("removing everything at the same time")
		runForEachFixture(fixtures, func(f *healthFixture) {
			manager.RemoveHealthCheckReport(f.report.Namespace, f.report.Name)
			manager.RemoveHealthCheck(f.healthCheck.Name)
			manager.RemoveClusterProfileStatus(f.summary.Namespace, f.summary.Name)
		})

		Expect(manager.ClusterHealthStatus(capi, clusterNamespace, clusterName)).To(BeEmpty())
		health = manager.GetClusterHealth(capi, clusterNamespace, clusterName)
		Expect(health.State).To(Equal(healthmonitoring.StateNotEnabled))
		Expect(health.Resources).To(BeEmpty())
	})
})

var _ = Describe("TransformHealthCheckReport", func() {
	It("drops the collected resources of a report", func() {
		report := &libsveltosv1beta1.HealthCheckReport{
			ObjectMeta: metav1.ObjectMeta{
				Name:          randomString(),
				ManagedFields: []metav1.ManagedFieldsEntry{{Manager: randomString()}},
			},
			Spec: libsveltosv1beta1.HealthCheckReportSpec{
				ResourceStatuses: []libsveltosv1beta1.ResourceStatus{
					{
						ObjectRef:    corev1.ObjectReference{Kind: deploymentKind, Name: "web"},
						HealthStatus: libsveltosv1beta1.HealthStatusDegraded,
						Resource:     []byte("a large object"),
					},
				},
			},
		}

		transformed, err := server.TransformHealthCheckReport()(report)
		Expect(err).To(BeNil())

		result, ok := transformed.(*libsveltosv1beta1.HealthCheckReport)
		Expect(ok).To(BeTrue())
		Expect(result.ManagedFields).To(BeEmpty())
		Expect(result.Spec.ResourceStatuses).To(HaveLen(1))
		Expect(result.Spec.ResourceStatuses[0].Resource).To(BeNil())
		Expect(result.Spec.ResourceStatuses[0].ObjectRef.Name).To(Equal("web"))
		Expect(result.Spec.ResourceStatuses[0].HealthStatus).To(Equal(libsveltosv1beta1.HealthStatusDegraded))
	})
})
