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
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/labels"
)

type ManagedCluster struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	ClusterInfo `json:"clusterInfo"`
}

type ManagedClusters []ManagedCluster

type ClusterResult struct {
	TotalClusters   int             `json:"totalClusters"`
	ManagedClusters ManagedClusters `json:"managedClusters"`
}

func (s ManagedClusters) Len() int      { return len(s) }
func (s ManagedClusters) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s ManagedClusters) Less(i, j int) bool {
	if s[i].Namespace == s[j].Namespace {
		return s[i].Name < s[j].Name
	}
	return s[i].Namespace < s[j].Namespace
}

func getClustersInRange(clusters ManagedClusters, limit, skip int) (ManagedClusters, error) {
	if len(clusters) == 0 {
		return clusters, nil
	}

	if skip < 0 {
		return nil, errors.New("skip cannot be negative")
	}
	if limit < 0 {
		return nil, errors.New("limit cannot be negative")
	}
	if skip >= len(clusters) {
		return nil, errors.New("skip cannot be greater than or equal to the length of the slice")
	}

	// Adjust limit based on slice length and skip
	adjustedLimit := limit
	if skip+limit > len(clusters) {
		adjustedLimit = len(clusters) - skip
	}

	// Use slicing to extract the desired sub-slice
	return clusters[skip : skip+adjustedLimit], nil
}

type clusterFilters struct {
	Namespace     string          `uri:"namespace"`
	name          string          `uri:"name"`
	labelSelector labels.Selector `uri:"labels"`
}

func getClusterFiltersFromQuery(c *gin.Context) (*clusterFilters, error) {
	var filters clusterFilters
	// Get the values from query parameters
	filters.Namespace = c.Query("namespace")
	filters.name = c.Query("name")
	filters.labelSelector = labels.NewSelector()

	lbls := c.Query("labels")

	if lbls != "" {
		// format is labels=key1:value1_key2:value2
		lbls = strings.ReplaceAll(lbls, ":", "=")
		lbls = strings.ReplaceAll(lbls, "_", ",")
		parsedSelector, err := labels.Parse(lbls)
		if err != nil {
			return nil, err
		}
		filters.labelSelector = parsedSelector
	}

	return &filters, nil
}
