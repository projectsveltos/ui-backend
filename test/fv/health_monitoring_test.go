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

package fv_test

import (
	"context"
	"fmt"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	"github.com/projectsveltos/addon-controller/lib/clusterops"
	"github.com/projectsveltos/addon-controller/lib/healthmonitoring"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// The HealthCheckReports are created by this spec: nothing in the fv environment evaluates the
// HealthChecks (no healthcheck-manager, no sveltos-agent). The spec stands in for healthcheck-manager and
// writes reports the way it does, so what is verified here is addon-controller generating the HealthCheck
// for a real ClusterProfile and ui-backend turning ClusterSummaries, HealthChecks and reports into its
// responses. The whole chain, with a real agent, is covered by the e2e repository.
//
// Health monitoring state is per cluster and the fv cluster is shared by all specs, which run in
// parallel: everything is one spec, and no other spec enables health monitoring.
var _ = Describe("Health monitoring", func() {
	const (
		unclaimedHealthCheckName = "fv-user-health-check"
		profileNameParam         = "profile_name"
	)

	It("reports the health of the resources deployed by a ClusterProfile", Label("FV"), func() {
		Byf("Creating a cluster-admin-bound ServiceAccount to query ui-backend with")
		token, cleanupAdmin := createAdminToken()
		defer cleanupAdmin()

		Byf("Creating a ClusterProfile deploying a Deployment, with health monitoring enabled")
		deploymentName := randomString()
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: metav1.NamespaceDefault, Name: randomString()},
			Data:       map[string]string{"deployment.yaml": deploymentYAML(deploymentName)},
		}
		Expect(k8sClient.Create(context.TODO(), configMap)).To(Succeed())

		clusterProfile := getClusterProfile("health-", map[string]string{key: value})
		clusterProfile.Spec.SyncMode = configv1beta1.SyncModeContinuous
		clusterProfile.Spec.HealthMonitoring = &configv1beta1.HealthMonitoring{Enabled: true}
		clusterProfile.Spec.PolicyRefs = []configv1beta1.PolicyRef{
			{Kind: string(libsveltosv1beta1.ConfigMapReferencedResourceKind), Namespace: configMap.Namespace, Name: configMap.Name},
		}
		Expect(k8sClient.Create(context.TODO(), clusterProfile)).To(Succeed())

		Byf("Waiting for the ClusterSummary of ClusterProfile %s to deploy the resources", clusterProfile.Name)
		var clusterSummary *configv1beta1.ClusterSummary
		Eventually(func() bool {
			clusterSummary = getClusterSummary(clusterProfile.Name)
			return clusterSummary != nil && resourcesFeatureIsProvisioned(clusterSummary)
		}, timeout, pollingInterval).Should(BeTrue())

		Byf("Verifying addon-controller generated the HealthCheck and the ClusterHealthCheck for the ClusterSummary")
		var healthCheck *libsveltosv1beta1.HealthCheck
		Eventually(func() bool {
			healthCheck = getGeneratedHealthCheck(clusterSummary)
			return healthCheck != nil && getGeneratedClusterHealthCheck(clusterSummary) != nil
		}, timeout, pollingInterval).Should(BeTrue())

		Byf("Port-forwarding to ui-backend-manager")
		localPort, stopChan := portForwardToPod(uiBackendPodLabels)
		defer close(stopChan)

		clusterType := string(libsveltosv1beta1.ClusterTypeCapi)

		Byf("Verifying that, with no report yet, the cluster and the profile have no data")
		expectClusterHealthState(localPort, token, healthmonitoring.StateNoData)
		Eventually(func() healthmonitoring.State {
			return profileHealthStatus(localPort, token, clusterProfile.Name)
		}, timeout, pollingInterval).Should(Equal(healthmonitoring.StateNoData))
		health, err := getClusterHealth(localPort, token, "")
		Expect(err).To(BeNil())
		Expect(health.State).To(Equal(healthmonitoring.StateNoData))
		Expect(health.Resources).To(BeEmpty())

		Byf("Creating a report with a Degraded and a Progressing resource")
		report := newHealthCheckReport(healthCheck.Name,
			newResourceStatus(appsv1.SchemeGroupVersion.String(), "Deployment", deploymentName,
				libsveltosv1beta1.HealthStatusDegraded, "0/1 replicas available"),
			newResourceStatus(appsv1.SchemeGroupVersion.String(), "StatefulSet", randomString(),
				libsveltosv1beta1.HealthStatusProgressing, "rolling update"))
		Expect(k8sClient.Create(context.TODO(), report)).To(Succeed())
		defer deleteIgnoringNotFound(report)

		Byf("Verifying the cluster, the profile and /clusterHealth report the resources, worst first")
		expectClusterHealthState(localPort, token, healthmonitoring.StateDegraded)
		Eventually(func() healthmonitoring.State {
			return profileHealthStatus(localPort, token, clusterProfile.Name)
		}, timeout, pollingInterval).Should(Equal(healthmonitoring.StateDegraded))

		health, err = getClusterHealth(localPort, token, "")
		Expect(err).To(BeNil())
		Expect(health.State).To(Equal(healthmonitoring.StateDegraded))
		Expect(health.TotalResources).To(Equal(2))
		Expect(health.Resources[0].Kind).To(Equal("Deployment"))
		Expect(health.Resources[0].Name).To(Equal(deploymentName))
		Expect(health.Resources[0].Group).To(Equal("apps"))
		Expect(health.Resources[0].Version).To(Equal("v1"))
		Expect(health.Resources[0].Status).To(Equal(libsveltosv1beta1.HealthStatusDegraded))
		Expect(health.Resources[0].Message).To(Equal("0/1 replicas available"))
		Expect(health.Resources[0].HealthCheckName).To(BeEmpty())
		Expect(health.Resources[0].Profiles).To(HaveLen(1))
		Expect(health.Resources[0].Profiles[0].Kind).To(Equal(configv1beta1.ClusterProfileKind))
		Expect(health.Resources[0].Profiles[0].Name).To(Equal(clusterProfile.Name))
		Expect(health.Resources[1].Kind).To(Equal("StatefulSet"))

		Byf("Verifying the filters and the pagination of /clusterHealth")
		query := url.Values{"profile_kind": {configv1beta1.ClusterProfileKind}, profileNameParam: {clusterProfile.Name}}
		health, err = getClusterHealth(localPort, token, query.Encode())
		Expect(err).To(BeNil())
		Expect(health.TotalResources).To(Equal(2))

		health, err = getClusterHealth(localPort, token, url.Values{profileNameParam: {randomString()}}.Encode())
		Expect(err).To(BeNil())
		Expect(health.TotalResources).To(BeZero())
		Expect(health.State).To(Equal(healthmonitoring.StateDegraded))

		health, err = getClusterHealth(localPort, token, url.Values{"resource_kind": {"statefulset"}}.Encode())
		Expect(err).To(BeNil())
		Expect(health.TotalResources).To(Equal(1))
		Expect(health.Resources[0].Kind).To(Equal("StatefulSet"))

		health, err = getClusterHealth(localPort, token, url.Values{"limit": {"1"}, "skip": {"1"}}.Encode())
		Expect(err).To(BeNil())
		Expect(health.TotalResources).To(Equal(2))
		Expect(health.Resources).To(HaveLen(1))
		Expect(health.Resources[0].Kind).To(Equal("StatefulSet"))

		Byf("Updating the report: nothing unhealthy anymore")
		Eventually(func() error {
			current := &libsveltosv1beta1.HealthCheckReport{}
			if err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(report), current); err != nil {
				return err
			}
			current.Spec.ResourceStatuses = nil
			return k8sClient.Update(context.TODO(), current)
		}, timeout, pollingInterval).Should(Succeed())

		expectClusterHealthState(localPort, token, healthmonitoring.StateHealthy)
		Eventually(func() healthmonitoring.State {
			return profileHealthStatus(localPort, token, clusterProfile.Name)
		}, timeout, pollingInterval).Should(Equal(healthmonitoring.StateHealthy))

		Byf("Creating a report of a HealthCheck no profile owns")
		unclaimed := newHealthCheckReport(unclaimedHealthCheckName,
			newResourceStatus(appsv1.SchemeGroupVersion.String(), "DaemonSet", randomString(),
				libsveltosv1beta1.HealthStatusSuspended, "paused"))
		Expect(k8sClient.Create(context.TODO(), unclaimed)).To(Succeed())
		defer deleteIgnoringNotFound(unclaimed)

		Byf("Verifying the resource is listed without profiles and counts for the cluster")
		expectClusterHealthState(localPort, token, healthmonitoring.StateSuspended)
		health, err = getClusterHealth(localPort, token, "")
		Expect(err).To(BeNil())
		Expect(health.Resources).To(HaveLen(1))
		Expect(health.Resources[0].Kind).To(Equal("DaemonSet"))
		Expect(health.Resources[0].Profiles).To(BeEmpty())
		Expect(health.Resources[0].HealthCheckName).To(Equal(unclaimedHealthCheckName))
		// the profile itself is still healthy
		Expect(profileHealthStatus(localPort, token, clusterProfile.Name)).To(Equal(healthmonitoring.StateHealthy))

		Byf("Deleting the reports: the profile has no data again")
		Expect(k8sClient.Delete(context.TODO(), report)).To(Succeed())
		Expect(k8sClient.Delete(context.TODO(), unclaimed)).To(Succeed())
		expectClusterHealthState(localPort, token, healthmonitoring.StateNoData)

		Byf("Deleting the ClusterProfile")
		deleteClusterProfile(clusterProfile)
		Expect(k8sClient.Delete(context.TODO(), configMap)).To(Succeed())

		Byf("Verifying addon-controller removed the generated HealthCheck")
		Eventually(func() bool {
			return getGeneratedHealthCheck(clusterSummary) == nil
		}, timeout, pollingInterval).Should(BeTrue())

		Byf("Verifying ui-backend no longer reports a health status for the cluster %s/%s",
			kindWorkloadCluster.Namespace, clusterType)
		expectClusterHealthState(localPort, token, "")
		health, err = getClusterHealth(localPort, token, "")
		Expect(err).To(BeNil())
		Expect(health.State).To(Equal(healthmonitoring.StateNotEnabled))
		Expect(health.Resources).To(BeEmpty())
	})
})

func deploymentYAML(name string) string {
	return fmt.Sprintf(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: default
spec:
  replicas: 0
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: pause
        image: registry.k8s.io/pause:3.9
`, name, name, name)
}

// createAdminToken creates a cluster-admin-bound ServiceAccount and returns a token for it, and a
// function removing what was created.
func createAdminToken() (token string, cleanup func()) {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: randomString()}}
	Expect(k8sClient.Create(context.TODO(), namespace)).To(Succeed())

	serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: namespace.Name, Name: randomString()}}
	Expect(k8sClient.Create(context.TODO(), serviceAccount)).To(Succeed())

	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: randomString()},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     clusterRoleKind,
			Name:     clusterAdminClusterRoleName,
		},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: namespace.Name, Name: serviceAccount.Name},
		},
	}
	Expect(k8sClient.Create(context.TODO(), binding)).To(Succeed())

	return getServiceAccountToken(namespace.Name, serviceAccount.Name), func() {
		deleteIgnoringNotFound(binding)
		deleteIgnoringNotFound(namespace)
	}
}

func deleteIgnoringNotFound(obj client.Object) {
	err := k8sClient.Delete(context.TODO(), obj)
	if err != nil && !apierrors.IsNotFound(err) {
		Expect(err).To(BeNil())
	}
}

// getClusterSummary returns the ClusterSummary of a ClusterProfile for the kind workload cluster, or nil.
func getClusterSummary(clusterProfileName string) *configv1beta1.ClusterSummary {
	clusterSummaryList := &configv1beta1.ClusterSummaryList{}
	listOptions := []client.ListOption{
		client.InNamespace(kindWorkloadCluster.Namespace),
		client.MatchingLabels{
			clusterops.ClusterProfileLabelName: clusterProfileName,
			configv1beta1.ClusterNameLabel:     kindWorkloadCluster.Name,
		},
	}
	if err := k8sClient.List(context.TODO(), clusterSummaryList, listOptions...); err != nil ||
		len(clusterSummaryList.Items) != 1 {

		return nil
	}
	return &clusterSummaryList.Items[0]
}

func resourcesFeatureIsProvisioned(clusterSummary *configv1beta1.ClusterSummary) bool {
	for i := range clusterSummary.Status.FeatureSummaries {
		fs := &clusterSummary.Status.FeatureSummaries[i]
		if fs.FeatureID == libsveltosv1beta1.FeatureResources && fs.Status == libsveltosv1beta1.FeatureStatusProvisioned {
			return true
		}
	}
	return false
}

func generatedCheckSelector(clusterSummary *configv1beta1.ClusterSummary) client.MatchingLabels {
	return client.MatchingLabels{configv1beta1.HealthMonitoringClusterSummaryUIDLabel: string(clusterSummary.UID)}
}

// getGeneratedHealthCheck returns the HealthCheck addon-controller generated for a ClusterSummary, or nil.
func getGeneratedHealthCheck(clusterSummary *configv1beta1.ClusterSummary) *libsveltosv1beta1.HealthCheck {
	healthCheckList := &libsveltosv1beta1.HealthCheckList{}
	if err := k8sClient.List(context.TODO(), healthCheckList, generatedCheckSelector(clusterSummary)); err != nil ||
		len(healthCheckList.Items) != 1 {

		return nil
	}
	return &healthCheckList.Items[0]
}

// getGeneratedClusterHealthCheck returns the ClusterHealthCheck addon-controller generated for a ClusterSummary, or nil.
func getGeneratedClusterHealthCheck(clusterSummary *configv1beta1.ClusterSummary) *libsveltosv1beta1.ClusterHealthCheck {
	clusterHealthCheckList := &libsveltosv1beta1.ClusterHealthCheckList{}
	if err := k8sClient.List(context.TODO(), clusterHealthCheckList, generatedCheckSelector(clusterSummary)); err != nil ||
		len(clusterHealthCheckList.Items) != 1 {

		return nil
	}
	return &clusterHealthCheckList.Items[0]
}

func newResourceStatus(apiVersion, kind, name string, status libsveltosv1beta1.HealthStatus, message string,
) libsveltosv1beta1.ResourceStatus {

	return libsveltosv1beta1.ResourceStatus{
		ObjectRef:    corev1.ObjectReference{APIVersion: apiVersion, Kind: kind, Namespace: metav1.NamespaceDefault, Name: name},
		HealthStatus: status,
		Message:      message,
	}
}

// newHealthCheckReport builds a report for the kind workload cluster the way healthcheck-manager does.
func newHealthCheckReport(healthCheckName string, resources ...libsveltosv1beta1.ResourceStatus,
) *libsveltosv1beta1.HealthCheckReport {

	clusterType := libsveltosv1beta1.ClusterTypeCapi
	return &libsveltosv1beta1.HealthCheckReport{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: kindWorkloadCluster.Namespace,
			Name:      libsveltosv1beta1.GetHealthCheckReportName(healthCheckName, kindWorkloadCluster.Name, &clusterType),
			Labels:    libsveltosv1beta1.GetHealthCheckReportLabels(healthCheckName, kindWorkloadCluster.Name, &clusterType),
		},
		Spec: libsveltosv1beta1.HealthCheckReportSpec{
			ClusterNamespace: kindWorkloadCluster.Namespace,
			ClusterName:      kindWorkloadCluster.Name,
			ClusterType:      clusterType,
			HealthCheckName:  healthCheckName,
			ResourceStatuses: resources,
		},
	}
}

// expectClusterHealthState waits for the health status of the kind workload cluster in /capiclusters.
func expectClusterHealthState(localPort int, token string, expected healthmonitoring.State) {
	Eventually(func() healthmonitoring.State {
		path := "/capiclusters?" + workloadClusterQuery().Encode()
		result := &server.ClusterResult{}
		if err := getJSON(localPort, token, path, result); err != nil {
			return healthmonitoring.State("error: " + err.Error())
		}
		for i := range result.ManagedClusters {
			if result.ManagedClusters[i].Name == kindWorkloadCluster.Name {
				return result.ManagedClusters[i].HealthStatus
			}
		}
		return healthmonitoring.State("cluster not found")
	}, timeout, pollingInterval).Should(Equal(expected))
}

// profileHealthStatus returns the health status /getClusterStatus gives for a profile, or "".
func profileHealthStatus(localPort int, token, profileName string) healthmonitoring.State {
	resp, err := getClusterStatus(localPort, token, kindWorkloadCluster.Namespace, kindWorkloadCluster.Name,
		string(libsveltosv1beta1.ClusterTypeCapi))
	if err != nil {
		return ""
	}
	for i := range resp.Profiles {
		if resp.Profiles[i].ProfileName == profileName {
			return resp.Profiles[i].HealthStatus
		}
	}
	return ""
}

// getClusterHealth calls /clusterHealth for the kind workload cluster. query holds extra, already encoded, parameters.
func getClusterHealth(localPort int, token, query string) (*server.ClusterHealthResult, error) {
	clusterQuery := workloadClusterQuery()
	clusterQuery.Set("type", string(libsveltosv1beta1.ClusterTypeCapi))
	path := "/clusterHealth?" + clusterQuery.Encode()
	if query != "" {
		path += "&" + query
	}

	result := &server.ClusterHealthResult{}
	err := getJSON(localPort, token, path, result)
	return result, err
}

// workloadClusterQuery returns the namespace and name query parameters selecting the kind workload cluster.
func workloadClusterQuery() url.Values {
	return url.Values{
		"namespace": {kindWorkloadCluster.Namespace},
		"name":      {kindWorkloadCluster.Name},
	}
}
