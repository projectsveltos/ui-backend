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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
})