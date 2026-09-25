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
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	"github.com/projectsveltos/addon-controller/lib/clusterops"
	eventv1beta1 "github.com/projectsveltos/event-manager/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/libsveltos/lib/clusterproxy"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
)

const (
	verbList = "list"
	verbGet  = "get"

	// SubjectAccessReview.Spec.ResourceAttributes.Resource must be the plural, lower-case
	// REST resource name (the same string that appears in an RBAC rule's resources list),
	// not the Kind. Using a Kind here (e.g. "SveltosCluster") never matches a real RBAC
	// rule, so every check below silently denies non-wildcard roles.
	resourceSveltosClusters              = "sveltosclusters"
	resourceClusters                     = "clusters"
	resourceClusterProfiles              = "clusterprofiles"
	resourceProfiles                     = "profiles"
	resourceClusterSummaries             = "clustersummaries"
	resourceEventTriggers                = "eventtriggers"
	resourceClassifiers                  = "classifiers"
	resourceManagementClusterClassifiers = "managementclusterclassifiers"
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

// getImpersonatedClient returns a controller-runtime client authenticated as the token's own
// owner (token pass-through, not rest.Config.Impersonate) rather than this manager's own
// ServiceAccount. Every create/update/delete on ClusterProfile/Profile/ConfigMap/Secret must go
// through this client so the apiserver enforces RBAC exactly as it would for a direct kubectl
// call by that user - never through m.client, which is this ServiceAccount's own cached client.
func (m *instance) getImpersonatedClient(token string) (client.Client, error) {
	config, err := m.getKubernetesRestConfig(token)
	if err != nil {
		return nil, err
	}

	return client.New(config, client.Options{Scheme: m.scheme})
}

// getUserFromToken returns the caller's username and group memberships, as reported by the
// API server for the given token. Both must be propagated into every SubjectAccessReview
// below: per the SubjectAccessReviewSpec.User doc, specifying User without Groups is
// interpreted as "what if User were not a member of any groups", so omitting Groups makes
// every RBAC binding to a Group subject (the common shape for OIDC/Entra ID group-based
// access) invisible to these checks.
func (m *instance) getUserFromToken(token string) (user string, groups []string, err error) {
	config, err := m.getKubernetesRestConfig(token)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get restConfig: %v", err))
		return "", nil, err
	}

	authV1Client, err := authenticationv1client.NewForConfig(config)
	if err != nil {
		return "", nil, err
	}

	res, err := authV1Client.SelfSubjectReviews().
		Create(context.TODO(), &authenticationv1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err != nil {
		return "", nil, err
	}

	return res.Status.UserInfo.Username, res.Status.UserInfo.Groups, nil
}

// canListSveltosClusters returns true if user can list all SveltosClusters in all namespaces
func (m *instance) canListSveltosClusters(user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbList,
				Group:    libsveltosv1beta1.GroupVersion.Group,
				Version:  libsveltosv1beta1.GroupVersion.Version,
				Resource: resourceSveltosClusters,
			},
			User:   user,
			Groups: groups,
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
func (m *instance) canGetSveltosCluster(clusterNamespace, clusterName, user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:      verbGet,
				Group:     libsveltosv1beta1.GroupVersion.Group,
				Version:   libsveltosv1beta1.GroupVersion.Version,
				Resource:  resourceSveltosClusters,
				Namespace: clusterNamespace,
				Name:      clusterName,
			},
			User:   user,
			Groups: groups,
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
func (m *instance) canListCAPIClusters(user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbList,
				Group:    clusterv1.GroupVersion.Group,
				Version:  clusterv1.GroupVersion.Version,
				Resource: resourceClusters,
			},
			User:   user,
			Groups: groups,
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
func (m *instance) canGetCAPICluster(clusterNamespace, clusterName, user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:      verbGet,
				Group:     clusterv1.GroupVersion.Group,
				Version:   clusterv1.GroupVersion.Version,
				Resource:  resourceClusters,
				Namespace: clusterNamespace,
				Name:      clusterName,
			},
			User:   user,
			Groups: groups,
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
func (m *instance) canGetCluster(clusterNamespace, clusterName, user string, groups []string,
	clusterType libsveltosv1beta1.ClusterType) (bool, error) {

	if clusterType == libsveltosv1beta1.ClusterTypeCapi {
		return m.canGetCAPICluster(clusterNamespace, clusterName, user, groups)
	}

	return m.canGetSveltosCluster(clusterNamespace, clusterName, user, groups)
}

// canListClusterProfiles verifies whether user has permission to view ClusterProfiles
func (m *instance) canListClusterProfiles(user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    configv1beta1.GroupVersion.Group,
				Version:  configv1beta1.GroupVersion.Version,
				Resource: resourceClusterProfiles,
			},
			User:   user,
			Groups: groups,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canListEventTriggers verifies whether user has permission to view EventTriggers
func (m *instance) canListEventTriggers(user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    eventv1beta1.GroupVersion.Group,
				Version:  eventv1beta1.GroupVersion.Version,
				Resource: resourceEventTriggers,
			},
			User:   user,
			Groups: groups,
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
func (m *instance) canGetClusterProfile(clusterProfileName, user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    configv1beta1.GroupVersion.Group,
				Version:  configv1beta1.GroupVersion.Version,
				Resource: resourceClusterProfiles,
				Name:     clusterProfileName,
			},
			User:   user,
			Groups: groups,
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
func (m *instance) canListProfiles(user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    configv1beta1.GroupVersion.Group,
				Version:  configv1beta1.GroupVersion.Version,
				Resource: resourceProfiles,
			},
			User:   user,
			Groups: groups,
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
func (m *instance) canGetProfile(profileNamespace, profileName, user string, groups []string) (bool, error) {
	// Create a Kubernetes clientset
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:      verbGet,
				Group:     configv1beta1.GroupVersion.Group,
				Version:   configv1beta1.GroupVersion.Version,
				Resource:  resourceProfiles,
				Name:      profileName,
				Namespace: profileNamespace,
			},
			User:   user,
			Groups: groups,
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
	user string, groups []string) (*configv1beta1.Spec, []MatchingClusters, error) {

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
		canGet, err := m.canGetCluster(cluster.Namespace, cluster.Name, user, groups, clusterproxy.GetClusterType(cluster))
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

// canListClusterSummaries verifies whether user has permission to list ClusterSummaries
func (m *instance) canListClusterSummaries(user string, groups []string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbList,
				Group:    configv1beta1.GroupVersion.Group,
				Version:  configv1beta1.GroupVersion.Version,
				Resource: resourceClusterSummaries,
			},
			User:   user,
			Groups: groups,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetClusterSummary returns true if user can access ClusterSummary namespace/name
func (m *instance) canGetClusterSummary(namespace, name, user string, groups []string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:      verbGet,
				Group:     configv1beta1.GroupVersion.Group,
				Version:   configv1beta1.GroupVersion.Version,
				Resource:  resourceClusterSummaries,
				Namespace: namespace,
				Name:      name,
			},
			User:   user,
			Groups: groups,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetEventTrigger returns true if user can access EventTrigger name
func (m *instance) canGetEventTrigger(name, user string, groups []string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    eventv1beta1.GroupVersion.Group,
				Version:  eventv1beta1.GroupVersion.Version,
				Resource: resourceEventTriggers,
				Name:     name,
			},
			User:   user,
			Groups: groups,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canListClassifiers verifies whether user has permission to view Classifiers
func (m *instance) canListClassifiers(user string, groups []string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    libsveltosv1beta1.GroupVersion.Group,
				Version:  libsveltosv1beta1.GroupVersion.Version,
				Resource: resourceClassifiers,
			},
			User:   user,
			Groups: groups,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetClassifier returns true if user can access Classifier name
func (m *instance) canGetClassifier(name, user string, groups []string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    libsveltosv1beta1.GroupVersion.Group,
				Version:  libsveltosv1beta1.GroupVersion.Version,
				Resource: resourceClassifiers,
				Name:     name,
			},
			User:   user,
			Groups: groups,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canListManagementClusterClassifiers verifies whether user has permission to view
// ManagementClusterClassifiers
func (m *instance) canListManagementClusterClassifiers(user string, groups []string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    libsveltosv1beta1.GroupVersion.Group,
				Version:  libsveltosv1beta1.GroupVersion.Version,
				Resource: resourceManagementClusterClassifiers,
			},
			User:   user,
			Groups: groups,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}

// canGetManagementClusterClassifier returns true if user can access ManagementClusterClassifier name
func (m *instance) canGetManagementClusterClassifier(name, user string, groups []string) (bool, error) {
	clientset, err := kubernetes.NewForConfig(m.config)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to get clientset: %v", err))
		return false, err
	}

	sar := &authorizationapi.SubjectAccessReview{
		Spec: authorizationapi.SubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Verb:     verbGet,
				Group:    libsveltosv1beta1.GroupVersion.Group,
				Version:  libsveltosv1beta1.GroupVersion.Version,
				Resource: resourceManagementClusterClassifiers,
				Name:     name,
			},
			User:   user,
			Groups: groups,
		},
	}

	canI, err := clientset.AuthorizationV1().SubjectAccessReviews().Create(context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to check clientset permissions: %v", err))
		return false, err
	}

	return canI.Status.Allowed, nil
}
