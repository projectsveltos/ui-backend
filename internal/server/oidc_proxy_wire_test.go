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
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2/textlogger"

	"github.com/projectsveltos/ui-backend/internal/server"
)

// unusedAPIServer is the API server address of the manager under test: the dashboard user's token goes to
// the proxy, never here.
const unusedAPIServer = "https://unused.invalid"

// fakeOIDCProxy is a TLS server standing in for kube-oidc-proxy. It records the path and the
// Authorization header of every request it receives, which is what the real proxy bases its
// authentication decision on.
type fakeOIDCProxy struct {
	*httptest.Server
	mux           sync.Mutex
	authorization map[string][]string
	hosts         map[string][]string
}

func newFakeOIDCProxy() *fakeOIDCProxy {
	proxy := &fakeOIDCProxy{authorization: map[string][]string{}, hosts: map[string][]string{}}
	proxy.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxy.mux.Lock()
		proxy.authorization[r.URL.Path] = append(proxy.authorization[r.URL.Path], r.Header.Get("Authorization"))
		proxy.hosts[r.URL.Path] = append(proxy.hosts[r.URL.Path], r.Host)
		proxy.mux.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"major":"1","minor":"30","gitVersion":"v1.30.0"}`))
		case "/apis/authentication.k8s.io/v1/selfsubjectreviews":
			review := authenticationv1.SelfSubjectReview{
				Status: authenticationv1.SelfSubjectReviewStatus{
					UserInfo: authenticationv1.UserInfo{Username: "alice", Groups: []string{"platform"}},
				},
			}
			_ = json.NewEncoder(w).Encode(review)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return proxy
}

func (p *fakeOIDCProxy) authorizationFor(path string) []string {
	p.mux.Lock()
	defer p.mux.Unlock()
	return append([]string(nil), p.authorization[path]...)
}

func (p *fakeOIDCProxy) hostFor(path string) []string {
	p.mux.Lock()
	defer p.mux.Unlock()
	return append([]string(nil), p.hosts[path]...)
}

// address is the host:port the proxy listens on, which is the Host header a client sends by default.
func (p *fakeOIDCProxy) address() string {
	return p.Listener.Addr().String()
}

// caFile writes the proxy's certificate where --oidc-proxy-ca-file would point.
func (p *fakeOIDCProxy) caFile() string {
	caFile := filepath.Join(GinkgoT().TempDir(), "ca.crt")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: p.Certificate().Raw})
	Expect(os.WriteFile(caFile, data, 0o600)).To(Succeed())
	return caFile
}

// The OIDC proxy decides on the Authorization header alone. These tests check, on the wire, that the
// token of the dashboard user reaches the proxy in the flows meant to carry it.
var _ = Describe("dashboard token sent to the OIDC proxy", func() {
	const token = "header.payload.signature"
	const wantHeader = "Bearer " + token

	var proxy *fakeOIDCProxy
	var m interface {
		ValidateToken(token string) error
		GetUserFromToken(token string) (string, []string, error)
	}

	BeforeEach(func() {
		proxy = newFakeOIDCProxy()
		m = server.NewTestInstanceWithOIDCProxy(&rest.Config{Host: unusedAPIServer},
			proxy.URL, proxy.caFile(), nil, textlogger.NewLogger(textlogger.NewConfig()))
	})

	AfterEach(func() {
		proxy.Close()
	})

	It("validateToken sends the bearer token on the /version request", func() {
		Expect(m.ValidateToken(token)).To(Succeed())

		Expect(proxy.authorizationFor("/version")).To(Equal([]string{wantHeader}))
	})

	It("getUserFromToken sends the bearer token on the SelfSubjectReview request", func() {
		user, groups, err := m.GetUserFromToken(token)
		Expect(err).To(BeNil())
		Expect(user).To(Equal("alice"))
		Expect(groups).To(ContainElement("platform"))

		Expect(proxy.authorizationFor("/apis/authentication.k8s.io/v1/selfsubjectreviews")).To(Equal([]string{wantHeader}))
	})
})

// A load balancer in front of the API server rejects a Host header that is not its own (HTTP 421), and the
// proxy forwards the Host header it receives. These tests check what the proxy receives.
var _ = Describe("Host header sent to the OIDC proxy", func() {
	const token = "header.payload.signature"
	const apiServerHost = "api.example.internal"
	const versionPath = "/version"
	const selfSubjectReviewPath = "/apis/authentication.k8s.io/v1/selfsubjectreviews"

	var proxy *fakeOIDCProxy

	newInstance := func(apiServerHostFlag string) interface {
		ValidateToken(token string) error
		GetUserFromToken(token string) (string, []string, error)
	} {
		m := server.NewTestInstanceWithOIDCProxy(&rest.Config{Host: unusedAPIServer},
			proxy.URL, proxy.caFile(), nil, textlogger.NewLogger(textlogger.NewConfig()))
		return m.SetOIDCProxyAPIServerHost(apiServerHostFlag)
	}

	BeforeEach(func() {
		proxy = newFakeOIDCProxy()
	})

	AfterEach(func() {
		proxy.Close()
	})

	It("sends the API server host when it is set, on both flows that carry the user token", func() {
		m := newInstance(apiServerHost)

		Expect(m.ValidateToken(token)).To(Succeed())
		_, _, err := m.GetUserFromToken(token)
		Expect(err).To(BeNil())

		Expect(proxy.hostFor(versionPath)).To(Equal([]string{apiServerHost}))
		Expect(proxy.hostFor(selfSubjectReviewPath)).To(Equal([]string{apiServerHost}))
	})

	It("still connects to the proxy and verifies its certificate when the Host header is overridden", func() {
		m := newInstance(apiServerHost)

		// The proxy's certificate is for its own address, not for apiServerHost: the request succeeds
		// only if the override leaves the TLS server name alone.
		Expect(m.ValidateToken(token)).To(Succeed())
	})

	It("sends the token and the API server host together", func() {
		m := newInstance(apiServerHost)

		Expect(m.ValidateToken(token)).To(Succeed())

		Expect(proxy.authorizationFor(versionPath)).To(Equal([]string{"Bearer " + token}))
		Expect(proxy.hostFor(versionPath)).To(Equal([]string{apiServerHost}))
	})

	It("sends the address of the proxy when the API server host is not set", func() {
		m := newInstance("")

		Expect(m.ValidateToken(token)).To(Succeed())

		Expect(proxy.hostFor(versionPath)).To(Equal([]string{proxy.address()}))
	})
})
