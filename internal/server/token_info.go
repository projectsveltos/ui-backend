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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// tokenInfo is what can safely be said about a token in a log: its kind and the claims that
// identify who it was issued by and for, never the token or who it belongs to.
type tokenInfo struct {
	// kind is "JWT", "Kubernetes ServiceAccount token" or "opaque token"
	kind     string
	issuer   string
	audience []string
	expiry   time.Time
	length   int
}

const (
	kindJWT            = "JWT"
	kindServiceAccount = "Kubernetes ServiceAccount token"
	kindOpaque         = "opaque token"

	// jwtSegments is the number of dot separated parts of a signed JWT: header, payload, signature
	jwtSegments = 3
)

// describeToken reads the payload of a token to describe it, without verifying it.
// Whatever the token is, the result never contains the token, its signature or its subject.
func describeToken(token string) *tokenInfo {
	info := &tokenInfo{kind: kindOpaque, length: len(token)}

	segments := strings.Split(token, ".")
	if len(segments) != jwtSegments {
		return info
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segments[1], "="))
	if err != nil {
		return info
	}

	var claims struct {
		Issuer     string          `json:"iss"`
		Audience   json.RawMessage `json:"aud"`
		Expiry     float64         `json:"exp"`
		Kubernetes json.RawMessage `json:"kubernetes.io"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return info
	}

	info.kind = kindJWT
	if len(claims.Kubernetes) != 0 {
		info.kind = kindServiceAccount
	}
	info.issuer = claims.Issuer
	info.audience = parseAudience(claims.Audience)
	if claims.Expiry != 0 {
		info.expiry = time.Unix(int64(claims.Expiry), 0).UTC()
	}

	return info
}

// parseAudience handles both forms of the aud claim: a string or a list of strings.
func parseAudience(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}

	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}
	}

	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}

	return nil
}

func (t *tokenInfo) isServiceAccount() bool {
	return t.kind == kindServiceAccount
}

func (t *tokenInfo) String() string {
	if t.kind == kindOpaque {
		return fmt.Sprintf("%s, %d characters, not a JWT", t.kind, t.length)
	}

	description := fmt.Sprintf("%s, issuer=%q audience=%v", t.kind, t.issuer, t.audience)
	if !t.expiry.IsZero() {
		state := "valid"
		if time.Now().After(t.expiry) {
			state = "expired"
		}
		description += fmt.Sprintf(" expires=%s (%s)", t.expiry.Format(time.RFC3339), state)
	}

	return description
}

// tokenTargetHost is where the dashboard user's own token is sent: the OIDC proxy when one is
// configured, the API server otherwise.
func (m *instance) tokenTargetHost() string {
	if m.oidcProxyHost != "" {
		return m.oidcProxyHost
	}
	return m.config.Host
}

// describeTokenFailure explains where a token that failed validation was sent and what it is,
// to be logged next to the error. It never contains the token.
func (m *instance) describeTokenFailure(token string) string {
	info := describeToken(token)

	description := fmt.Sprintf("Token (%s) was sent to %s.", info, m.tokenTargetHost())

	// An OIDC proxy only accepts tokens of its own issuer. Anything else, such as a ServiceAccount
	// token, is refused unless the proxy is set to pass such tokens through to the API server.
	if m.oidcProxyHost != "" && info.isServiceAccount() {
		description += " An OIDC proxy refuses ServiceAccount tokens unless token passthrough is enabled on it."
	}

	return description
}
