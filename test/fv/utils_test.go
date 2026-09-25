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

package fv_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	"github.com/projectsveltos/addon-controller/lib/clusterops"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

const (
	key   = "env"
	value = "fv"

	// clusterRoleKind is the RoleRef.Kind used by every ClusterRoleBinding created across the
	// RBAC-related fv tests.
	clusterRoleKind = "ClusterRole"

	// clusterAdminClusterRoleName is the built-in ClusterRole bound by every fv spec that needs
	// a fully-privileged caller to exercise ui-backend's write path or "can list everything" path.
	clusterAdminClusterRoleName = "cluster-admin"

	// verbGet is the RBAC verb used across the RBAC-related fv tests to grant narrow,
	// get-only access (as opposed to list-everything access).
	verbGet = "get"

	// uiBackendNamespace and uiBackendLabelKey/uiBackendLabelValue locate the ui-backend-manager
	// pod that the HTTP-hitting fv tests port-forward to.
	uiBackendNamespace  = "projectsveltos"
	uiBackendLabelKey   = "control-plane"
	uiBackendLabelValue = "ui-backend"
	uiBackendPort       = 8080
)

// uiBackendPodLabels selects the ui-backend-manager pod for portForwardToPod.
var uiBackendPodLabels = map[string]string{uiBackendLabelKey: uiBackendLabelValue}

func getClusterProfile(namePrefix string, clusterLabels map[string]string) *configv1beta1.ClusterProfile {
	clusterProfile := &configv1beta1.ClusterProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name: namePrefix + randomString(),
		},
		Spec: configv1beta1.Spec{
			ClusterSelector: libsveltosv1beta1.Selector{
				LabelSelector: metav1.LabelSelector{
					MatchLabels: clusterLabels,
				},
			},
		},
	}

	return clusterProfile
}

// deleteClusterProfile deletes ClusterProfile and verifies all ClusterSummaries created by this ClusterProfile
// instances are also gone
func deleteClusterProfile(clusterProfile *configv1beta1.ClusterProfile) {
	listOptions := []client.ListOption{
		client.MatchingLabels{
			clusterops.ClusterProfileLabelName: clusterProfile.Name,
		},
	}
	clusterSummaryList := &configv1beta1.ClusterSummaryList{}
	Expect(k8sClient.List(context.TODO(), clusterSummaryList, listOptions...)).To(Succeed())

	Byf("Deleting the ClusterProfile %s", clusterProfile.Name)
	currentClusterProfile := &configv1beta1.ClusterProfile{}
	Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: clusterProfile.Name}, currentClusterProfile)).To(BeNil())
	Expect(k8sClient.Delete(context.TODO(), currentClusterProfile)).To(Succeed())

	for i := range clusterSummaryList.Items {
		Byf("Verifying ClusterSummary %s are gone", clusterSummaryList.Items[i].Name)
	}
	Eventually(func() bool {
		for i := range clusterSummaryList.Items {
			clusterSummaryNamespace := clusterSummaryList.Items[i].Namespace
			clusterSummaryName := clusterSummaryList.Items[i].Name
			currentClusterSummary := &configv1beta1.ClusterSummary{}
			err := k8sClient.Get(context.TODO(),
				types.NamespacedName{Namespace: clusterSummaryNamespace, Name: clusterSummaryName}, currentClusterSummary)
			if err == nil || !apierrors.IsNotFound(err) {
				return false
			}
		}
		return true
	}, timeout, pollingInterval).Should(BeTrue())

	Byf("Verifying ClusterProfile %s is gone", clusterProfile.Name)

	Eventually(func() bool {
		err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: clusterProfile.Name}, currentClusterProfile)
		return apierrors.IsNotFound(err)
	}, timeout, pollingInterval).Should(BeTrue())
}

func randomString() string {
	const length = 10
	return "fv-" + util.RandomString(length)
}

// getServiceAccountToken requests a short-lived token for the given ServiceAccount via the
// TokenRequest API. This is a real bearer token, resolved server-side (via SelfSubjectReview)
// to the ServiceAccount's username and its group memberships, exactly like an OIDC token would
// be - it's the mechanism the group-based RBAC test uses in place of an actual OIDC provider.
func getServiceAccountToken(namespace, name string) string {
	const tokenTTLSeconds = 600

	tr, err := clientset.CoreV1().ServiceAccounts(namespace).CreateToken(context.TODO(), name,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				ExpirationSeconds: ptr.To(int64(tokenTTLSeconds)),
			},
		}, metav1.CreateOptions{})
	Expect(err).To(BeNil())

	return tr.Status.Token
}

// portForwardToPod opens a local port forwarded to uiBackendPort on a Ready pod matching
// labelSelector in uiBackendNamespace (the only namespace/port any fv spec ever port-forwards
// into), mirroring what `kubectl port-forward` does. It returns the local port that was picked
// and a stop channel; closing the stop channel tears the forward down.
func portForwardToPod(labelSelector map[string]string) (localPort int, stop chan struct{}) {
	var podName string
	Eventually(func() bool {
		podList := &corev1.PodList{}
		listOptions := []client.ListOption{
			client.InNamespace(uiBackendNamespace),
			client.MatchingLabels(labelSelector),
		}
		if err := k8sClient.List(context.TODO(), podList, listOptions...); err != nil {
			return false
		}
		for i := range podList.Items {
			pod := &podList.Items[i]
			if pod.Status.Phase == corev1.PodRunning {
				podName = pod.Name
				return true
			}
		}
		return false
	}, timeout, pollingInterval).Should(BeTrue())

	roundTripper, upgrader, err := spdy.RoundTripperFor(restConfig)
	Expect(err).To(BeNil())

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(uiBackendNamespace).
		Name(podName).
		SubResource("portforward")

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, req.URL())

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)

	forwarder, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", uiBackendPort)}, stopChan, readyChan, out, errOut)
	Expect(err).To(BeNil())

	go func() {
		defer GinkgoRecover()
		if fwErr := forwarder.ForwardPorts(); fwErr != nil {
			Byf("port-forward to %s/%s exited: %v", uiBackendNamespace, podName, fwErr)
		}
	}()

	<-readyChan

	ports, err := forwarder.GetPorts()
	Expect(err).To(BeNil())
	Expect(ports).To(HaveLen(1))

	return int(ports[0].Local), stopChan
}
