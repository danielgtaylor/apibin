package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma"
	"github.com/danielgtaylor/huma/conditional"
	"github.com/fxamacker/cbor/v2"
	msgpack "github.com/shamaton/msgpack/v2"
	"github.com/zeebo/xxh3"
	"gopkg.in/yaml.v2"
)

type RequestInfo struct {
	// You should generally not do this, but we want to introspect the request
	// itself and echo it back, so we need to store its info.
	request *http.Request
}

func (r *RequestInfo) Resolve(ctx huma.Context, req *http.Request) {
	r.request = req
}

type EchoModel struct {
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Host    string            `json:"host,omitempty"`
	URL     string            `json:"url"`
	Body    string            `json:"body"`
	Parsed  interface{}       `json:"parsed,omitempty"`
}

func tryParse(contentType string, body []byte) interface{} {
	var result interface{}
	switch {
	case strings.Contains(contentType, "json"):
		json.Unmarshal(body, &result)
	case strings.Contains(contentType, "yaml"):
		yaml.Unmarshal(body, &result)
	case strings.Contains(contentType, "cbor"):
		cbor.Unmarshal(body, &result)
	case strings.Contains(contentType, "msgpack"):
		msgpack.Unmarshal(body, &result)
	}
	return result
}

func genETag(v interface{}) string {
	m, _ := cbor.Marshal(v)
	return genETagBytes(m)
}

func genETagBytes(b []byte) string {
	hash := xxh3.Hash(b)
	sum := make([]byte, 8)
	binary.BigEndian.PutUint64(sum, hash)
	return base64.RawURLEncoding.EncodeToString(sum)
}

func echoHandler(ctx huma.Context, input struct {
	RequestInfo
	Status int `query:"status" default:"200" minimum:"100" maximum:"599" doc:"Status code to return"`
	conditional.Params
	Body io.Reader
}) {
	headers := map[string]string{}
	for k, v := range input.request.Header {
		headers[k] = strings.Join(v, ", ")
	}

	body := ""
	var parsed interface{}
	if input.request.Body != nil {
		defer input.request.Body.Close()
		b, err := ioutil.ReadAll(input.request.Body)
		if err == nil {
			body = string(b)
			parsed = tryParse(input.request.Header.Get("Content-Type"), b)
		}
	}

	resp := EchoModel{
		Method:  input.request.Method,
		Headers: headers,
		Host:    input.request.Host,
		URL:     input.request.URL.String(),
		Body:    body,
		Parsed:  parsed,
	}

	lastModified, _ := time.Parse(time.RFC3339, "2022-02-01T12:34:56Z")
	etag := genETag(resp)

	if input.PreconditionFailed(ctx, etag, lastModified) {
		return
	}

	ctx.Header().Set("Cache-Control", "no-cache")
	ctx.Header().Set("Last-Modified", lastModified.Format(http.TimeFormat))
	ctx.Header().Set("ETag", `"`+etag+`"`)

	ctx.WriteModel(input.Status, resp)
}