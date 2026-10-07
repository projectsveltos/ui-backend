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

package server_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2/textlogger"

	"github.com/projectsveltos/ui-backend/internal/server"
)

const (
	claimAudience   = "aud"
	claimKubernetes = "kubernetes.io"
	aliceEmail      = "alice@example.com"
	clusterIssuer   = "https://kubernetes.default.svc.cluster.local"
	oidcIssuer      = "https://dex.example.com"
	claimEmail      = "email"
	claimNamespace  = "namespace"
	claimIssuer     = "iss"
	claimExpiry     = "exp"
	claimSubject    = "sub"
	dashboardClient = "sveltos-dashboard"
	testSubject     = "user-1234"
)

// makeJWT builds an unsigned token with the given payload: describing a token never verifies it.
func makeJWT(claims map[string]interface{}) string {
	encode := func(v interface{}) string {
		data, err := json.Marshal(v)
		Expect(err).To(BeNil())
		return base64.RawURLEncoding.EncodeToString(data)
	}
	return encode(map[string]string{"alg": "RS256"}) + "." + encode(claims) + ".c2lnbmF0dXJlLXNlY3JldA"
}

var _ = Describe("describing a token for the logs", func() {
	It("reports the issuer, audience and expiry of an OIDC token", func() {
		token := makeJWT(map[string]interface{}{
			claimIssuer: oidcIssuer, claimAudience: dashboardClient, claimSubject: testSubject,
			claimEmail: aliceEmail, claimExpiry: time.Now().Add(time.Hour).Unix(),
		})

		description := server.DescribeToken(token)

		Expect(description).To(ContainSubstring("JWT"))
		Expect(description).To(ContainSubstring("issuer=\"" + oidcIssuer + "\""))
		Expect(description).To(ContainSubstring("audience=[" + dashboardClient + "]"))
		Expect(description).To(ContainSubstring("(valid)"))
	})

	It("reads an audience that is a list", func() {
		token := makeJWT(map[string]interface{}{claimIssuer: "i", claimAudience: []string{"a", "b"}})

		Expect(server.DescribeToken(token)).To(ContainSubstring("audience=[a b]"))
	})

	It("reports an expired token as expired", func() {
		token := makeJWT(map[string]interface{}{claimIssuer: "i", claimAudience: "a", claimExpiry: time.Now().Add(-time.Hour).Unix()})

		Expect(server.DescribeToken(token)).To(ContainSubstring("(expired)"))
	})

	It("recognizes a Kubernetes ServiceAccount token", func() {
		token := makeJWT(map[string]interface{}{
			claimIssuer:   clusterIssuer,
			claimAudience: []string{clusterIssuer},
			claimSubject:  "system:serviceaccount:default:platform-admin",
			claimKubernetes: map[string]interface{}{
				claimNamespace: testNamespace, "serviceaccount": map[string]string{"name": "platform-admin"},
			},
		})

		Expect(server.DescribeToken(token)).To(ContainSubstring("Kubernetes ServiceAccount token"))
	})

	It("says so when the token is not a JWT", func() {
		description := server.DescribeToken("an-opaque-access-token")

		Expect(description).To(ContainSubstring("opaque token"))
		Expect(description).To(ContainSubstring("not a JWT"))
	})

	It("does not fail on a malformed token", func() {
		for _, token := range []string{"", "a.b.c", "a.!!!.c", "header." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".sig"} {
			Expect(server.DescribeToken(token)).To(ContainSubstring("opaque token"))
		}
	})

	It("never includes the token, its signature or who it belongs to", func() {
		token := makeJWT(map[string]interface{}{
			claimIssuer: oidcIssuer, claimAudience: dashboardClient, claimSubject: testSubject,
			claimEmail: aliceEmail, claimExpiry: time.Now().Add(time.Hour).Unix(),
		})

		description := server.DescribeToken(token)

		Expect(description).ToNot(ContainSubstring(token))
		Expect(description).ToNot(ContainSubstring(strings.Split(token, ".")[2]))
		Expect(description).ToNot(ContainSubstring(testSubject))
		Expect(description).ToNot(ContainSubstring(aliceEmail))
	})
})

var _ = Describe("explaining a token that failed validation", func() {
	const proxyHost = "https://kube-oidc-proxy.kube-oidc-proxy.svc.cluster.local:443"
	const apiServerHost = "https://kubernetes.default.svc:443"

	serviceAccountToken := makeJWT(map[string]interface{}{
		claimIssuer: clusterIssuer, claimAudience: "x",
		claimKubernetes: map[string]interface{}{claimNamespace: testNamespace},
	})
	logger := textlogger.NewLogger(textlogger.NewConfig())

	It("names the OIDC proxy as where the token went, and warns about ServiceAccount tokens", func() {
		m := server.NewTestInstanceWithOIDCProxy(&rest.Config{Host: apiServerHost}, proxyHost, "", nil, logger)

		description := m.DescribeTokenFailure(serviceAccountToken)

		Expect(description).To(ContainSubstring("sent to " + proxyHost))
		Expect(description).To(ContainSubstring("Kubernetes ServiceAccount token"))
		Expect(description).To(ContainSubstring("token passthrough"))
	})

	It("does not warn about a ServiceAccount token when there is no proxy", func() {
		m := server.NewTestInstanceWithOIDCProxy(&rest.Config{Host: apiServerHost}, "", "", nil, logger)

		description := m.DescribeTokenFailure(serviceAccountToken)

		Expect(description).To(ContainSubstring("sent to " + apiServerHost))
		Expect(description).ToNot(ContainSubstring("passthrough"))
	})

	It("does not warn about an OIDC token that went to the proxy", func() {
		m := server.NewTestInstanceWithOIDCProxy(&rest.Config{Host: apiServerHost}, proxyHost, "", nil, logger)

		description := m.DescribeTokenFailure(makeJWT(map[string]interface{}{claimIssuer: oidcIssuer, claimAudience: "a"}))

		Expect(description).ToNot(ContainSubstring("passthrough"))
	})
})

var _ = Describe("reading the token from the Authorization header", func() {
	read := func(header string) (string, int, error) {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/stats", http.NoBody)
		if header != "" {
			c.Request.Header.Set("Authorization", header)
		}
		token, err := server.GetTokenFromAuthorizationHeader(c)
		return token, recorder.Code, err
	}

	It("returns the token of a bearer header", func() {
		token, _, err := read("Bearer abc.def.ghi")

		Expect(err).To(BeNil())
		Expect(token).To(Equal("abc.def.ghi"))
	})

	It("accepts the scheme in any case", func() {
		for _, header := range []string{"bearer abc", "BEARER abc", "BeArEr abc"} {
			token, _, err := read(header)
			Expect(err).To(BeNil())
			Expect(token).To(Equal("abc"))
		}
	})

	It("answers 401 when the header is missing", func() {
		_, code, err := read("")

		Expect(err).To(MatchError("authorization header is missing"))
		Expect(code).To(Equal(http.StatusUnauthorized))
	})

	It("answers 401 instead of failing when the header is shorter than the scheme", func() {
		for _, header := range []string{"Bearer", "Bear", "x"} {
			_, code, err := read(header)

			Expect(err).To(MatchError("authorization header is not a bearer token"))
			Expect(code).To(Equal(http.StatusUnauthorized))
		}
	})

	It("answers 401 for another scheme", func() {
		_, code, err := read("Basic dXNlcjpwYXNz")

		Expect(err).To(MatchError("authorization header is not a bearer token"))
		Expect(code).To(Equal(http.StatusUnauthorized))
	})

	It("answers 401 when the token is empty", func() {
		for _, header := range []string{"Bearer ", "Bearer    "} {
			_, code, err := read(header)

			Expect(err).To(MatchError("token is missing"))
			Expect(code).To(Equal(http.StatusUnauthorized))
		}
	})
})
