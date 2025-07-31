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

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationapi "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	authenticationv1client "k8s.io/client-go/kubernetes/typed/authentication/v1"
	"k8s.io/client-go/rest"
	certutil "k8s.io/client-go/util/cert"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	"github.com/projectsveltos/addon-controller/lib/clusterops"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/libsveltos/lib/clusterproxy"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
)

func (m *instance) getKubernetesRestConfig(token string) (*rest.Config, error) {
	const (
		rootCAFile = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	)

	tlsClientConfig := rest.TLSClientConfig{}
	if _, err := certutil.NewPool(rootCAFile); err != nil {
		return nil, err
	} else {
		tlsClientConfig.CAFile = rootCAFile
	}

	return &rest.Config{
		BearerToken:     token,
		Host:            m.config.Host,
		TLSClientConfig: tlsClientConfig,
	}, nil
}

func (m *instance) getUserFromToken(token string) (string, error) {
	config, err := m.getKubernetesRestConfig(token)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get restConfig: %v", err))
		return "", err
	}

	authV1Client, err := authenticationv1client.NewForConfig(config)
	if err != nil {
		return "", err
	}

	res, err := authV1Client.SelfSubjectReviews().
		Create(context.TODO(), &authenticationv1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}

	return res.Status.UserInfo.Username, nil
}

// canListSveltosClusters returns true if user can list all SveltosClusters in all namespaces
func (m *instance) canListSveltosClusters(user string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     "list",
				Group:    libsveltosv1beta1.GroupVersion.Group,
				Version:  libsveltosv1beta1.GroupVersion.Version,
				Resource: libsveltosv1beta1.SveltosClusterKind,
			},
			User: user,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetSveltosCluster returns true if user can access SveltosCluster clusterNamespace:clusterName
func (m *instance) canGetSveltosCluster(clusterNamespace, clusterName, user string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:      "get",
				Group:     libsveltosv1beta1.GroupVersion.Group,
				Version:   libsveltosv1beta1.GroupVersion.Version,
				Resource:  libsveltosv1beta1.SveltosClusterKind,
				Namespace: clusterNamespace,
				Name:      clusterName,
			},
			User: user,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canListCAPIClusters returns true if user can list all CAPI Clusters in all namespaces
func (m *instance) canListCAPIClusters(user string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     "list",
				Group:    clusterv1.GroupVersion.Group,
				Version:  clusterv1.GroupVersion.Version,
				Resource: clusterv1.ClusterKind,
			},
			User: user,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetCAPICluster returns true if user can access CAPI Cluster clusterNamespace:clusterName
func (m *instance) canGetCAPICluster(clusterNamespace, clusterName, user string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:      "get",
				Group:     clusterv1.GroupVersion.Group,
				Version:   clusterv1.GroupVersion.Version,
				Resource:  clusterv1.ClusterKind,
				Namespace: clusterNamespace,
				Name:      clusterName,
			},
			User: user,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetCluster verifies whether user has permission to view CAPI/Sveltos Cluster
func (m *instance) canGetCluster(clusterNamespace, clusterName, user string,
	clusterType libsveltosv1beta1.ClusterType) (bool, error) {

	if clusterType == libsveltosv1beta1.ClusterTypeCapi {
		return m.canGetCAPICluster(clusterNamespace, clusterName, user)
	}

	return m.canGetSveltosCluster(clusterNamespace, clusterName, user)
}

// canListClusterProfiles verifies whether user has permission to view ClusterProfiles
func (m *instance) canListClusterProfiles(user string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     "get",
				Group:    configv1beta1.GroupVersion.Group,
				Version:  configv1beta1.GroupVersion.Version,
				Resource: configv1beta1.ClusterProfileKind,
			},
			User: user,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetClusterProfile returns true if user can access ClusterProfile
func (m *instance) canGetClusterProfile(clusterProfileName, user string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     "get",
				Group:    configv1beta1.GroupVersion.Group,
				Version:  configv1beta1.GroupVersion.Version,
				Resource: configv1beta1.ClusterProfileKind,
				Name:     clusterProfileName,
			},
			User: user,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canListProfiles verifies whether user has permission to view Profiles
func (m *instance) canListProfiles(user string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     "get",
				Group:    configv1beta1.GroupVersion.Group,
				Version:  configv1beta1.GroupVersion.Version,
				Resource: configv1beta1.ProfileKind,
			},
			User: user,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetProfile returns true if user can access Profile
func (m *instance) canGetProfile(profileNamespace, profileName, user string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:      "get",
				Group:     configv1beta1.GroupVersion.Group,
				Version:   configv1beta1.GroupVersion.Version,
				Resource:  configv1beta1.ProfileKind,
				Name:      profileName,
				Namespace: profileNamespace,
			},
			User: user,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

func (m *instance) getClusterProfileInstance(ctx context.Context, name string) (*configv1beta1.ClusterProfile, error) {
	clusterProfile := configv1beta1.ClusterProfile{}

	err := m.client.Get(ctx, types.NamespacedName{Name: name}, &clusterProfile)
	return &clusterProfile, err
}

func (m *instance) getProfileInstance(ctx context.Context, namespace, name string) (*configv1beta1.Profile, error) {
	profile := configv1beta1.Profile{}

	err := m.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &profile)
	return &profile, err
}

// getProfileSpecAndMatchingClusters returns:
// - profile Spec
// - list of all matching clusters. For each matching cluster, status of each feature is reported.
func (m *instance) getProfileSpecAndMatchingClusters(ctx context.Context, profileRef *corev1.ObjectReference,
	user string) (*configv1beta1.Spec, []MatchingClusters, error) {

	var spec configv1beta1.Spec
	var matchingClusters []corev1.ObjectReference
	if profileRef.Kind == configv1beta1.ClusterProfileKind {
		cp, err := m.getClusterProfileInstance(ctx, profileRef.Name)
		if err != nil {
			return nil, nil, err
		}

		spec = cp.Spec
		matchingClusters = cp.Status.MatchingClusterRefs
	} else {
		p, err := m.getProfileInstance(ctx, profileRef.Namespace, profileRef.Name)
		if err != nil {
			return nil, nil, err
		}
		spec = p.Spec
		matchingClusters = p.Status.MatchingClusterRefs
	}

	accessibleMatchingClusters := make([]MatchingClusters, 0)
	for i := range matchingClusters {
		cluster := &matchingClusters[i]
		canGet, err := m.canGetCluster(cluster.Namespace, cluster.Name, user, clusterproxy.GetClusterType(cluster))
		if err != nil {
			return nil, nil, err
		}
		if canGet {
			clusterSummaryName := clusterops.GetClusterSummaryName(profileRef.Kind, profileRef.Name,
				cluster.Name, cluster.Kind == libsveltosv1beta1.SveltosClusterKind)

			clusterSummaryRef := &corev1.ObjectReference{
				Namespace:  cluster.Namespace,
				Name:       clusterSummaryName,
				Kind:       configv1beta1.ClusterSummaryKind,
				APIVersion: configv1beta1.GroupVersion.String(),
			}

			m.clusterStatusesMux.Lock()
			clusterProfileStatuses := m.clusterSummaryReport[*clusterSummaryRef]
			m.clusterStatusesMux.Unlock()
			accessibleMatchingClusters = append(accessibleMatchingClusters,
				MatchingClusters{
					Cluster:                 *cluster,
					ClusterFeatureSummaries: clusterProfileStatuses.Summary,
				})
		}
	}

	return &spec, accessibleMatchingClusters, nil
}
