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
	"reflect"
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("Deployed Addons and Applications", func() {
	It("getHelmReleaseInRange returns the right set of deployed Helm Releases", func() {
		helmReleases := make([]server.HelmRelease, 0)
		for i := 0; i < 10; i++ {
			helmRelease := server.HelmRelease{
				Namespace:    randomString(),
				ReleaseName:  randomString(),
				RepoURL:      randomString(),
				Icon:         randomString(),
				ChartVersion: randomString(),
				ProfileName:  randomString(),
			}

			helmReleases = append(helmReleases, helmRelease)
		}

		limit := 1
		skip := 3
		result, err := server.GetHelmReleaseInRange(helmReleases, limit, skip)
		Expect(err).To(BeNil())
		for i := 0; i < limit; i++ {
			Expect(reflect.DeepEqual(result[i], helmReleases[skip+i]))
		}

		limit = 3
		skip = 5
		result, err = server.GetHelmReleaseInRange(helmReleases, limit, skip)
		Expect(err).To(BeNil())
		for i := 0; i < limit; i++ {
			Expect(reflect.DeepEqual(result[i], helmReleases[skip+i]))
		}

		limit = 3
		skip = 9
		result, err = server.GetHelmReleaseInRange(helmReleases, limit, skip)
		Expect(err).To(BeNil())
		// limit is 3 but skip starts from 9. Original number of clusters is 10. So expect only 1 cluster
		Expect(len(result)).To(Equal(1))
		Expect(reflect.DeepEqual(result[0], helmReleases[skip]))

		limit = 3
		skip = 11
		result, err = server.GetHelmReleaseInRange(helmReleases, limit, skip)
		Expect(err).To(BeNil())
		Expect(len(result)).To(BeZero())
	})

	It("getResourcesInRange returns the right set of deployed Kubernetes resources", func() {
		resources := make([]server.Resource, 0)
		for i := 0; i < 10; i++ {
			resource := server.Resource{
				Namespace:    randomString(),
				Name:         randomString(),
				Group:        randomString(),
				Version:      randomString(),
				Kind:         randomString(),
				ProfileNames: []string{randomString()},
			}

			resources = append(resources, resource)
		}

		limit := 1
		skip := 3
		result, err := server.GetResourcesInRange(resources, limit, skip)
		Expect(err).To(BeNil())
		for i := 0; i < limit; i++ {
			Expect(reflect.DeepEqual(result[i], resources[skip+i]))
		}

		limit = 3
		skip = 5
		result, err = server.GetResourcesInRange(resources, limit, skip)
		Expect(err).To(BeNil())
		for i := 0; i < limit; i++ {
			Expect(reflect.DeepEqual(result[i], resources[skip+i]))
		}

		limit = 3
		skip = 9
		result, err = server.GetResourcesInRange(resources, limit, skip)
		Expect(err).To(BeNil())
		// limit is 3 but skip starts from 9. Original number of clusters is 10. So expect only 1 cluster
		Expect(len(result)).To(Equal(1))
		Expect(reflect.DeepEqual(result[0], resources[skip]))

		limit = 3
		skip = 11
		result, err = server.GetResourcesInRange(resources, limit, skip)
		Expect(err).To(BeNil())
		Expect(len(result)).To(BeZero())
	})

	It("sortResources sorts resources by applied time first and GVK then", func() {
		time1 := metav1.Time{Time: time.Now()}
		resources := make([]server.Resource, 0)
		for i := 0; i < 10; i++ {
			resource := server.Resource{
				Namespace:       randomString(),
				Name:            randomString(),
				Group:           randomString(),
				Version:         randomString(),
				Kind:            randomString(),
				ProfileNames:    []string{randomString()},
				LastAppliedTime: &time1,
			}

			resources = append(resources, resource)
		}

		time2 := metav1.Time{Time: time.Now().Add(-time.Minute)}
		resource := server.Resource{
			Namespace:       randomString(),
			Name:            randomString(),
			Group:           randomString(),
			Version:         randomString(),
			Kind:            randomString(),
			ProfileNames:    []string{randomString()},
			LastAppliedTime: &time2,
		}

		resources = append(resources, resource)

		sort.Slice(resources, func(i, j int) bool {
			return server.SortResources(resources, i, j)
		})

		Expect(resources[0]).To(Equal(resource))

		// Remove first element
		resources = resources[1:]
		for i := 0; i < len(resources)-2; i++ {
			gvk1 := schema.GroupVersionKind{
				Group:   resources[i].Group,
				Kind:    resources[i].Kind,
				Version: resources[i].Version,
			}

			gvk2 := schema.GroupVersionKind{
				Group:   resources[i+1].Group,
				Kind:    resources[i+1].Kind,
				Version: resources[i+1].Version,
			}

			Expect(gvk1.String() < gvk2.String()).To(BeTrue())
		}
	})

	It("sortHelmCharts sorts Helm Charts by applied time first and namespace/release name then", func() {
		time1 := metav1.Time{Time: time.Now()}
		helmReleases := make([]server.HelmRelease, 0)
		for i := 0; i < 5; i++ {
			helmRelease := server.HelmRelease{
				Namespace:       randomString(),
				ReleaseName:     randomString(),
				RepoURL:         randomString(),
				Icon:            randomString(),
				ChartVersion:    randomString(),
				ProfileName:     randomString(),
				LastAppliedTime: &time1,
			}

			helmReleases = append(helmReleases, helmRelease)
		}

		time2 := metav1.Time{Time: time.Now().Add(-time.Minute)}
		helmRelease := server.HelmRelease{
			Namespace:       randomString(),
			ReleaseName:     randomString(),
			RepoURL:         randomString(),
			Icon:            randomString(),
			ChartVersion:    randomString(),
			ProfileName:     randomString(),
			LastAppliedTime: &time2,
		}

		helmReleases = append(helmReleases, helmRelease)

		sort.Slice(helmReleases, func(i, j int) bool {
			return server.SortHelmCharts(helmReleases, i, j)
		})

		Expect(helmReleases[0]).To(Equal(helmRelease))

		// Remove first element
		helmReleases = helmReleases[1:]
		for i := 0; i < len(helmReleases)-2; i++ {
			if helmReleases[i].Namespace == helmReleases[i+1].Namespace {
				Expect(helmReleases[i].ReleaseName < helmReleases[i+1].ReleaseName)
			}

			Expect(helmReleases[i].Namespace < helmReleases[i+1].Namespace).To(BeTrue())
		}
	})

	It("getHelmChartsForCluster joins outdated-version info from ClusterSummary", func() {
		namespace := randomString()
		clusterName := randomString()
		clusterType := libsveltosv1beta1.ClusterTypeCapi
		profileName := randomString()
		releaseName := randomString()
		releaseNamespace := randomString()
		chartVersion := testChartVersionBase
		latest := testLatestVersion
		latestPatch := "1.0.1"
		lastChecked := metav1.Now()

		clusterConfig := &configv1beta1.ClusterConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      randomString(),
				Namespace: namespace,
				Labels: map[string]string{
					configv1beta1.ClusterNameLabel: clusterName,
					configv1beta1.ClusterTypeLabel: string(clusterType),
				},
			},
			Status: configv1beta1.ClusterConfigurationStatus{
				ClusterProfileResources: []configv1beta1.ClusterProfileResource{
					{
						ClusterProfileName: profileName,
						Features: []configv1beta1.Feature{
							{
								FeatureID: libsveltosv1beta1.FeatureHelm,
								Charts: []configv1beta1.Chart{
									{
										ReleaseName:  releaseName,
										Namespace:    releaseNamespace,
										ChartVersion: chartVersion,
									},
								},
							},
						},
					},
				},
			},
		}

		clusterSummary := &configv1beta1.ClusterSummary{
			ObjectMeta: metav1.ObjectMeta{
				Name:      randomString(),
				Namespace: namespace,
				Labels: map[string]string{
					configv1beta1.ClusterNameLabel: clusterName,
					configv1beta1.ClusterTypeLabel: string(clusterType),
				},
			},
			Status: configv1beta1.ClusterSummaryStatus{
				HelmReleaseSummaries: []configv1beta1.HelmChartSummary{
					{
						ReleaseName: releaseName, ReleaseNamespace: releaseNamespace,
						Status:             configv1beta1.HelmChartStatusManaging,
						LatestVersion:      &latest,
						LatestPatchVersion: &latestPatch,
						LastCheckedTime:    &lastChecked,
					},
				},
			},
		}

		initObjects := []client.Object{clusterConfig, clusterSummary}
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(initObjects...).WithObjects(initObjects...).Build()

		logger := textlogger.NewLogger(textlogger.NewConfig())
		m := server.NewTestInstance(c, logger)

		releases, err := m.GetHelmChartsForCluster(context.TODO(), namespace, clusterName, clusterType)
		Expect(err).To(BeNil())
		Expect(releases).To(HaveLen(1))
		Expect(releases[0].LatestVersion).To(Equal(latest))
		Expect(releases[0].LatestPatchVersion).To(Equal(latestPatch))
		Expect(releases[0].LastCheckedTime).ToNot(BeNil())
	})

	It("getHelmChartsForCluster leaves outdated-version fields empty when release is up to date", func() {
		namespace := randomString()
		clusterName := randomString()
		clusterType := libsveltosv1beta1.ClusterTypeCapi
		profileName := randomString()
		releaseName := randomString()
		releaseNamespace := randomString()

		clusterConfig := &configv1beta1.ClusterConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name:      randomString(),
				Namespace: namespace,
				Labels: map[string]string{
					configv1beta1.ClusterNameLabel: clusterName,
					configv1beta1.ClusterTypeLabel: string(clusterType),
				},
			},
			Status: configv1beta1.ClusterConfigurationStatus{
				ClusterProfileResources: []configv1beta1.ClusterProfileResource{
					{
						ClusterProfileName: profileName,
						Features: []configv1beta1.Feature{
							{
								FeatureID: libsveltosv1beta1.FeatureHelm,
								Charts: []configv1beta1.Chart{
									{ReleaseName: releaseName, Namespace: releaseNamespace, ChartVersion: testChartVersionBase},
								},
							},
						},
					},
				},
			},
		}

		// No ClusterSummary at all for this cluster: outdated info lookup should be a no-op,
		// not an error, and the release should still be returned.
		initObjects := []client.Object{clusterConfig}
		c := fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(initObjects...).WithObjects(initObjects...).Build()

		logger := textlogger.NewLogger(textlogger.NewConfig())
		m := server.NewTestInstance(c, logger)

		releases, err := m.GetHelmChartsForCluster(context.TODO(), namespace, clusterName, clusterType)
		Expect(err).To(BeNil())
		Expect(releases).To(HaveLen(1))
		Expect(releases[0].LatestVersion).To(BeEmpty())
		Expect(releases[0].LatestPatchVersion).To(BeEmpty())
		Expect(releases[0].LastCheckedTime).To(BeNil())
	})
})
