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

package server

import (
	"fmt"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

func examineClusterConditions(cluster *clusterv1.Cluster) *string {
	if cluster == nil {
		return nil
	}

	message := ""

	for i := range cluster.Status.Conditions {
		c := cluster.Status.Conditions[i]
		if c.Status != metav1.ConditionTrue && c.Message != "" {
			message = fmt.Sprintf("%s\n%s", message, c.Message)
		}
	}

	if message != "" {
		return &message
	}

	return nil
}

type helmFilters struct {
	ReleaseNamespace string `uri:"namespace"`
	ReleaseName      string `uri:"name"`
}

func getHelmFiltersFromQuery(c *gin.Context) *helmFilters {
	var filters helmFilters
	// Get the values from query parameters
	filters.ReleaseNamespace = c.Query("release_namespace")
	filters.ReleaseName = c.Query("release_name")

	return &filters
}

type resourceFilters struct {
	Namespace string `uri:"namespace"`
	Name      string `uri:"name"`
	Kind      string `uri:"kind"`
}

func getResourceFiltersFromQuery(c *gin.Context) *resourceFilters {
	var filters resourceFilters
	// Get the values from query parameters
	filters.Namespace = c.Query("resource_namespace")
	filters.Name = c.Query("resource_name")
	filters.Kind = c.Query("resource_kind")

	return &filters
}
