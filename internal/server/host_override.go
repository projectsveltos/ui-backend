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

package server

import (
	"net/http"

	"k8s.io/client-go/transport"
)

// hostOverrideRoundTripper sets the Host header of every request to host. Only the header changes: the
// connection, the TLS server name and the certificate check still use the address of the request URL.
type hostOverrideRoundTripper struct {
	host string
	next http.RoundTripper
}

func (r *hostOverrideRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// A RoundTripper must not modify the request it is given
	req = req.Clone(req.Context())
	req.Host = r.host

	return r.next.RoundTrip(req)
}

func (r *hostOverrideRoundTripper) WrappedRoundTripper() http.RoundTripper {
	return r.next
}

// newHostOverrideWrapper returns a rest.Config.WrapTransport function that sets the Host header to host.
func newHostOverrideWrapper(host string) transport.WrapperFunc {
	return func(next http.RoundTripper) http.RoundTripper {
		return &hostOverrideRoundTripper{host: host, next: next}
	}
}
