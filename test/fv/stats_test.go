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
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	"github.com/projectsveltos/addon-controller/lib/clusterops"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// This suite runs with -nodes 6 (see the fv Makefile target), i.e. many specs mutate
// ClusterProfiles/ClusterSummaries on the shared kind cluster concurrently. Assertions here
// deliberately avoid exact counts/deltas on /stats, which would be racy against whatever other
// specs are doing at the same moment - they only ever check for the presence of this spec's own
// named resources, which is safe regardless of what else is running.
var _ = Describe("Sveltos stats and cluster status", func() {
	const (
		chartRepo           = "https://charts.bitnami.com/bitnami"
		chartRepoName       = "bitnami"
		chartName           = "bitnami/wildfly"
		chartVersion        = "20.2.8"
		helmConvergeTimeout = 4 * time.Minute
	)

	It("reflects a newly created ClusterProfile and its deployment status", Label("FV"), func() {
		Byf("Creating a cluster-admin-bound ServiceAccount to query ui-backend with")
		adminNamespace := randomString()
		Expect(k8sClient.Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: adminNamespace},
		})).To(Succeed())

		adminSAName := randomString()
		Expect(k8sClient.Create(context.TODO(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Namespace: adminNamespace, Name: adminSAName},
		})).To(Succeed())

		adminBindingName := randomString()
		adminBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: adminBindingName},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     clusterRoleKind,
				Name:     clusterAdminClusterRoleName,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      rbacv1.ServiceAccountKind,
					Namespace: adminNamespace,
					Name:      adminSAName,
				},
			},
		}
		Expect(k8sClient.Create(context.TODO(), adminBinding)).To(Succeed())

		token := getServiceAccountToken(adminNamespace, adminSAName)

		releaseName := randomString()

		clusterProfile := getClusterProfile("stats-", map[string]string{key: value})
		clusterProfile.Spec.SyncMode = configv1beta1.SyncModeContinuous
		clusterProfile.Spec.HelmCharts = []configv1beta1.HelmChart{
			{
				RepositoryURL:    chartRepo,
				RepositoryName:   chartRepoName,
				ChartName:        chartName,
				ChartVersion:     chartVersion,
				ReleaseName:      releaseName,
				ReleaseNamespace: releaseName,
				HelmChartAction:  configv1beta1.HelmChartActionInstall,
			},
		}
		Byf("Creating ClusterProfile %s", clusterProfile.Name)
		Expect(k8sClient.Create(context.TODO(), clusterProfile)).To(Succeed())

		Byf("Port-forwarding to ui-backend-manager")
		localPort, stopChan := portForwardToPod(uiBackendPodLabels)

		Byf("Verifying /stats reports the CAPI workload cluster and does not error " +
			"(regression check: /stats used to 500 when Cluster API's CRDs were absent)")
		Eventually(func() bool {
			stats, err := getStats(localPort, token)
			// The fv environment always has exactly one CAPI cluster (see fv_suite_test.go
			// BeforeSuite); this is a real invariant, not something another concurrent spec
			// could change, so it's safe to assert exactly rather than just ">= 1".
			return err == nil && stats.CAPIClusters == 1
		}, timeout, pollingInterval).Should(BeTrue())

		Byf("Verifying ClusterProfile %s shows up via /profiles", clusterProfile.Name)
		Eventually(func() bool {
			profiles, err := getProfiles(localPort, token)
			if err != nil {
				return false
			}
			return profileNamed(profiles, clusterProfile.Name) != nil
		}, timeout, pollingInterval).Should(BeTrue())

		Byf("Waiting for the Helm feature to reach Provisioned on the ClusterSummary "+
			"(up to %s: a real Helm install, not just object creation)", helmConvergeTimeout)
		Eventually(func() bool {
			return helmFeatureIsProvisioned(clusterProfile.Name)
		}, helmConvergeTimeout, pollingInterval).Should(BeTrue())

		Byf("Verifying /getClusterStatus reports the Helm feature as Provisioned for %s/%s",
			kindWorkloadCluster.Namespace, kindWorkloadCluster.Name)
		Eventually(func() bool {
			resp, err := getClusterStatus(localPort, token, kindWorkloadCluster.Namespace,
				kindWorkloadCluster.Name, string(libsveltosv1beta1.ClusterTypeCapi))
			if err != nil {
				return false
			}
			for i := range resp.Profiles {
				p := &resp.Profiles[i]
				if p.ProfileName == clusterProfile.Name &&
					p.FeatureID == libsveltosv1beta1.FeatureHelm &&
					p.Status == libsveltosv1beta1.FeatureStatusProvisioned {

					return true
				}
			}
			return false
		}, timeout, pollingInterval).Should(BeTrue())

		close(stopChan)

		Byf("Cleaning up")
		deleteClusterProfile(clusterProfile)
		Expect(k8sClient.Delete(context.TODO(), adminBinding)).To(Succeed())
		Expect(k8sClient.Delete(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: adminNamespace},
		})).To(Succeed())
	})
})

// helmFeatureIsProvisioned returns true once at least one ClusterSummary owned by
// clusterProfileName reports its Helm feature as Provisioned.
func helmFeatureIsProvisioned(clusterProfileName string) bool {
	clusterSummaryList := &configv1beta1.ClusterSummaryList{}
	listOptions := []client.ListOption{
		client.MatchingLabels{clusterops.ClusterProfileLabelName: clusterProfileName},
	}
	if err := k8sClient.List(context.TODO(), clusterSummaryList, listOptions...); err != nil {
		return false
	}

	for i := range clusterSummaryList.Items {
		summaries := clusterSummaryList.Items[i].Status.FeatureSummaries
		for j := range summaries {
			if summaries[j].FeatureID == libsveltosv1beta1.FeatureHelm &&
				summaries[j].Status == libsveltosv1beta1.FeatureStatusProvisioned {

				return true
			}
		}
	}
	return false
}

type getClusterStatusResponse struct {
	TotalResources int                          `json:"totalResources"`
	Profiles       []server.ProfileStatusResult `json:"profiles"`
}

// profileNamed returns the Profile named name across all tiers of a /profiles response, or nil.
func profileNamed(profiles map[int32]server.ProfileResult, name string) *server.Profile {
	for tier := range profiles {
		for i := range profiles[tier].Profiles {
			if profiles[tier].Profiles[i].Name == name {
				return &profiles[tier].Profiles[i]
			}
		}
	}
	return nil
}

func getStats(localPort int, token string) (*server.Stats, error) {
	result := &server.Stats{}
	err := getJSON(localPort, token, "/stats", result)
	return result, err
}

func getProfiles(localPort int, token string) (map[int32]server.ProfileResult, error) {
	result := map[int32]server.ProfileResult{}
	err := getJSON(localPort, token, "/profiles", &result)
	return result, err
}

func getClusterStatus(localPort int, token, namespace, name, clusterType string) (*getClusterStatusResponse, error) {
	path := fmt.Sprintf("/getClusterStatus?namespace=%s&name=%s&type=%s", namespace, name, clusterType)
	result := &getClusterStatusResponse{}
	err := getJSON(localPort, token, path, result)
	return result, err
}

// getJSON calls ui-backend's HTTP API, through the local port opened by portForwardToPod, at
// path, authenticating with token, and decodes the JSON response into out.
func getJSON(localPort int, token, path string, out interface{}) error {
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d%s", localPort, path), http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d for %s", resp.StatusCode, path)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
