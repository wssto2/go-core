//nolint:paralleltest,testpackage,noctx,ireturn,varnamelen // tests in the style of the package
package binders

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bindBody[T any](t *testing.T, body string) (T, error) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	var in T

	err := BindRequest(c, &in)

	return in, err
}

type rawItem struct {
	Key     string          `json:"key"`
	Content json.RawMessage `json:"content"`
	Extra   map[string]any  `json:"extra"`
	List    []any           `json:"list"`
}

type rawIn struct {
	Raw   json.RawMessage `json:"raw"`
	Num   float64         `json:"num"`
	Items []rawItem       `json:"items"`
	Inner struct {
		Raw json.RawMessage `json:"raw"`
	} `json:"inner"`
	Ptr *json.RawMessage `json:"ptr"`
}

func TestBindRequest_RawMessage_KeepsTheBodysBytes(t *testing.T) {
	big := `{"z": 1.0, "a":[9007199254740993, 1e2], "m": {"b":2,"a":1}}`
	body := `{"num": 1.0, "raw": ` + big + `, "inner": {"raw": [3.10, "x"]},
		"ptr": {"q":  1.50},
		"items": [{"key":"k","content":{"y":1.0,"x":2},"extra":{"n":1,"o":{"p":[1,"two"]}},"list":[1,"a",null,{"b":true}]}]}`

	in, err := bindBody[rawIn](t, body)
	require.NoError(t, err)

	assert.Equal(t, big, string(in.Raw))
	assert.InDelta(t, 1.0, in.Num, 0)
	assert.Equal(t, `[3.10, "x"]`, string(in.Inner.Raw))
	require.NotNil(t, in.Ptr)
	assert.JSONEq(t, `{"q":1.50}`, string(*in.Ptr))
	assert.Equal(t, `{"q":  1.50}`, string(*in.Ptr))

	require.Len(t, in.Items, 1)
	item := in.Items[0]
	assert.Equal(t, `{"y":1.0,"x":2}`, string(item.Content))
	assert.Equal(t, map[string]any{"n": 1.0, "o": map[string]any{"p": []any{1.0, "two"}}}, item.Extra)
	assert.Equal(t, []any{1.0, "a", nil, map[string]any{"b": true}}, item.List)
}

func TestBindRequest_RawMessage_NullAndAbsentStayEmpty(t *testing.T) {
	in, err := bindBody[rawIn](t, `{"raw": null}`)
	require.NoError(t, err)
	assert.Nil(t, in.Raw)
	assert.Nil(t, in.Ptr)
}

func TestBindRequest_ItemsStillValidated(t *testing.T) {
	_, err := bindBody[rawIn](t, `{"items": [{"key": 5, "content": {"a":1}}]}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation")
}
