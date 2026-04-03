package frontend

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegisterSPA_NilLogger_NoPanic confirms that passing a nil logger to
// RegisterSPA does not panic either at registration time or when the NoRoute
// handler fires for a non-API path.
func TestRegisterSPA_NilLogger_NoPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// Use minimal config; templates are not loaded so we can't render HTML,
	// but the handler should still not panic before hitting the render call.
	cfg := SPAConfig{
		APIPrefix: "/api",
	}

	require.NotPanics(t, func() {
		RegisterSPA(engine, cfg, nil)
	}, "RegisterSPA must not panic with a nil logger")

	// Trigger the NoRoute handler for an API path (returns JSON, no template needed).
	req := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	w := httptest.NewRecorder()
	require.NotPanics(t, func() {
		engine.ServeHTTP(w, req)
	}, "NoRoute handler must not panic with a nil logger")

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestBuiltinFuncMap_ToJSON_ReturnsTemplateJS verifies that toJSON returns
// a template.JS value (not escaped as a string) so it is safe in <script> blocks.
func TestBuiltinFuncMap_ToJSON_ReturnsTemplateJS(t *testing.T) {
	funcs := BuiltinFuncMap()
	toJSON, ok := funcs["toJSON"]
	require.True(t, ok, "toJSON must be in BuiltinFuncMap")

	type testVal struct{ Name string }
	result := toJSON.(func(any) template.JS)(testVal{Name: "hello<world>"})

	// template.JS values are not HTML-escaped, so < must remain as-is.
	assert.Contains(t, string(result), "<world>", "< must not be escaped in template.JS output")
	assert.Contains(t, string(result), `"hello`, "JSON output must be valid JSON")
}
