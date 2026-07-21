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
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gin-gonic/gin"

	"github.com/projectsveltos/ui-backend/internal/mcpclient"
	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("AbortMCPError", func() {
	It("responds with 503 when the MCP server is unreachable", func() {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		server.AbortMCPError(c, mcpclient.ErrMCPServerUnavailable)

		Expect(c.Writer.Status()).To(Equal(http.StatusServiceUnavailable))
	})

	It("responds with 500 for any other error", func() {
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		server.AbortMCPError(c, errors.New(randomString()))

		Expect(c.Writer.Status()).To(Equal(http.StatusInternalServerError))
	})
})
