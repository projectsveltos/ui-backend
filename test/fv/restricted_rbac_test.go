/*
Copyright 2026. projectsveltos.io. All rights reserved.

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

package fv_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
)

// Regression coverage for a code path distinct from both the full-access case (canListAll=true,
// served straight from the cache) and the Group-based RBAC test (group_rbac_test.go): when a
// caller can only `get` specific named resources (not `list` everything), ui-backend falls back
// to a live List filtered down to only the items the caller can individually `get`. That
// filtering must both include what the caller has access to and exclude what it doesn't - a
// scoped RBAC role granting nothing is easy to satisfy trivially, so this test creates two
// ClusterProfiles and grants access to only one of them, via a direct User subject (not a
// Group) and RBAC's `resourceNames`, to prove both directions.
var _ = Describe("Restricted RBAC filtering", func() {
	It("ui-backend's /profiles only returns ClusterProfiles the caller can individually get", Label("FV"), func() {
		// Neither ClusterProfile needs to actually match a cluster or deploy anything - this
		// test is only about whether the RBAC-filtered list includes/excludes the right names.
		nonMatchingLabels := map[string]string{"restricted-rbac-fv": randomString()}

		visibleProfile := getClusterProfile("restricted-visible-", nonMatchingLabels)
		Byf("Creating ClusterProfile %s (the caller WILL be granted access to this one)", visibleProfile.Name)
		Expect(k8sClient.Create(context.TODO(), visibleProfile)).To(Succeed())

		hiddenProfile := getClusterProfile("restricted-hidden-", nonMatchingLabels)
		Byf("Creating ClusterProfile %s (the caller will NOT be granted access to this one)", hiddenProfile.Name)
		Expect(k8sClient.Create(context.TODO(), hiddenProfile)).To(Succeed())

		namespace := randomString()
		Expect(k8sClient.Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		})).To(Succeed())

		saName := randomString()
		Expect(k8sClient.Create(context.TODO(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: saName},
		})).To(Succeed())

		token := getServiceAccountToken(namespace, saName)
		user := fmt.Sprintf("system:serviceaccount:%s:%s", namespace, saName)

		clusterRoleName := randomString()
		Byf("Creating ClusterRole %s granting get on ONLY %s (via resourceNames)", clusterRoleName, visibleProfile.Name)
		clusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups:     []string{configv1beta1.GroupVersion.Group},
					Resources:     []string{"clusterprofiles"},
					ResourceNames: []string{visibleProfile.Name},
					Verbs:         []string{verbGet},
				},
			},
		}
		Expect(k8sClient.Create(context.TODO(), clusterRole)).To(Succeed())

		bindingName := randomString()
		Byf("Binding ClusterRole %s to User %s (not a Group)", clusterRoleName, user)
		binding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: bindingName},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     clusterRoleKind,
				Name:     clusterRoleName,
			},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.UserKind, Name: user, APIGroup: rbacv1.GroupName},
			},
		}
		Expect(k8sClient.Create(context.TODO(), binding)).To(Succeed())

		Byf("Port-forwarding to ui-backend-manager")
		localPort, stopChan := portForwardToPod(uiBackendPodLabels)

		Byf("Verifying /profiles includes %s and excludes %s", visibleProfile.Name, hiddenProfile.Name)
		Eventually(func() bool {
			profiles, err := getProfiles(localPort, token)
			if err != nil {
				return false
			}
			return profileNamed(profiles, visibleProfile.Name) != nil &&
				profileNamed(profiles, hiddenProfile.Name) == nil
		}, timeout, pollingInterval).Should(BeTrue())

		close(stopChan)

		Byf("Cleaning up")
		Expect(k8sClient.Delete(context.TODO(), binding)).To(Succeed())
		Expect(k8sClient.Delete(context.TODO(), clusterRole)).To(Succeed())
		Expect(k8sClient.Delete(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: namespace},
		})).To(Succeed())
		deleteClusterProfile(visibleProfile)
		deleteClusterProfile(hiddenProfile)
	})
})
