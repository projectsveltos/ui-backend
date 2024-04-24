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
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"

	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
)

type ManagedCluster struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	ClusterInfo `json:"clusterInfo"`
}

var (
	ginLogger logr.Logger

	getManagedCAPIClusters = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get managed ClusterAPI Clusters")

		manager := GetManagerInstance()
		clusters := manager.GetManagedCAPIClusters()
		managedClusterData := getManagedClusterData(clusters)

		// Return JSON response
		c.JSON(http.StatusOK, managedClusterData)
	}

	getManagedSveltosClusters = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get managed SveltosClusters")

		manager := GetManagerInstance()
		clusters := manager.GetManagedSveltosClusters()
		managedClusterData := getManagedClusterData(clusters)

		// Return JSON response
		c.JSON(http.StatusOK, managedClusterData)
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

func getManagedClusterData(clusters map[corev1.ObjectReference]ClusterInfo) []ManagedCluster {
	data := make([]ManagedCluster, len(clusters))
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
