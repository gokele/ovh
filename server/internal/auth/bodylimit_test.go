package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestBodyLimitCoversDeclaredAndStreamingBodies(t *testing.T) {
	for _, contentLength := range []int64{-1, 1000} {
		router := gin.New()
		router.Use(LimitRequestBody(64))
		parsed := false
		router.POST("/api/app/pair", func(c *gin.Context) {
			var body struct {
				Code string `json:"code"`
			}
			if err := c.ShouldBindJSON(&body); err != nil {
				c.Status(http.StatusBadRequest)
				return
			}
			parsed = true
			c.Status(http.StatusOK)
		})
		request := httptest.NewRequest("POST", "/api/app/pair", strings.NewReader(`{"code":"`+strings.Repeat("x", 1000)+`"}`))
		request.ContentLength = contentLength
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if parsed || response.Code < 400 {
			t.Fatal("oversized request reached the application", response.Code)
		}
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("POST", "/api/app/pair", strings.NewReader(`{"code":"valid"}`)))
		if !parsed || response.Code != http.StatusOK {
			t.Fatal("valid request was rejected", response.Code)
		}
	}
}
