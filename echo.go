package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/conditional"
	"github.com/fxamacker/cbor/v2"
	"github.com/zeebo/xxh3"
)

type RequestInfo struct {
	// You should generally not do this, but we want to introspect the request
	// itself and echo it back, so we need to store its info.
	ctx huma.Context
}

func (r *RequestInfo) Resolve(ctx huma.Context) []error {
	r.ctx = ctx
	return nil
}

type EchoModel struct {
	Method  string            `json:"method" enum:"GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS" doc:"HTTP method used" example:"GET"`
	Headers map[string]string `json:"headers" doc:"HTTP headers"`
	Host    string            `json:"host,omitempty" doc:"Hostname and optional port" example:"api.rest.sh"`
	URL     string            `json:"url" format:"uri" doc:"Full URL" example:"https://api.rest.sh/get?show_env=1"`
	Path    string            `json:"path" doc:"URL path" example:"/get"`
	Query   map[string]string `json:"query,omitempty" doc:"URL query parameters"`
	Body    interface{}       `json:"body,omitempty" doc:"Raw request body, either a UTF-8 string or bytes"`
	Parsed  interface{}       `json:"parsed,omitempty" doc:"Parsed request body"`
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

func quoteETag(tag string) string {
	return strconv.Quote(tag)
}

type EchoResponse struct {
	Status int

	CacheControl string    `header:"Cache-Control"`
	ETag         string    `header:"ETag"`
	LastModified time.Time `header:"Last-Modified"`
	Vary         string    `header:"Vary"`

	Body EchoModel
}

type BodyResponse struct {
	Body any
}

func (s *APIServer) echoHandler(ctx context.Context, input *struct {
	RequestInfo
	Status int `query:"status" default:"200" minimum:"100" maximum:"599" doc:"Status code to return"`
	conditional.Params
	Body    any
	RawBody []byte
}) (*EchoResponse, error) {
	headers := map[string]string{}
	input.ctx.EachHeader(func(name, value string) {
		headers[name] = value
	})

	reqURL := input.ctx.URL()

	query := map[string]string{}
	values, _ := url.ParseQuery(reqURL.RawQuery)
	for k := range values {
		query[k] = values.Get(k)
	}

	scheme := "http"
	if proto := input.ctx.Header("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	var rawBody any
	if len(input.RawBody) > 0 {
		if utf8.Valid(input.RawBody) {
			rawBody = string(input.RawBody)
		} else {
			rawBody = input.RawBody
		}
	}

	resp := &EchoResponse{}
	resp.Body = EchoModel{
		Method:  input.ctx.Method(),
		Headers: headers,
		Host:    input.ctx.Host(),
		URL:     scheme + "://" + input.ctx.Host() + reqURL.String(),
		Path:    reqURL.Path,
		Query:   query,
		Body:    rawBody,
		Parsed:  input.Body,
	}

	lastModified, _ := time.Parse(time.RFC3339, "2022-02-01T12:34:56Z")
	etag := genETag(resp)

	if err := input.PreconditionFailed(etag, lastModified); err != nil {
		return nil, err
	}

	resp.Status = input.Status

	resp.CacheControl = "no-store"
	resp.Vary = "*"
	resp.LastModified = lastModified
	resp.ETag = quoteETag(etag)

	return resp, nil
}

func (s *APIServer) RegisterBody(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "post-body",
		Method:      http.MethodPost,
		Path:        "/body",
		Summary:     "Return the parsed request body",
		Description: "Echo the parsed request body as the complete response body.",
		Tags:        []string{"Echo"},
	}, func(ctx context.Context, input *struct {
		Body any `doc:"Request body to parse and echo"`
	}) (*BodyResponse, error) {
		return &BodyResponse{Body: input.Body}, nil
	})
}

func (s *APIServer) RegisterEcho(api huma.API) {
	for _, method := range []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	} {
		huma.Register(api, huma.Operation{
			OperationID: strings.ToLower(method) + "-echo",
			Method:      method,
			Path:        "/",
			Tags:        []string{"Echo"},
		}, s.echoHandler)
	}
}
