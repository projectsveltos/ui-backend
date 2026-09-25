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
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"github.com/projectsveltos/ui-backend/internal/server"
)

// Regression test for a bug where ui-backend's SubjectAccessReview checks sent only the
// caller's username and never their group memberships. Per the Kubernetes API's own doc for
// SubjectAccessReviewSpec.User, specifying User without Groups is interpreted as "what if this
// user were in no groups at all" - so RBAC granted only to a Group subject (the common shape
// for OIDC/Entra ID group-based access) was silently invisible, no matter what role it granted.
//
// kind has no OIDC provider to reproduce an Entra ID login, so this test uses a ServiceAccount
// token instead: Kubernetes automatically places every ServiceAccount in the group
// "system:serviceaccounts:<namespace>", which is a real Group subject resolved server-side
// exactly like an OIDC group claim would be. RBAC below is granted to that group only - never
// to the ServiceAccount itself - reproducing the reported bug's shape precisely.
var _ = Describe("Group-based RBAC", func() {
	It("ui-backend honors RBAC granted to a Group the caller belongs to", Label("FV"), func() {
		namespace := randomString()
		Byf("Creating namespace %s", namespace)
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Expect(k8sClient.Create(context.TODO(), ns)).To(Succeed())

		saName := randomString()
		Byf("Creating ServiceAccount %s/%s", namespace, saName)
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: saName}}
		Expect(k8sClient.Create(context.TODO(), sa)).To(Succeed())

		token := getServiceAccountToken(namespace, saName)

		Byf("Port-forwarding to ui-backend-manager")
		localPort, stopChan := portForwardToPod(uiBackendPodLabels)

		Byf("Verifying the ServiceAccount sees no clusters before any RBAC is granted")
		Eventually(func() bool {
			result, err := getCAPIClusters(localPort, token)
			return err == nil && result.TotalClusters == 0
		}, timeout, pollingInterval).Should(BeTrue())

		// This is the crux of the test: the ClusterRoleBinding subject below is the Group
		// every ServiceAccount in this namespace automatically belongs to. There is no
		// binding anywhere naming the ServiceAccount itself.
		group := fmt.Sprintf("system:serviceaccounts:%s", namespace)

		clusterRoleName := randomString()
		Byf("Creating ClusterRole %s granting get/list/watch on Cluster API clusters", clusterRoleName)
		clusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{clusterv1.GroupVersion.Group}, Resources: []string{"clusters"}, Verbs: []string{verbGet, "list", "watch"}},
			},
		}
		Expect(k8sClient.Create(context.TODO(), clusterRole)).To(Succeed())

		bindingName := randomString()
		Byf("Binding ClusterRole %s to Group %s (not to the ServiceAccount)", clusterRoleName, group)
		binding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: bindingName},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     clusterRoleKind,
				Name:     clusterRoleName,
			},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.GroupKind, Name: group, APIGroup: rbacv1.GroupName},
			},
		}
		Expect(k8sClient.Create(context.TODO(), binding)).To(Succeed())

		Byf("Verifying the ServiceAccount now sees the workload cluster via its Group membership")
		Eventually(func() bool {
			result, err := getCAPIClusters(localPort, token)
			if err != nil {
				return false
			}
			return result.TotalClusters == 1
		}, timeout, pollingInterval).Should(BeTrue())

		close(stopChan)

		Byf("Cleaning up")
		Expect(k8sClient.Delete(context.TODO(), binding)).To(Succeed())
		Expect(k8sClient.Delete(context.TODO(), clusterRole)).To(Succeed())
		Expect(k8sClient.Delete(context.TODO(), ns)).To(Succeed())
	})
})

// getCAPIClusters calls ui-backend's /capiclusters endpoint through the given local port
// (opened by portForwardToPod), authenticating with token.
func getCAPIClusters(localPort int, token string) (*server.ClusterResult, error) {
	result := &server.ClusterResult{}
	err := getJSON(localPort, token, "/capiclusters", result)
	return result, err
}
