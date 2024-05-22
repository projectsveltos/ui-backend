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
	"sort"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

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
		_, err = server.GetHelmReleaseInRange(helmReleases, limit, skip)
		Expect(err).ToNot(BeNil())
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
		_, err = server.GetResourcesInRange(resources, limit, skip)
		Expect(err).ToNot(BeNil())
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
})
