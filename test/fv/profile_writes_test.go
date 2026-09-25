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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// This exercises the dashboard's write path (POST/PUT/DELETE /profile) end to end, through the
// real, deployed ui-backend HTTP API against the real apiserver and the real addon-controller -
// not just the object shape ExpectSuccess assertions in the envtest-based unit tests cover, but
// whether a profile created this way is actually picked up and deployed by the rest of Sveltos.
var _ = Describe("Profile writes via ui-backend (POST/PUT/DELETE /profile)", func() {
	const writeConvergeTimeout = 2 * time.Minute

	It("creates, updates and deletes a Profile (YAML flow) end to end", Label("FV"), func() {
		Byf("Creating a cluster-admin-bound ServiceAccount to write through ui-backend with")
		adminNamespace := randomString()
		Expect(k8sClient.Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: adminNamespace},
		})).To(Succeed())

		adminSAName := randomString()
		Expect(k8sClient.Create(context.TODO(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Namespace: adminNamespace, Name: adminSAName},
		})).To(Succeed())

		adminBindingName := randomString()
		adminBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: adminBindingName},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     clusterRoleKind,
				Name:     clusterAdminClusterRoleName,
			},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.ServiceAccountKind, Namespace: adminNamespace, Name: adminSAName},
			},
		}
		Expect(k8sClient.Create(context.TODO(), adminBinding)).To(Succeed())
		token := getServiceAccountToken(adminNamespace, adminSAName)

		Byf("Port-forwarding to ui-backend-manager")
		localPort, stopChan := portForwardToPod(uiBackendPodLabels)

		// Profile is namespace-scoped and only targets clusters in its OWN namespace (unlike
		// ClusterProfile, which matches by label alone across all namespaces) - it must live in
		// kindWorkloadCluster.Namespace, not some unrelated freshly-created namespace, or no
		// cluster will ever match and no ClusterSummary will ever be created.
		profileNamespace := kindWorkloadCluster.Namespace
		profileName := randomString()
		yamlContent := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: fv-write-content\ndata:\n  hello: world\n"

		Byf("POST /profile: creating Profile %s/%s (YAML flow) targeting the fv workload cluster",
			profileNamespace, profileName)
		createReq := server.CreateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{
				Kind: configv1beta1.ProfileKind, Namespace: profileNamespace, Name: profileName,
			},
			ClusterSelector: map[string]string{key: value},
			YAML:            &yamlContent,
		}
		Expect(postJSON(localPort, token, http.MethodPost, "/profile", createReq, nil)).To(Succeed())

		Byf("Verifying the Profile and its content ConfigMap actually exist")
		var profile configv1beta1.Profile
		Eventually(func() bool {
			return k8sClient.Get(context.TODO(),
				types.NamespacedName{Namespace: profileNamespace, Name: profileName}, &profile) == nil
		}, timeout, pollingInterval).Should(BeTrue())
		Expect(profile.Spec.PolicyRefs).To(HaveLen(1))
		contentConfigMapName := profile.Spec.PolicyRefs[0].Name

		var contentConfigMap corev1.ConfigMap
		Expect(k8sClient.Get(context.TODO(),
			types.NamespacedName{Namespace: profileNamespace, Name: contentConfigMapName}, &contentConfigMap)).To(Succeed())

		Byf("Rejecting a second POST /profile with the same identity (name-collision handling)")
		var conflictBody bytes.Buffer
		statusCode := postJSONWithStatus(localPort, token, http.MethodPost, "/profile", createReq, &conflictBody)
		Expect(statusCode).To(Equal(http.StatusConflict))

		Byf("Waiting for the Resources feature to reach Provisioned on the real ClusterSummary "+
			"(up to %s: a real deploy, not just object creation)", writeConvergeTimeout)
		Eventually(func() bool {
			return resourcesFeatureIsProvisionedForProfile(profileNamespace, profileName)
		}, writeConvergeTimeout, pollingInterval).Should(BeTrue())

		Byf("PUT /profile: raising the Profile's Tier via SpecYAML")
		updateReq := server.UpdateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{
				Kind: configv1beta1.ProfileKind, Namespace: profileNamespace, Name: profileName,
			},
			SpecYAML: specYAMLWithTier(&profile, 200),
		}
		Expect(postJSON(localPort, token, http.MethodPut, "/profile", updateReq, nil)).To(Succeed())

		Eventually(func() bool {
			var updated configv1beta1.Profile
			if err := k8sClient.Get(context.TODO(),
				types.NamespacedName{Namespace: profileNamespace, Name: profileName}, &updated); err != nil {
				return false
			}
			return updated.Spec.Tier == 200
		}, timeout, pollingInterval).Should(BeTrue())

		Byf("DELETE /profile with removeReferencedContent=true")
		deleteReq := server.DeleteProfileRequest{
			ProfileIdentity: server.ProfileIdentity{
				Kind: configv1beta1.ProfileKind, Namespace: profileNamespace, Name: profileName,
			},
			RemoveReferencedContent: true,
		}
		Expect(postJSON(localPort, token, http.MethodDelete, "/profile", deleteReq, nil)).To(Succeed())

		Byf("Verifying the Profile and its content ConfigMap are both gone")
		Eventually(func() bool {
			err := k8sClient.Get(context.TODO(),
				types.NamespacedName{Namespace: profileNamespace, Name: profileName}, &profile)
			return apierrors.IsNotFound(err)
		}, timeout, pollingInterval).Should(BeTrue())
		Eventually(func() bool {
			err := k8sClient.Get(context.TODO(),
				types.NamespacedName{Namespace: profileNamespace, Name: contentConfigMapName}, &contentConfigMap)
			return apierrors.IsNotFound(err)
		}, timeout, pollingInterval).Should(BeTrue())

		close(stopChan)

		Byf("Cleaning up")
		Expect(k8sClient.Delete(context.TODO(), adminBinding)).To(Succeed())
		Expect(k8sClient.Delete(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: adminNamespace},
		})).To(Succeed())
	})
})

// specYAMLWithTier marshals profile's current Spec back to YAML with Tier overridden - a stand-in
// for what the dashboard's Edit view sends after a user edits the loaded YAML in the textarea.
func specYAMLWithTier(profile *configv1beta1.Profile, tier int32) string {
	spec := profile.Spec
	spec.Tier = tier
	out, err := json.Marshal(spec)
	Expect(err).To(BeNil())
	// json.Marshal, not sigs.k8s.io/yaml.Marshal: the server unmarshals with sigs.k8s.io/yaml,
	// which is JSON-superset-compatible, and this avoids a second yaml dependency in the test.
	return string(out)
}

// resourcesFeatureIsProvisionedForProfile returns true once at least one ClusterSummary owned by
// namespace/name reports its Resources feature (the YAML/PolicyRefs flow) as Provisioned.
func resourcesFeatureIsProvisionedForProfile(namespace, name string) bool {
	clusterSummaryList := &configv1beta1.ClusterSummaryList{}
	if err := k8sClient.List(context.TODO(), clusterSummaryList); err != nil {
		return false
	}

	for i := range clusterSummaryList.Items {
		cs := &clusterSummaryList.Items[i]
		ref, err := configv1beta1.GetProfileOwnerReference(cs)
		if err != nil || ref.Kind != configv1beta1.ProfileKind ||
			ref.Name != name || cs.Namespace != namespace {

			continue
		}
		for j := range cs.Status.FeatureSummaries {
			fs := &cs.Status.FeatureSummaries[j]
			if fs.FeatureID == libsveltosv1beta1.FeatureResources && fs.Status == libsveltosv1beta1.FeatureStatusProvisioned {
				return true
			}
		}
	}
	return false
}

// postJSON POSTs/PUTs/DELETEs body as JSON to ui-backend's HTTP API through the local port opened
// by portForwardToPod, authenticating with token, and fails the spec on a non-2xx response.
func postJSON(localPort int, token, method, path string, body, out any) error {
	statusCode, respBody := doJSON(localPort, token, method, path, body)
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status code %d for %s %s: %s", statusCode, method, path, respBody)
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}

// postJSONWithStatus is like postJSON but returns the status code instead of erroring on non-2xx,
// for tests asserting a specific error status (e.g. 409 Conflict).
func postJSONWithStatus(localPort int, token, method, path string, body any, respBuf *bytes.Buffer) int {
	statusCode, respBody := doJSON(localPort, token, method, path, body)
	if respBuf != nil {
		respBuf.Write(respBody)
	}
	return statusCode
}

func doJSON(localPort int, token, method, path string, body any) (statusCode int, respBody []byte) {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		Expect(err).To(BeNil())
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(context.TODO(), method,
		fmt.Sprintf("http://127.0.0.1:%d%s", localPort, path), reqBody)
	Expect(err).To(BeNil())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	Expect(err).To(BeNil())
	defer resp.Body.Close()

	respBody, err = io.ReadAll(resp.Body)
	Expect(err).To(BeNil())

	return resp.StatusCode, respBody
}
