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
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"

	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
)

const (
	maxItems = 6
)

var (
	ginLogger logr.Logger

	getManagedCAPIClusters = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get managed ClusterAPI Clusters")

		limit, skip := getLimitAndSkipFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("limit %d skip %d", limit, skip))

		manager := GetManagerInstance()
		clusters := manager.GetManagedCAPIClusters()
		managedClusterData := getManagedClusterData(clusters)
		sort.Sort(managedClusterData)

		result, err := getLimitedClusters(managedClusterData, limit, skip)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
		}
		// Return JSON response
		c.JSON(http.StatusOK, result)
	}

	getManagedSveltosClusters = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get managed SveltosClusters")

		limit, skip := getLimitAndSkipFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("limit %d skip %d", limit, skip))

		manager := GetManagerInstance()
		clusters := manager.GetManagedSveltosClusters()
		managedClusterData := getManagedClusterData(clusters)
		result, err := getLimitedClusters(managedClusterData, limit, skip)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
		}

		// Return JSON response
		c.JSON(http.StatusOK, result)
	}
)

func (m *instance) start(ctx context.Context, port string, logger logr.Logger) {
	ginLogger = logger

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.GET("/capiclusters", getManagedCAPIClusters)
	r.GET("/sveltosclusters", getManagedSveltosClusters)

	errCh := make(chan error)

	go func() {
		err := r.Run(port)
		errCh <- err // Send the error on the channel
		ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("run failed: %v", err))
		if killErr := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); killErr != nil {
			panic("kill -TERM failed")
		}
	}()

	for {
		select {
		case <-ctx.Done():
			ginLogger.V(logs.LogInfo).Info("context canceled")
			return
		case <-errCh:
			return
		}
	}
}

func getManagedClusterData(clusters map[corev1.ObjectReference]ClusterInfo) ManagedClusters {
	data := make(ManagedClusters, len(clusters))
	i := 0
	for k := range clusters {
		data[i] = ManagedCluster{
			Namespace:   k.Namespace,
			Name:        k.Name,
			ClusterInfo: clusters[k],
		}
		i++
	}

	return data
}

func getLimitAndSkipFromQuery(c *gin.Context) (limit, skip int) {
	// Define default values for limit and skip
	limit = maxItems
	skip = 0

	// Get the values from query parameters
	queryLimit := c.Query("limit")
	querySkip := c.Query("skip")

	// Parse the query parameters to int (handle errors)
	var err error
	if queryLimit != "" {
		limit, err = strconv.Atoi(queryLimit)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit parameter"})
			return
		}
	}
	if querySkip != "" {
		skip, err = strconv.Atoi(querySkip)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid skip parameter"})
			return
		}
	}

	return
}
