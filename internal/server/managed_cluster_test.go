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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("ManageClusters", func() {
	It("Clusters are sorted by Namespace/Name", func() {
		managedClusters := make(server.ManagedClusters, 0)
		for i := 0; i < 10; i++ {
			cluster := server.ManagedCluster{
				Namespace: randomString(),
				Name:      randomString(),
			}

			managedClusters = append(managedClusters, cluster)
		}

		sort.Sort(managedClusters)

		var previousNamespace string
		for i := range managedClusters {
			if i == 0 {
				previousNamespace = managedClusters[i].Namespace
			} else {
				Expect(previousNamespace < managedClusters[i].Namespace)
				previousNamespace = managedClusters[i].Namespace
			}
		}
	})

	It("getLimitedClusters returns the right set of clusters", func() {
		managedClusters := make(server.ManagedClusters, 0)
		for i := 0; i < 10; i++ {
			cluster := server.ManagedCluster{
				Namespace: randomString(),
				Name:      randomString(),
			}

			managedClusters = append(managedClusters, cluster)
		}

		sort.Sort(managedClusters)

		limit := 1
		skip := 3
		result, err := server.GetLimitedClusters(managedClusters, limit, skip)
		Expect(err).To(BeNil())
		for i := 0; i < limit; i++ {
			Expect(reflect.DeepEqual(result[i], managedClusters[skip+i]))
		}

		limit = 3
		skip = 5
		result, err = server.GetLimitedClusters(managedClusters, limit, skip)
		Expect(err).To(BeNil())
		for i := 0; i < limit; i++ {
			Expect(reflect.DeepEqual(result[i], managedClusters[skip+i]))
		}

		limit = 3
		skip = 9
		result, err = server.GetLimitedClusters(managedClusters, limit, skip)
		Expect(err).To(BeNil())
		// limit is 3 but skip starts from 9. Original number of clusters is 10. So expect only 1 cluster
		Expect(len(result)).To(Equal(1))
		Expect(reflect.DeepEqual(result[0], managedClusters[skip]))

		limit = 3
		skip = 11
		_, err = server.GetLimitedClusters(managedClusters, limit, skip)
		Expect(err).ToNot(BeNil())
	})
})
