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
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
	libsveltosset "github.com/projectsveltos/libsveltos/lib/set"
)

const (
	maxItems = 6
)

type Token struct {
	Value string `json:"token,omitempty"`
}

var (
	ginLogger logr.Logger

	getManagedCAPIClusters = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get managed ClusterAPI Clusters")

		limit, skip := getLimitAndSkipFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("limit %d skip %d", limit, skip))
		filters, err := getClusterFiltersFromQuery(c)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("filters: namespace %q name %q labels %q",
			filters.Namespace, filters.Name, filters.labelSelector))

		user, err := validateToken(c)
		if err != nil {
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		manager := GetManagerInstance()

		canListAll, err := manager.canListCAPIClusters(user)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		clusters, err := manager.GetManagedCAPIClusters(c.Request.Context(), canListAll, user)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		managedClusterData := getManagedClusterData(clusters, filters)
		sort.Sort(managedClusterData)

		result, err := getClustersInRange(managedClusterData, limit, skip)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		response := ClusterResult{
			TotalClusters:   len(managedClusterData),
			ManagedClusters: result,
		}

		// Return JSON response
		c.JSON(http.StatusOK, response)
	}

	getManagedSveltosClusters = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get managed SveltosClusters")

		limit, skip := getLimitAndSkipFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("limit %d skip %d", limit, skip))
		filters, err := getClusterFiltersFromQuery(c)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("filters: namespace %q name %q labels %q",
			filters.Namespace, filters.Name, filters.labelSelector))

		user, err := validateToken(c)
		if err != nil {
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		manager := GetManagerInstance()

		canListAll, err := manager.canListSveltosClusters(user)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		clusters, err := manager.GetManagedSveltosClusters(c.Request.Context(), canListAll, user)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		managedClusterData := getManagedClusterData(clusters, filters)
		sort.Sort(managedClusterData)

		result, err := getClustersInRange(managedClusterData, limit, skip)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		response := ClusterResult{
			TotalClusters:   len(managedClusterData),
			ManagedClusters: result,
		}

		// Return JSON response
		c.JSON(http.StatusOK, response)
	}

	getDeployedHelmCharts = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get deployed HelmCharts")

		namespace, name, clusterType := getClusterFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("cluster %s:%s/%s", clusterType, namespace, name))

		limit, skip := getLimitAndSkipFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("limit %d skip %d", limit, skip))

		user, err := validateToken(c)
		if err != nil {
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		manager := GetManagerInstance()

		canGetCluster, err := manager.canGetCluster(namespace, name, user, clusterType)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		if !canGetCluster {
			_ = c.AbortWithError(http.StatusUnauthorized, errors.New("no permissions to access this cluster"))
			return
		}

		helmCharts, err := manager.getHelmChartsForCluster(c.Request.Context(),
			namespace, name, clusterType)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}
		sort.Slice(helmCharts, func(i, j int) bool {
			return sortHelmCharts(helmCharts, i, j)
		})

		result, err := getHelmReleaseInRange(helmCharts, limit, skip)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		response := HelmReleaseResult{
			TotalHelmReleases: len(helmCharts),
			HelmReleases:      result,
		}

		// Return JSON response
		c.JSON(http.StatusOK, response)
	}

	getDeployedResources = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get deployed Kubernetes resources")

		limit, skip := getLimitAndSkipFromQuery(c)
		namespace, name, clusterType := getClusterFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("cluster %s:%s/%s", clusterType, namespace, name))
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("limit %d skip %d", limit, skip))

		user, err := validateToken(c)
		if err != nil {
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		manager := GetManagerInstance()

		canGetCluster, err := manager.canGetCluster(namespace, name, user, clusterType)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		if !canGetCluster {
			_ = c.AbortWithError(http.StatusUnauthorized, errors.New("no permissions to access this cluster"))
			return
		}

		resources, err := manager.getResourcesForCluster(c.Request.Context(),
			namespace, name, clusterType)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}
		sort.Slice(resources, func(i, j int) bool {
			return sortResources(resources, i, j)
		})

		result, err := getResourcesInRange(resources, limit, skip)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		response := ResourceResult{
			TotalResources: len(resources),
			Resources:      result,
		}

		// Return JSON response
		c.JSON(http.StatusOK, response)
	}

	getClusterStatus = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get list of profiles (and their status) matching a cluster")

		failedOnly := getFailedOnlyFromQuery(c)
		limit, skip := getLimitAndSkipFromQuery(c)
		namespace, name, clusterType := getClusterFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("cluster %s:%s/%s", clusterType, namespace, name))
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("limit %d skip %d", limit, skip))
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("failed %t", failedOnly))

		user, err := validateToken(c)
		if err != nil {
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		manager := GetManagerInstance()

		canGetCluster, err := manager.canGetCluster(namespace, name, user, clusterType)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		if !canGetCluster {
			_ = c.AbortWithError(http.StatusUnauthorized, errors.New("no permissions to access this cluster"))
			return
		}

		clusterProfileStatuses := manager.GetClusterProfileStatusesByCluster(&namespace, &name, clusterType)

		flattenedProfileStatuses := flattenProfileStatuses(clusterProfileStatuses, failedOnly)
		sort.Slice(flattenedProfileStatuses, func(i, j int) bool {
			return sortClusterProfileStatus(flattenedProfileStatuses, i, j)
		})

		result, err := getFlattenedProfileStatusesInRange(flattenedProfileStatuses, limit, skip)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("bad request %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"totalResources": len(flattenedProfileStatuses),
			"profiles":       result,
		})
	}

	getProfiles = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get managed ClusterProfiles/Profiles")

		filters := getProfileFiltersFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("filters: kind %q namespace %q name %q",
			filters.Kind, filters.Namespace, filters.Name))

		user, err := validateToken(c)
		if err != nil {
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		manager := GetManagerInstance()

		canListClusterProfiles, err := manager.canListClusterProfiles(user)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		canListProfiles, err := manager.canListProfiles(user)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		profiles, err := manager.GetProfiles(c.Request.Context(), canListClusterProfiles, canListProfiles, user)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		profileData := getProfileData(profiles, filters)
		for k := range profileData {
			sort.Sort(profileData[k])
		}

		response := map[int32]ProfileResult{}
		for k := range profileData {
			response[k] = ProfileResult{
				TotalProfiles: len(profileData[k]),
				Profiles:      profileData[k],
			}
		}

		// Return JSON response
		c.JSON(http.StatusOK, response)
	}

	getProfile = func(c *gin.Context) {
		ginLogger.V(logs.LogDebug).Info("get a managed ClusterProfile/Profile")

		filters := getProfileFiltersFromQuery(c)
		ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("filters: kind %q namespace %q name %q",
			filters.Kind, filters.Namespace, filters.Name))

		if filters.Kind != configv1beta1.ClusterProfileKind &&
			filters.Kind != configv1beta1.ProfileKind {
			msg := fmt.Sprintf("supported kinds are %q and %q",
				configv1beta1.ClusterProfileKind, configv1beta1.ProfileKind)
			ginLogger.V(logs.LogInfo).Info(msg)
			_ = c.AbortWithError(http.StatusBadRequest, errors.New(msg))
		}

		if filters.Kind == configv1beta1.ProfileKind {
			if filters.Namespace == "" {
				msg := fmt.Sprintf("namespace is required for %q", configv1beta1.ProfileKind)
				ginLogger.V(logs.LogInfo).Info(msg)
				_ = c.AbortWithError(http.StatusBadRequest, errors.New(msg))
			}
		}

		if filters.Name == "" {
			msg := "name is required"
			ginLogger.V(logs.LogInfo).Info(msg)
			_ = c.AbortWithError(http.StatusBadRequest, errors.New(msg))
		}

		user, err := validateToken(c)
		if err != nil {
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}

		manager := GetManagerInstance()

		var canGetResource bool
		if filters.Kind == configv1beta1.ClusterProfileKind {
			canGetResource, err = manager.canGetClusterProfile(filters.Name, user)
		} else {
			canGetResource, err = manager.canGetProfile(filters.Namespace, filters.Name, user)
		}

		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to verify permissions %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}
		if !canGetResource {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("user does not have permission to access resource. URI: %s", c.Request.URL))
			_ = c.AbortWithError(http.StatusUnauthorized, err)
			return
		}

		profileRef := &corev1.ObjectReference{
			Kind:       filters.Kind,
			APIVersion: configv1beta1.GroupVersion.String(),
			Namespace:  filters.Namespace,
			Name:       filters.Name,
		}

		profileInfo := manager.GetProfile(profileRef)

		if reflect.DeepEqual(profileInfo, ProfileInfo{}) {
			c.JSON(http.StatusOK, "")
		}

		spec, matchingClusters, err := manager.getProfileSpecAndMatchingClusters(c.Request.Context(), profileRef, user)
		if err != nil {
			ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get profile instance. %s: %v", c.Request.URL, err))
			_ = c.AbortWithError(http.StatusInternalServerError, err)
			return
		}

		result := Profile{
			Kind:             profileRef.Kind,
			Namespace:        profileRef.Namespace,
			Name:             profileRef.Name,
			Spec:             *spec,
			MatchingClusters: matchingClusters,
			Dependencies:     transformSetToSlice(profileInfo.Dependencies),
			Dependents:       transformSetToSlice(profileInfo.Dependents),
		}

		// Return JSON response
		c.JSON(http.StatusOK, result)
	}
)

func (m *instance) start(ctx context.Context, port string, logger logr.Logger) {
	ginLogger = logger

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Return managed ClusterAPI powered clusters
	r.GET("/capiclusters", getManagedCAPIClusters)
	// Return SveltosClusters
	r.GET("/sveltosclusters", getManagedSveltosClusters)
	// Return helm charts deployed in a given managed cluster
	r.GET("/helmcharts", getDeployedHelmCharts)
	// Return resources deployed in a given managed cluster
	r.GET("/resources", getDeployedResources)
	// Return the specified cluster status
	r.GET("/getClusterStatus", getClusterStatus)
	// Return existing ClusterProfiles/Profiles
	r.GET("/profiles", getProfiles)
	// Return details about a ClusterProfile/Profile
	r.GET("/profile", getProfile)

	errCh := make(chan error)

	logger.V(logs.LogDebug).Info(fmt.Sprintf("Listening and serving HTTP on %s\n", port))

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

func getManagedClusterData(clusters map[corev1.ObjectReference]ClusterInfo, filters *clusterFilters,
) ManagedClusters {

	data := make(ManagedClusters, 0)
	for k := range clusters {
		if filters.Namespace != "" {
			if !strings.Contains(k.Namespace, filters.Namespace) {
				continue
			}
		}

		if filters.Name != "" {
			if !strings.Contains(k.Name, filters.Name) {
				continue
			}
		}

		if !filters.labelSelector.Empty() {
			if !filters.labelSelector.Matches(labels.Set(clusters[k].Labels)) {
				continue
			}
		}

		data = append(data, ManagedCluster{
			Namespace:   k.Namespace,
			Name:        k.Name,
			ClusterInfo: clusters[k],
		})
	}

	return data
}

// getProfileData groups profiles by tiers. Returns a map of all profiles for a given tier.
func getProfileData(profiles map[corev1.ObjectReference]ProfileInfo, filters *profileFilters,
) map[int32]Profiles {

	result := make(map[int32]Profiles)

	for k := range profiles {
		profile := profiles[k]
		if filters.Kind != "" {
			if k.Kind != filters.Kind {
				continue
			}
		}

		if filters.Namespace != "" {
			if !strings.Contains(k.Namespace, filters.Namespace) {
				continue
			}
		}

		if filters.Name != "" {
			if !strings.Contains(k.Name, filters.Name) {
				continue
			}
		}

		_, ok := result[profile.Tier]
		if !ok {
			result[profile.Tier] = make(Profiles, 0)
		}

		tmpProfile := Profile{
			Kind:         k.Kind,
			Namespace:    k.Namespace,
			Name:         k.Name,
			Dependencies: transformSetToSlice(profile.Dependencies),
			Dependents:   transformSetToSlice(profile.Dependents),
		}

		result[profile.Tier] = append(result[profile.Tier], tmpProfile)
	}

	return result
}

func transformSetToSlice(set *libsveltosset.Set) []corev1.ObjectReference {
	result := make([]corev1.ObjectReference, set.Len())

	dependencies := set.Items()

	for j := range dependencies {
		result[j] = corev1.ObjectReference{
			Kind:       dependencies[j].Kind,
			APIVersion: dependencies[j].APIVersion,
			Namespace:  dependencies[j].Namespace,
			Name:       dependencies[j].Name,
		}
	}

	return result
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

func getFailedOnlyFromQuery(c *gin.Context) bool {
	// Define default values for limit and skip
	failedOnly := false

	// Get the values from query parameters
	queryFailed := c.Query("failed")

	// Parse the query parameters to int (handle errors)
	var err error
	if queryFailed != "" {
		failedOnly, err = strconv.ParseBool(queryFailed)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid failed parameter"})
			return failedOnly
		}
	}

	return failedOnly
}

func getClusterFromQuery(c *gin.Context) (namespace, name string, clusterType libsveltosv1beta1.ClusterType) {
	// Get the values from query parameters
	queryNamespace := c.Query("namespace")
	queryName := c.Query("name")
	queryType := c.Query("type")

	// Parse the query parameters to int (handle errors)
	if queryNamespace == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "namespace is required"})
		return
	}
	if queryName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if queryType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster type is required"})
		return
	}

	if strings.EqualFold(queryType, string(libsveltosv1beta1.ClusterTypeSveltos)) {
		return queryNamespace, queryName, libsveltosv1beta1.ClusterTypeSveltos
	} else if strings.EqualFold(queryType, string(libsveltosv1beta1.ClusterTypeCapi)) {
		return queryNamespace, queryName, libsveltosv1beta1.ClusterTypeCapi
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "cluster type is incorrect"})
	return
}

func getTokenFromAuthorizationHeader(c *gin.Context) (string, error) {
	// Get the authorization header value
	authorizationHeader := c.GetHeader("Authorization")

	// Check if the authorization header is present
	if authorizationHeader == "" {
		errorMsg := "authorization header is missing"
		c.JSON(http.StatusUnauthorized, gin.H{"error": errorMsg})
		return "", errors.New(errorMsg)
	}

	// Extract the token from the authorization header
	// Assuming the authorization header format is "Bearer <token>"
	token := authorizationHeader[len("Bearer "):]
	// Check if the token is present
	if token == "" {
		errorMsg := "token is missing"
		c.JSON(http.StatusUnauthorized, gin.H{"error": errorMsg})
		return "", errors.New(errorMsg)
	}

	return token, nil
}

// validateToken:
// - gets token from authorization request. Returns an error if missing
// - validate token. Returns an error if this check fails
// - get and return user info. Returns an error if getting user from token fails
func validateToken(c *gin.Context) (string, error) {
	token, err := getTokenFromAuthorizationHeader(c)
	if err != nil {
		ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get token from authorization request. Request %s, error %v",
			c.Request.URL, err))
		_ = c.AbortWithError(http.StatusUnauthorized, errors.New("failed to get token from authorization request"))
		return "", err
	}

	manager := GetManagerInstance()
	err = manager.validateToken(token)
	if err != nil {
		ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to validate token: %v", err))
		_ = c.AbortWithError(http.StatusUnauthorized, errors.New("failed to validate token"))
		return "", err
	}

	user, err := manager.getUserFromToken(token)
	if err != nil {
		ginLogger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get user from token: %v", err))
		_ = c.AbortWithError(http.StatusUnauthorized, errors.New("failed to get user from token"))
		return "", err
	}

	ginLogger.V(logs.LogDebug).Info(fmt.Sprintf("user %s", user))

	return user, nil
}
