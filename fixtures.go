package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	mathrand "math/rand"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/danielgtaylor/huma/v2"
	"github.com/fxamacker/cbor/v2"
	"gopkg.in/yaml.v2"
)

type TokenResponse struct {
	Body struct {
		Token     string `json:"token"`
		TokenType string `json:"token_type"`
		User      string `json:"user"`
	}
}

func (s *APIServer) RegisterLogin(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "post-login",
		Method:      http.MethodPost,
		Path:        "/login",
		Summary:     "Mock form login",
		Description: "Accepts an application/x-www-form-urlencoded username and password and returns a mock bearer token.",
		Tags:        []string{"Auth"},
	}, func(ctx context.Context, input *struct {
		RawBody []byte `contentType:"application/x-www-form-urlencoded"`
	}) (*TokenResponse, error) {
		form, err := url.ParseQuery(string(input.RawBody))
		if err != nil {
			return nil, huma.Error400BadRequest("invalid form body")
		}
		user := form.Get("username")
		if user == "" {
			user = "anonymous"
		}
		resp := &TokenResponse{}
		resp.Body.Token = "docs-token-" + user
		resp.Body.TokenType = "Bearer"
		resp.Body.User = user
		return resp, nil
	})
}

type UploadFile struct {
	Field       string `json:"field"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size"`
}

type UploadResponse struct {
	Body struct {
		Fields map[string][]string `json:"fields"`
		Files  []UploadFile        `json:"files"`
	}
}

func (s *APIServer) RegisterUploads(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "post-upload",
		Method:      http.MethodPost,
		Path:        "/uploads",
		Summary:     "Echo multipart upload metadata",
		Tags:        []string{"Uploads"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
		RawBody []byte `contentType:"multipart/form-data"`
	}) (*UploadResponse, error) {
		_, params, err := mime.ParseMediaType(input.ctx.Header("Content-Type"))
		if err != nil {
			return nil, huma.Error400BadRequest("invalid multipart content type")
		}
		boundary := params["boundary"]
		if boundary == "" {
			return nil, huma.Error400BadRequest("missing multipart boundary")
		}
		form, err := multipart.NewReader(bytes.NewReader(input.RawBody), boundary).ReadForm(32 << 20)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid multipart body")
		}
		defer form.RemoveAll()
		resp := &UploadResponse{}
		resp.Body.Fields = form.Value
		for field, files := range form.File {
			for _, fh := range files {
				size := fh.Size
				if size == 0 {
					if f, err := fh.Open(); err == nil {
						n, _ := io.Copy(io.Discard, f)
						f.Close()
						size = n
					}
				}
				resp.Body.Files = append(resp.Body.Files, UploadFile{
					Field:       field,
					Filename:    fh.Filename,
					ContentType: fh.Header.Get("Content-Type"),
					Size:        size,
				})
			}
		}
		return resp, nil
	})
}

type AuthResponse struct {
	Body struct {
		Authenticated bool   `json:"authenticated"`
		Scheme        string `json:"scheme"`
		Subject       string `json:"subject,omitempty"`
	}
}

func addAuthSchemes(api huma.API) {
	o := api.OpenAPI()
	if o.Components.SecuritySchemes == nil {
		o.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	o.Components.SecuritySchemes["basicAuth"] = &huma.SecurityScheme{Type: "http", Scheme: "basic"}
	o.Components.SecuritySchemes["bearerAuth"] = &huma.SecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "opaque"}
	o.Components.SecuritySchemes["apiKeyHeader"] = &huma.SecurityScheme{Type: "apiKey", In: "header", Name: "X-API-Key"}
	o.Components.SecuritySchemes["apiKeyQuery"] = &huma.SecurityScheme{Type: "apiKey", In: "query", Name: "api_key"}
}

func addRestishCLIConfig(o *huma.OpenAPI) {
	if o.Extensions == nil {
		o.Extensions = map[string]any{}
	}
	o.Extensions["x-cli-config"] = map[string]any{
		"security": "basicAuth",
		"params": map[string]any{
			"username": "docs",
			"password": "docs",
		},
		"profiles": map[string]any{
			"default": map[string]any{
				"credentials": map[string]any{
					"basicAuth": map[string]any{
						"auth": map[string]any{
							"type": "http-basic",
							"params": map[string]any{
								"username": "docs",
								"password": "docs",
							},
						},
					},
					"bearerAuth": map[string]any{
						"auth": map[string]any{
							"type": "bearer",
							"params": map[string]any{
								"token": "docs-token",
							},
						},
					},
					"apiKeyHeader": map[string]any{
						"auth": map[string]any{
							"type": "api-key",
							"params": map[string]any{
								"in":    "header",
								"name":  "X-API-Key",
								"value": "docs-key",
							},
						},
					},
					"apiKeyQuery": map[string]any{
						"auth": map[string]any{
							"type": "api-key",
							"params": map[string]any{
								"in":    "query",
								"name":  "api_key",
								"value": "docs-query-key",
							},
						},
					},
				},
			},
		},
	}
}

func authOK(scheme, subject string) *AuthResponse {
	resp := &AuthResponse{}
	resp.Body.Authenticated = true
	resp.Body.Scheme = scheme
	resp.Body.Subject = subject
	return resp
}

func (s *APIServer) RegisterAuthBasic(api huma.API) {
	addAuthSchemes(api)
	huma.Register(api, huma.Operation{
		OperationID: "get-auth-basic",
		Method:      http.MethodGet,
		Path:        "/auth/basic",
		Summary:     "Require HTTP Basic auth",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{{"basicAuth": {}}},
	}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*AuthResponse, error) {
		user, pass, ok := parseBasic(input.ctx.Header("Authorization"))
		if !ok || user == "" || pass == "" {
			return nil, huma.Error401Unauthorized("missing or invalid Basic auth")
		}
		return authOK("basic", user), nil
	})
}

func (s *APIServer) RegisterAuthBearer(api huma.API) {
	addAuthSchemes(api)
	huma.Register(api, huma.Operation{
		OperationID: "get-auth-bearer",
		Method:      http.MethodGet,
		Path:        "/auth/bearer",
		Summary:     "Require bearer token auth",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{{"bearerAuth": {}}},
	}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*AuthResponse, error) {
		token, ok := strings.CutPrefix(input.ctx.Header("Authorization"), "Bearer ")
		if !ok || token == "" {
			return nil, huma.Error401Unauthorized("missing bearer token")
		}
		return authOK("bearer", token), nil
	})
}

func (s *APIServer) RegisterAuthAPIKeyHeader(api huma.API) {
	addAuthSchemes(api)
	huma.Register(api, huma.Operation{
		OperationID: "get-auth-api-key-header",
		Method:      http.MethodGet,
		Path:        "/auth/api-key-header",
		Summary:     "Require an API key header",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{{"apiKeyHeader": {}}},
	}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*AuthResponse, error) {
		key := input.ctx.Header("X-API-Key")
		if key == "" {
			return nil, huma.Error401Unauthorized("missing X-API-Key")
		}
		return authOK("api-key-header", key), nil
	})
}

func (s *APIServer) RegisterAuthAPIKeyQuery(api huma.API) {
	addAuthSchemes(api)
	huma.Register(api, huma.Operation{
		OperationID: "get-auth-api-key-query",
		Method:      http.MethodGet,
		Path:        "/auth/api-key-query",
		Summary:     "Require an API key query parameter",
		Tags:        []string{"Auth"},
		Security:    []map[string][]string{{"apiKeyQuery": {}}},
	}, func(ctx context.Context, input *struct {
		APIKey string `query:"api_key" doc:"API key"`
	}) (*AuthResponse, error) {
		if input.APIKey == "" {
			return nil, huma.Error401Unauthorized("missing api_key")
		}
		return authOK("api-key-query", input.APIKey), nil
	})
}

func parseBasic(header string) (string, string, bool) {
	if !strings.HasPrefix(header, "Basic ") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, "Basic "))
	if err != nil {
		return "", "", false
	}
	user, pass, ok := strings.Cut(string(decoded), ":")
	return user, pass, ok
}

type Item struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	Tags    []string  `json:"tags,omitempty"`
	Updated time.Time `json:"updated"`
}

type ItemsResponse struct {
	Body []Item
}

type ItemResponse struct {
	Body Item
}

var itemsMu = sync.RWMutex{}
var items = map[string]Item{}
var itemOrder = []string{}

func resetItems() {
	itemsMu.Lock()
	defer itemsMu.Unlock()
	now := time.Now().UTC().Truncate(time.Second)
	items = map[string]Item{
		"alpha": {ID: "alpha", Name: "Alpha item", Enabled: true, Tags: []string{"docs", "example"}, Updated: now},
		"beta":  {ID: "beta", Name: "Beta item", Enabled: false, Tags: []string{"demo"}, Updated: now},
	}
	itemOrder = []string{"alpha", "beta"}
}

func init() {
	resetItems()
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			resetItems()
		}
	}()
}

func (s *APIServer) RegisterListItems(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-items",
		Method:      http.MethodGet,
		Path:        "/items",
		Summary:     "List sample items",
		Tags:        []string{"Items"},
	}, func(ctx context.Context, input *struct{}) (*ItemsResponse, error) {
		itemsMu.RLock()
		defer itemsMu.RUnlock()
		body := make([]Item, 0, len(itemOrder))
		for _, id := range itemOrder {
			body = append(body, items[id])
		}
		return &ItemsResponse{Body: body}, nil
	})
}

func (s *APIServer) RegisterGetItem(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-item",
		Method:      http.MethodGet,
		Path:        "/items/{item-id}",
		Summary:     "Get a sample item",
		Tags:        []string{"Items"},
	}, func(ctx context.Context, input *struct {
		ID string `path:"item-id"`
	}) (*ItemResponse, error) {
		itemsMu.RLock()
		defer itemsMu.RUnlock()
		item, ok := items[input.ID]
		if !ok {
			return nil, huma.Error404NotFound(input.ID + " not found")
		}
		return &ItemResponse{Body: item}, nil
	})
}

func (s *APIServer) RegisterPostItem(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "create-item",
		Method:      http.MethodPost,
		Path:        "/items",
		Summary:     "Create a sample item",
		Tags:        []string{"Items"},
	}, func(ctx context.Context, input *struct {
		Body Item
	}) (*ItemResponse, error) {
		itemsMu.Lock()
		defer itemsMu.Unlock()
		item := input.Body
		if item.ID == "" {
			item.ID = fmt.Sprintf("item-%d", time.Now().UnixNano())
		}
		if _, ok := items[item.ID]; !ok {
			itemOrder = append(itemOrder, item.ID)
		}
		item.Updated = time.Now().UTC().Truncate(time.Second)
		items[item.ID] = item
		return &ItemResponse{Body: item}, nil
	})
}

func (s *APIServer) RegisterPatchItem(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "patch-item",
		Method:      http.MethodPatch,
		Path:        "/items/{item-id}",
		Summary:     "Patch a sample item",
		Tags:        []string{"Items"},
	}, func(ctx context.Context, input *struct {
		ID   string `path:"item-id"`
		Body map[string]any
	}) (*ItemResponse, error) {
		itemsMu.Lock()
		defer itemsMu.Unlock()
		item, ok := items[input.ID]
		if !ok {
			return nil, huma.Error404NotFound(input.ID + " not found")
		}
		if name, ok := input.Body["name"].(string); ok {
			item.Name = name
		}
		if enabled, ok := input.Body["enabled"].(bool); ok {
			item.Enabled = enabled
		}
		if tags, ok := input.Body["tags"].([]any); ok {
			item.Tags = item.Tags[:0]
			for _, tag := range tags {
				if s, ok := tag.(string); ok {
					item.Tags = append(item.Tags, s)
				}
			}
		}
		item.Updated = time.Now().UTC().Truncate(time.Second)
		items[input.ID] = item
		return &ItemResponse{Body: item}, nil
	})
}

func (s *APIServer) RegisterDeleteItem(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "delete-item",
		Method:      http.MethodDelete,
		Path:        "/items/{item-id}",
		Summary:     "Delete a sample item",
		Tags:        []string{"Items"},
	}, func(ctx context.Context, input *struct {
		ID string `path:"item-id"`
	}) (*struct{}, error) {
		itemsMu.Lock()
		defer itemsMu.Unlock()
		delete(items, input.ID)
		for i, id := range itemOrder {
			if id == input.ID {
				itemOrder = append(itemOrder[:i], itemOrder[i+1:]...)
				break
			}
		}
		return nil, nil
	})
}

var flakyMu sync.Mutex
var flakyCounts = map[string]int{}

func (s *APIServer) RegisterFlaky(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-flaky",
		Method:      http.MethodGet,
		Path:        "/flaky",
		Summary:     "Fail a configurable number of times, then succeed",
		Tags:        []string{"Reliability"},
	}, func(ctx context.Context, input *struct {
		Failures int    `query:"failures" default:"2" minimum:"0" maximum:"20"`
		Key      string `query:"key" default:"default"`
	}) (*struct {
		Status int
		Body   map[string]any
	}, error) {
		flakyMu.Lock()
		defer flakyMu.Unlock()
		key := input.Key
		if key == "" {
			key = "default"
		}
		attempt := flakyCounts[key] + 1
		flakyCounts[key] = attempt
		if attempt <= input.Failures {
			return &struct {
				Status int
				Body   map[string]any
			}{Status: http.StatusServiceUnavailable, Body: map[string]any{
				"attempt":  attempt,
				"failures": input.Failures,
				"key":      key,
				"ok":       false,
			}}, nil
		}
		return &struct {
			Status int
			Body   map[string]any
		}{Status: http.StatusOK, Body: map[string]any{
			"attempt":  attempt,
			"failures": input.Failures,
			"key":      key,
			"ok":       true,
		}}, nil
	})
}

func (s *APIServer) RegisterSlow(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-slow",
		Method:      http.MethodGet,
		Path:        "/slow",
		Summary:     "Delay before responding",
		Tags:        []string{"Reliability"},
	}, func(ctx context.Context, input *struct {
		Delay string `query:"delay" default:"1s" doc:"Delay duration, for example 500ms or 2s"`
	}) (*struct {
		Body map[string]any
	}, error) {
		delay, err := time.ParseDuration(input.Delay)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid delay")
		}
		if delay < 0 || delay > 10*time.Second {
			return nil, huma.Error400BadRequest("delay must be between 0s and 10s")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		return &struct {
			Body map[string]any
		}{Body: map[string]any{"delay": delay.String(), "ok": true}}, nil
	})
}

func (s *APIServer) RegisterRequestFixtures(api huma.API) {
	for _, route := range []struct {
		Method     string
		Path       string
		ID         string
		Parameters []*huma.Param
	}{
		{Method: http.MethodGet, Path: "/anything", ID: "get-anything"},
		{Method: http.MethodGet, Path: "/anything/{path}", ID: "get-anything-path", Parameters: []*huma.Param{
			{
				Name:        "path",
				In:          "path",
				Description: "Path suffix to echo",
				Required:    true,
				Schema:      &huma.Schema{Type: "string"},
			},
		}},
		{Method: http.MethodGet, Path: "/get", ID: "get-method"},
		{Method: http.MethodPost, Path: "/post", ID: "post-method"},
		{Method: http.MethodPut, Path: "/put", ID: "put-method"},
		{Method: http.MethodPatch, Path: "/patch", ID: "patch-method"},
		{Method: http.MethodDelete, Path: "/delete", ID: "delete-method"},
		{Method: http.MethodHead, Path: "/head", ID: "head-method"},
		{Method: http.MethodOptions, Path: "/options", ID: "options-method"},
	} {
		huma.Register(api, huma.Operation{
			OperationID: route.ID,
			Method:      route.Method,
			Path:        route.Path,
			Summary:     "Echo request data",
			Tags:        []string{"Inspection"},
			Parameters:  route.Parameters,
		}, s.echoHandler)
	}
}

func (s *APIServer) RegisterHeaders(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-headers",
		Method:      http.MethodGet,
		Path:        "/headers",
		Summary:     "Return request headers",
		Tags:        []string{"Inspection"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*struct {
		Body map[string]map[string]string
	}, error) {
		headers := map[string]string{}
		input.ctx.EachHeader(func(name, value string) {
			headers[name] = value
		})
		return &struct {
			Body map[string]map[string]string
		}{Body: map[string]map[string]string{"headers": headers}}, nil
	})
}

func (s *APIServer) RegisterUserAgent(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-user-agent",
		Method:      http.MethodGet,
		Path:        "/user-agent",
		Summary:     "Return the User-Agent header",
		Tags:        []string{"Inspection"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*struct {
		Body map[string]string
	}, error) {
		return &struct {
			Body map[string]string
		}{Body: map[string]string{"user-agent": input.ctx.Header("User-Agent")}}, nil
	})
}

func (s *APIServer) RegisterResponseHeaders(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-response-headers",
		Method:      http.MethodGet,
		Path:        "/response-headers",
		Summary:     "Set response headers from query parameters",
		Tags:        []string{"Inspection"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*struct {
		Body map[string]string
	}, error) {
		headers := map[string]string{}
		values, _ := url.ParseQuery(input.ctx.URL().RawQuery)
		for name, v := range values {
			value := v[len(v)-1]
			input.ctx.SetHeader(name, value)
			headers[name] = value
		}
		return &struct {
			Body map[string]string
		}{Body: headers}, nil
	})
}

func (s *APIServer) RegisterRedirects(api huma.API) {
	for _, route := range []struct {
		Path string
		ID   string
		Abs  bool
	}{
		{"/redirect/{n}", "get-redirect", false},
		{"/relative-redirect/{n}", "get-relative-redirect", false},
		{"/absolute-redirect/{n}", "get-absolute-redirect", true},
	} {
		abs := route.Abs
		huma.Register(api, huma.Operation{
			OperationID:   route.ID,
			Method:        http.MethodGet,
			Path:          route.Path,
			Summary:       "Redirect a configurable number of times",
			Tags:          []string{"Redirects"},
			DefaultStatus: http.StatusFound,
			Responses:     redirectResponses(http.StatusFound),
		}, func(ctx context.Context, input *struct {
			RequestInfo
			N int `path:"n" minimum:"1" maximum:"20"`
		}) (*struct{ Status int }, error) {
			n := input.N - 1
			next := "/get"
			if n > 0 {
				prefix := "/relative-redirect"
				if abs {
					prefix = "/absolute-redirect"
				}
				next = fmt.Sprintf("%s/%d", prefix, n)
			}
			if abs {
				next = schemeHost(input.ctx) + next
			}
			input.ctx.SetHeader("Location", next)
			return &struct{ Status int }{Status: http.StatusFound}, nil
		})
	}

	huma.Register(api, huma.Operation{
		OperationID:   "get-redirect-to",
		Method:        http.MethodGet,
		Path:          "/redirect-to",
		Summary:       "Redirect to a supplied URL",
		Tags:          []string{"Redirects"},
		DefaultStatus: http.StatusFound,
		Responses:     redirectRangeResponses(),
	}, func(ctx context.Context, input *struct {
		RequestInfo
		URL        string `query:"url" required:"true"`
		StatusCode int    `query:"status_code" default:"302" minimum:"300" maximum:"399"`
	}) (*struct{ Status int }, error) {
		input.ctx.SetHeader("Location", input.URL)
		return &struct{ Status int }{Status: input.StatusCode}, nil
	})
}

func schemeHost(ctx huma.Context) string {
	scheme := "http"
	if proto := ctx.Header("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	return scheme + "://" + ctx.Host()
}

func (s *APIServer) RegisterETagFixture(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-etag",
		Method:      http.MethodGet,
		Path:        "/etag/{etag}",
		Summary:     "Exercise ETag conditional headers",
		Tags:        []string{"Caching"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
		ETag string `path:"etag"`
	}) (*struct{}, error) {
		etag := `"` + input.ETag + `"`
		input.ctx.SetHeader("ETag", etag)
		if input.ctx.Header("If-None-Match") == etag {
			input.ctx.SetStatus(http.StatusNotModified)
			return nil, nil
		}
		if match := input.ctx.Header("If-Match"); match != "" && match != etag {
			input.ctx.SetStatus(http.StatusPreconditionFailed)
			return nil, nil
		}
		input.ctx.SetHeader("Content-Type", "application/json")
		_ = json.NewEncoder(input.ctx.BodyWriter()).Encode(map[string]string{"etag": input.ETag})
		return nil, nil
	})
}

func (s *APIServer) RegisterCacheFixture(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-cache",
		Method:      http.MethodGet,
		Path:        "/cache",
		Summary:     "Return 304 when conditional request headers are present",
		Tags:        []string{"Caching"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*struct{}, error) {
		lastModified := time.Date(2022, 2, 1, 12, 34, 56, 0, time.UTC)
		etag := `"docs-cache"`
		input.ctx.SetHeader("ETag", etag)
		input.ctx.SetHeader("Last-Modified", lastModified.Format(http.TimeFormat))
		if input.ctx.Header("If-None-Match") != "" || input.ctx.Header("If-Modified-Since") != "" {
			input.ctx.SetStatus(http.StatusNotModified)
			return nil, nil
		}
		input.ctx.SetHeader("Content-Type", "application/json")
		_ = json.NewEncoder(input.ctx.BodyWriter()).Encode(map[string]string{"cache": "fresh"})
		return nil, nil
	})
}

type BinaryResponse struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

func (s *APIServer) RegisterBytes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-bytes",
		Method:      http.MethodGet,
		Path:        "/bytes/{n}",
		Summary:     "Return random bytes",
		Tags:        []string{"Binary"},
	}, func(ctx context.Context, input *struct {
		N    int `path:"n" minimum:"1" maximum:"1048576"`
		Seed int `query:"seed" doc:"Optional deterministic seed"`
	}) (*BinaryResponse, error) {
		data, err := makeBytes(input.N, input.Seed)
		if err != nil {
			return nil, err
		}
		return &BinaryResponse{ContentType: "application/octet-stream", Body: data}, nil
	})
}

func makeBytes(n, seed int) ([]byte, error) {
	buf := make([]byte, n)
	if seed != 0 {
		r := mathrand.New(mathrand.NewSource(int64(seed)))
		_, err := r.Read(buf)
		return buf, err
	}
	_, err := io.ReadFull(rand.Reader, buf)
	return buf, err
}

func (s *APIServer) RegisterStreamBytes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-stream-bytes",
		Method:      http.MethodGet,
		Path:        "/stream-bytes/{n}",
		Summary:     "Stream bytes in chunks",
		Tags:        []string{"Binary"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
		N         int `path:"n" minimum:"1" maximum:"1048576"`
		ChunkSize int `query:"chunk_size" default:"1024" minimum:"1" maximum:"65536"`
		Seed      int `query:"seed"`
	}) (*struct{}, error) {
		input.ctx.SetHeader("Content-Type", "application/octet-stream")
		data, err := makeBytes(input.N, input.Seed)
		if err != nil {
			return nil, err
		}
		for offset := 0; offset < len(data); offset += input.ChunkSize {
			end := offset + input.ChunkSize
			if end > len(data) {
				end = len(data)
			}
			if _, err := input.ctx.BodyWriter().Write(data[offset:end]); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
}

func (s *APIServer) RegisterRange(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-range",
		Method:      http.MethodGet,
		Path:        "/range/{n}",
		Summary:     "Return bytes with Range support",
		Tags:        []string{"Binary"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
		N int `path:"n" minimum:"1" maximum:"1048576"`
	}) (*BinaryResponse, error) {
		data, err := makeBytes(input.N, 1)
		if err != nil {
			return nil, err
		}
		if header := input.ctx.Header("Range"); strings.HasPrefix(header, "bytes=") {
			start, end, ok := parseByteRange(strings.TrimPrefix(header, "bytes="), len(data))
			if !ok {
				return nil, huma.NewError(http.StatusRequestedRangeNotSatisfiable, "invalid range")
			}
			input.ctx.SetStatus(http.StatusPartialContent)
			input.ctx.SetHeader("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(data)))
			data = data[start:end]
		}
		return &BinaryResponse{ContentType: "application/octet-stream", Body: data}, nil
	})
}

func parseByteRange(spec string, length int) (int, int, bool) {
	startS, endS, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, false
	}
	start, err := strconv.Atoi(startS)
	if err != nil || start < 0 || start >= length {
		return 0, 0, false
	}
	end := length
	if endS != "" {
		v, err := strconv.Atoi(endS)
		if err != nil || v < start {
			return 0, 0, false
		}
		end = int(math.Min(float64(v+1), float64(length)))
	}
	return start, end, true
}

func (s *APIServer) RegisterDrip(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-drip",
		Method:      http.MethodGet,
		Path:        "/drip",
		Summary:     "Slowly stream bytes",
		Tags:        []string{"Binary"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
		NumBytes int    `query:"numbytes" default:"10" minimum:"1" maximum:"10000"`
		Duration string `query:"duration" default:"2s"`
		Delay    string `query:"delay" default:"0s"`
		Code     int    `query:"code" default:"200" minimum:"100" maximum:"599"`
	}) (*struct{}, error) {
		delay, err := time.ParseDuration(input.Delay)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid delay")
		}
		if delay < 0 || delay > 10*time.Second {
			return nil, huma.Error400BadRequest("delay must be between 0s and 10s")
		}
		duration, err := time.ParseDuration(input.Duration)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid duration")
		}
		if duration < 0 || duration > 30*time.Second {
			return nil, huma.Error400BadRequest("duration must be between 0s and 30s")
		}
		input.ctx.SetStatus(input.Code)
		input.ctx.SetHeader("Content-Type", "application/octet-stream")
		if delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		interval := duration / time.Duration(input.NumBytes)
		if interval < 0 {
			interval = 0
		}
		for i := 0; i < input.NumBytes; i++ {
			if _, err := input.ctx.BodyWriter().Write([]byte{'*'}); err != nil {
				return nil, err
			}
			if interval > 0 && i < input.NumBytes-1 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(interval):
				}
			}
		}
		return nil, nil
	})
}

func (s *APIServer) RegisterCompressionFixtures(api huma.API) {
	for _, route := range []struct {
		Path     string
		ID       string
		Encoding string
	}{
		{"/gzip", "get-gzip", "gzip"},
		{"/deflate", "get-deflate", "deflate"},
		{"/brotli", "get-brotli", "br"},
	} {
		encoding := route.Encoding
		huma.Register(api, huma.Operation{
			OperationID: route.ID,
			Method:      http.MethodGet,
			Path:        route.Path,
			Summary:     "Return an explicitly compressed response",
			Tags:        []string{"Content"},
		}, func(ctx context.Context, input *struct{}) (*struct {
			ContentEncoding string `header:"Content-Encoding"`
			ContentType     string `header:"Content-Type"`
			Body            []byte
		}, error) {
			payload, _ := json.Marshal(map[string]any{"compressed": true, "encoding": encoding})
			compressed, err := compressBytes(encoding, payload)
			if err != nil {
				return nil, err
			}
			return &struct {
				ContentEncoding string `header:"Content-Encoding"`
				ContentType     string `header:"Content-Type"`
				Body            []byte
			}{ContentEncoding: encoding, ContentType: "application/json", Body: compressed}, nil
		})
	}
}

func compressBytes(encoding string, payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	switch encoding {
	case "gzip":
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(payload); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	case "deflate":
		w := zlib.NewWriter(&buf)
		if _, err := w.Write(payload); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	case "br":
		w := brotli.NewWriter(&buf)
		if _, err := w.Write(payload); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func (s *APIServer) RegisterProblem(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-problem",
		Method:      http.MethodGet,
		Path:        "/problem",
		Summary:     "Return an RFC 7807 problem document",
		Tags:        []string{"Content"},
	}, func(ctx context.Context, input *struct{}) (*struct {
		ContentType string `header:"Content-Type"`
		Status      int
		Body        map[string]any
	}, error) {
		return &struct {
			ContentType string `header:"Content-Type"`
			Status      int
			Body        map[string]any
		}{ContentType: "application/problem+json", Status: http.StatusBadRequest, Body: map[string]any{
			"type":   "https://api.rest.sh/problems/example",
			"title":  "Example problem",
			"status": http.StatusBadRequest,
			"detail": "This is a structured problem response for docs.",
		}}, nil
	})
}

func (s *APIServer) RegisterFormats(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-format",
		Method:      http.MethodGet,
		Path:        "/formats/{format}",
		Summary:     "Return data using a specific media type",
		Tags:        []string{"Content"},
	}, func(ctx context.Context, input *struct {
		Format string `path:"format" enum:"json,yaml,cbor,vendor-json"`
	}) (*struct {
		ContentType string `header:"Content-Type"`
		Body        []byte
	}, error) {
		contentType := map[string]string{
			"json":        "application/json",
			"yaml":        "application/yaml",
			"cbor":        "application/cbor",
			"vendor-json": "application/vnd.api+json",
		}[input.Format]
		data := map[string]any{"format": input.Format, "ok": true}
		var body []byte
		var err error
		switch input.Format {
		case "yaml":
			body, err = yaml.Marshal(data)
		case "cbor":
			body, err = cbor.Marshal(data)
		default:
			body, err = json.Marshal(data)
		}
		if err != nil {
			return nil, err
		}
		return &struct {
			ContentType string `header:"Content-Type"`
			Body        []byte
		}{ContentType: contentType, Body: body}, nil
	})
}

func (s *APIServer) RegisterAcceptImage(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-accept-image",
		Method:      http.MethodGet,
		Path:        "/image",
		Summary:     "Return an image based on the Accept header",
		Tags:        []string{"Images"},
	}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*GetImageResponse, error) {
		format, ok := acceptImageFormat(input.ctx.Header("Accept"))
		if !ok {
			return nil, huma.Error406NotAcceptable("no acceptable image format")
		}
		return imageResponse(format), nil
	})
}

func acceptImageFormat(accept string) (string, bool) {
	if accept == "" {
		return "png", true
	}

	formatByMediaType := map[string]string{
		"image/png":  "png",
		"image/jpeg": "jpeg",
		"image/webp": "webp",
		"image/gif":  "gif",
		"image/heic": "heic",
	}

	bestFormat := ""
	bestQ := 0.0
	for _, part := range strings.Split(accept, ",") {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}

		format := formatByMediaType[mediaType]
		if format == "" && (mediaType == "image/*" || mediaType == "*/*") {
			format = "png"
		}
		if format == "" {
			continue
		}

		q := 1.0
		if rawQ := params["q"]; rawQ != "" {
			parsed, err := strconv.ParseFloat(rawQ, 64)
			if err != nil {
				continue
			}
			q = parsed
		}
		if q <= 0 {
			continue
		}
		if q > bestQ {
			bestQ = q
			bestFormat = format
		}
	}
	if bestFormat != "" {
		return bestFormat, true
	}
	return "", false
}

func imageResponse(format string) *GetImageResponse {
	var body []byte
	switch format {
	case "jpeg":
		body = exampleJPEG
	case "webp":
		body = exampleWEBP
	case "gif":
		body = exampleGIF
	case "heic":
		body = exampleHeic
	default:
		format = "png"
		body = examplePNG
	}
	return &GetImageResponse{ContentType: "image/" + format, Body: body}
}

func (s *APIServer) RegisterUtilityFixtures(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "get-cookies", Method: http.MethodGet, Path: "/cookies", Summary: "Return request cookies", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*struct{ Body map[string]map[string]string }, error) {
		cookies := map[string]string{}
		for _, part := range strings.Split(input.ctx.Header("Cookie"), ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok && name != "" {
				cookies[name] = value
			}
		}
		return &struct{ Body map[string]map[string]string }{Body: map[string]map[string]string{"cookies": cookies}}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-cookies-set", Method: http.MethodGet, Path: "/cookies/set", Summary: "Set cookies from query parameters", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*struct{ Body map[string]map[string]string }, error) {
		values, _ := url.ParseQuery(input.ctx.URL().RawQuery)
		cookies := map[string]string{}
		for name, vals := range values {
			value := vals[len(vals)-1]
			input.ctx.AppendHeader("Set-Cookie", (&http.Cookie{Name: name, Value: value, Path: "/"}).String())
			cookies[name] = value
		}
		return &struct{ Body map[string]map[string]string }{Body: map[string]map[string]string{"cookies": cookies}}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-cookies-delete", Method: http.MethodGet, Path: "/cookies/delete", Summary: "Delete cookies named by query parameters", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*struct{ Body map[string][]string }, error) {
		values, _ := url.ParseQuery(input.ctx.URL().RawQuery)
		deleted := []string{}
		for name := range values {
			input.ctx.AppendHeader("Set-Cookie", (&http.Cookie{Name: name, MaxAge: -1, Path: "/"}).String())
			deleted = append(deleted, name)
		}
		return &struct{ Body map[string][]string }{Body: map[string][]string{"deleted": deleted}}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-uuid", Method: http.MethodGet, Path: "/uuid", Summary: "Return a UUID", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct{}) (*struct{ Body map[string]string }, error) {
		b := make([]byte, 16)
		rand.Read(b)
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		uuid := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
		return &struct{ Body map[string]string }{Body: map[string]string{"uuid": uuid}}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-ip", Method: http.MethodGet, Path: "/ip", Summary: "Return the requester IP", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct {
		RequestInfo
	}) (*struct{ Body map[string]string }, error) {
		origin := input.ctx.Header("X-Forwarded-For")
		if origin == "" {
			origin = input.ctx.Header("X-Real-IP")
		}
		if origin == "" {
			origin = "unknown"
		}
		return &struct{ Body map[string]string }{Body: map[string]string{"origin": origin}}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-html", Method: http.MethodGet, Path: "/html", Summary: "Return HTML", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct{}) (*struct {
		ContentType string `header:"Content-Type"`
		Body        []byte
	}, error) {
		return &struct {
			ContentType string `header:"Content-Type"`
			Body        []byte
		}{ContentType: "text/html; charset=utf-8", Body: []byte("<!doctype html><title>api.rest.sh</title><h1>Example HTML</h1>")}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-xml", Method: http.MethodGet, Path: "/xml", Summary: "Return XML", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct{}) (*struct {
		ContentType string `header:"Content-Type"`
		Body        []byte
	}, error) {
		type note struct {
			XMLName xml.Name `xml:"note"`
			Message string   `xml:"message"`
		}
		b, _ := xml.Marshal(note{Message: "Hello from api.rest.sh"})
		return &struct {
			ContentType string `header:"Content-Type"`
			Body        []byte
		}{ContentType: "application/xml", Body: b}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-base64-encode", Method: http.MethodGet, Path: "/base64/encode/{value}", Summary: "Base64-url encode a value", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct {
		Value string `path:"value"`
	}) (*struct{ Body map[string]string }, error) {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(input.Value))
		return &struct{ Body map[string]string }{Body: map[string]string{"encoded": encoded}}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-base64-decode", Method: http.MethodGet, Path: "/base64/decode/{value}", Summary: "Base64-url decode a value", Tags: []string{"Utilities"}}, func(ctx context.Context, input *struct {
		Value string `path:"value"`
	}) (*struct{ Body map[string]string }, error) {
		decoded, err := base64.RawURLEncoding.DecodeString(input.Value)
		if err != nil {
			decoded, err = base64.StdEncoding.DecodeString(input.Value)
		}
		if err != nil {
			return nil, huma.Error400BadRequest("invalid base64 value")
		}
		return &struct{ Body map[string]string }{Body: map[string]string{"decoded": string(decoded)}}, nil
	})
}
