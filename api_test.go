package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/autopatch"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v2"
)

func newTestAPI(t *testing.T) humatest.TestAPI {
	t.Helper()

	_, api := newTestHandler(t)
	return humatest.Wrap(t, api)
}

func newTestHandler(t *testing.T) (http.Handler, huma.API) {
	t.Helper()

	config := huma.DefaultConfig("Test API", "1.0.0")
	yamlFormat := huma.Format{
		Marshal: func(writer io.Writer, v any) error {
			return yaml.NewEncoder(writer).Encode(v)
		},
		Unmarshal: func(data []byte, v any) error {
			return yaml.Unmarshal(data, v)
		},
	}
	config.Formats["application/yaml"] = yamlFormat
	config.Formats["yaml"] = yamlFormat

	router := chi.NewMux()
	router.Use(ResponseGuard)
	router.Use(CORS)
	router.Use(ContentEncoding)
	api := humachi.New(router, config)

	server := APIServer{}
	huma.AutoRegister(api, &server)
	autopatch.AutoPatch(api)
	return router, api
}

func assertStatus(t *testing.T, resp *httptest.ResponseRecorder, status int) {
	t.Helper()
	if resp.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", resp.Code, status, resp.Body.String())
	}
}

func assertHeader(t *testing.T, resp *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := resp.Header().Get(name); got != want {
		t.Fatalf("%s = %q, want %q; body: %s", name, got, want, resp.Body.String())
	}
}

func decodeJSON(t *testing.T, resp *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &data); err != nil {
		t.Fatalf("invalid JSON body: %v\n%s", err, resp.Body.String())
	}
	return data
}

func TestETagHelpersAndConditionals(t *testing.T) {
	for name, tag := range map[string]string{
		"generated": quoteETag(genETagBytes([]byte("hello"))),
		"book":      quoteETag(Book{Title: "Example"}.Version()),
	} {
		if !strings.HasPrefix(tag, `"`) || !strings.HasSuffix(tag, `"`) {
			t.Fatalf("%s ETag is not quoted: %q", name, tag)
		}
	}

	api := newTestAPI(t)
	resp := api.Get("/books/letters-from-an-astrophysicist")
	assertStatus(t, resp, http.StatusOK)
	etag := resp.Header().Get("ETag")
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Fatalf("response ETag is not quoted: %q", etag)
	}

	resp = api.Get("/books/letters-from-an-astrophysicist", "If-None-Match: "+etag)
	assertStatus(t, resp, http.StatusNotModified)

	resp = api.Get("/etag/docs")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "ETag", `"docs"`)

	resp = api.Get("/etag/docs", `If-None-Match: "docs"`)
	assertStatus(t, resp, http.StatusNotModified)

	resp = api.Get("/cache", "If-Modified-Since: Tue, 01 Feb 2022 12:34:56 GMT")
	assertStatus(t, resp, http.StatusNotModified)
}

func TestCoreContentAndInspectionEndpoints(t *testing.T) {
	api := newTestAPI(t)

	resp := api.Get("/example")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "ETag", quoteETag(exampleEtag))
	if !strings.Contains(resp.Body.String(), "basics") {
		t.Fatalf("example response missing basics: %s", resp.Body.String())
	}

	resp = api.Get("/types")
	assertStatus(t, resp, http.StatusOK)
	body := decodeJSON(t, resp)
	if body["boolean"] != true || body["string"] != "Hello, world!" {
		t.Fatalf("unexpected types body: %#v", body)
	}

	resp = api.Put("/types?status=202", map[string]any{"hello": "world"})
	assertStatus(t, resp, http.StatusAccepted)
	body = decodeJSON(t, resp)
	if body["method"] != http.MethodPut || body["path"] != "/types" {
		t.Fatalf("unexpected echo body: %#v", body)
	}

	resp = api.Get("/cached/5?private=true")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "Cache-Control", "private, max-age=5")

	resp = api.Get("/status/418?retry-after=3&x-retry-in=soon")
	assertStatus(t, resp, http.StatusTeapot)
	assertHeader(t, resp, "Retry-After", "3")
	assertHeader(t, resp, "X-Retry-In", "soon")

	resp = api.Get("/headers", "X-Test-Header: yes")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "X-Test-Header") {
		t.Fatalf("headers response did not echo header: %s", resp.Body.String())
	}

	resp = api.Get("/user-agent", "User-Agent: apibin-test")
	assertStatus(t, resp, http.StatusOK)
	if decodeJSON(t, resp)["user-agent"] != "apibin-test" {
		t.Fatalf("unexpected user-agent body: %s", resp.Body.String())
	}

	resp = api.Get("/response-headers?X-Reply=yes")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "X-Reply", "yes")

	resp = api.Post("/post", map[string]any{"hello": "world"})
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), `"parsed":{"hello":"world"}`) {
		t.Fatalf("echo response missing raw body: %s", resp.Body.String())
	}
}

func TestImagesAndContentNegotiation(t *testing.T) {
	api := newTestAPI(t)

	resp := api.Get("/images?limit=1&search=dragon")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "Dragonfly macro") {
		t.Fatalf("filtered images response mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/images?cursor=abc123&format=png")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), `"format":"png"`) {
		t.Fatalf("cursor image response mismatch: %s", resp.Body.String())
	}

	for _, route := range []struct {
		path        string
		contentType string
	}{
		{"/images/jpeg", "image/jpeg"},
		{"/images/webp", "image/webp"},
		{"/images/gif", "image/gif"},
		{"/images/png", "image/png"},
		{"/images/heic", "image/heic"},
	} {
		resp = api.Get(route.path)
		assertStatus(t, resp, http.StatusOK)
		assertHeader(t, resp, "Content-Type", route.contentType)
		if resp.Body.Len() == 0 {
			t.Fatalf("%s returned empty image body", route.path)
		}
	}

	resp = api.Get("/image", "Accept: image/webp,image/png")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "Content-Type", "image/webp")

	resp = api.Get("/image", "Accept: image/png;q=0")
	assertStatus(t, resp, http.StatusNotAcceptable)
}

func TestBooksAndItemsCRUD(t *testing.T) {
	api := newTestAPI(t)

	resp := api.Get("/books")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "/books/sapiens") {
		t.Fatalf("books list missing sapiens: %s", resp.Body.String())
	}

	resp = api.Put("/books/test-book", Book{Title: "Test Book", Author: "Codex"})
	assertStatus(t, resp, http.StatusNoContent)

	resp = api.Get("/books/test-book")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "Test Book") {
		t.Fatalf("created book not returned: %s", resp.Body.String())
	}

	resp = api.Delete("/books/test-book")
	assertStatus(t, resp, http.StatusNoContent)

	resp = api.Get("/books/test-book")
	assertStatus(t, resp, http.StatusNotFound)

	resp = api.Get("/items")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "Alpha item") {
		t.Fatalf("items list missing alpha: %s", resp.Body.String())
	}

	resp = api.Post("/items", Item{ID: "test-item", Name: "Test Item", Enabled: true})
	assertStatus(t, resp, http.StatusOK)

	resp = api.Patch("/items/test-item", map[string]any{"name": "Patched", "enabled": false, "tags": []string{"x", "y"}})
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "Patched") {
		t.Fatalf("patched item response mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/items/test-item")
	assertStatus(t, resp, http.StatusOK)

	resp = api.Delete("/items/test-item")
	assertStatus(t, resp, http.StatusNoContent)

	resp = api.Get("/items/test-item")
	assertStatus(t, resp, http.StatusNotFound)
}

func TestAuthFormsUploadsAndUtilities(t *testing.T) {
	api := newTestAPI(t)

	resp := api.Post("/login", "Content-Type: application/x-www-form-urlencoded", strings.NewReader("username=alice&password=secret"))
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "docs-token-alice") {
		t.Fatalf("login response mismatch: %s", resp.Body.String())
	}

	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	if err := writer.WriteField("note", "hello"); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	resp = api.Post("/uploads", "Content-Type: "+writer.FormDataContentType(), &upload)
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "hello.txt") {
		t.Fatalf("upload response mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/auth/basic")
	assertStatus(t, resp, http.StatusUnauthorized)

	basic := base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	resp = api.Get("/auth/basic", "Authorization: Basic "+basic)
	assertStatus(t, resp, http.StatusOK)

	resp = api.Get("/auth/bearer", "Authorization: Bearer token-123")
	assertStatus(t, resp, http.StatusOK)

	resp = api.Get("/auth/api-key-header", "X-API-Key: key-123")
	assertStatus(t, resp, http.StatusOK)

	resp = api.Get("/auth/api-key-query?api_key=query-key")
	assertStatus(t, resp, http.StatusOK)

	resp = api.Get("/cookies", "Cookie: a=1; b=2")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), `"a":"1"`) {
		t.Fatalf("cookies response mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/cookies/set?session=abc")
	assertStatus(t, resp, http.StatusOK)
	if got := resp.Header().Values("Set-Cookie"); len(got) != 1 || !strings.Contains(got[0], "session=abc") {
		t.Fatalf("set-cookie headers = %#v", got)
	}

	resp = api.Get("/cookies/delete?session")
	assertStatus(t, resp, http.StatusOK)
	if got := resp.Header().Values("Set-Cookie"); len(got) != 1 || !strings.Contains(got[0], "Max-Age=0") {
		t.Fatalf("delete-cookie headers = %#v", got)
	}

	resp = api.Get("/uuid")
	assertStatus(t, resp, http.StatusOK)
	if len(decodeJSON(t, resp)["uuid"].(string)) != 36 {
		t.Fatalf("uuid response mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/ip", "X-Forwarded-For: 203.0.113.1")
	assertStatus(t, resp, http.StatusOK)
	if decodeJSON(t, resp)["origin"] != "203.0.113.1" {
		t.Fatalf("ip response mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/html")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "Content-Type", "text/html; charset=utf-8")

	resp = api.Get("/xml")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "Content-Type", "application/xml")

	resp = api.Get("/base64/encode/hello")
	assertStatus(t, resp, http.StatusOK)
	encoded := decodeJSON(t, resp)["encoded"].(string)

	resp = api.Get("/base64/decode/" + encoded)
	assertStatus(t, resp, http.StatusOK)
	if decodeJSON(t, resp)["decoded"] != "hello" {
		t.Fatalf("base64 decode response mismatch: %s", resp.Body.String())
	}
}

func TestReliabilityRedirectsBinaryAndContentFixtures(t *testing.T) {
	api := newTestAPI(t)

	resp := api.Get("/flaky?key=test&failures=1")
	assertStatus(t, resp, http.StatusServiceUnavailable)
	resp = api.Get("/flaky?key=test&failures=1")
	assertStatus(t, resp, http.StatusOK)

	resp = api.Get("/slow?delay=0s")
	assertStatus(t, resp, http.StatusOK)

	resp = api.Get("/redirect/2")
	assertStatus(t, resp, http.StatusFound)
	assertHeader(t, resp, "Location", "/relative-redirect/1")

	resp = api.Get("/absolute-redirect/1", "Host: api.example", "X-Forwarded-Proto: https")
	assertStatus(t, resp, http.StatusFound)
	assertHeader(t, resp, "Location", "https://api.example/get")

	resp = api.Get("/redirect-to?url=https%3A%2F%2Fexample.com%2Fnext&status_code=307")
	assertStatus(t, resp, http.StatusTemporaryRedirect)
	assertHeader(t, resp, "Location", "https://example.com/next")

	resp = api.Get("/bytes/16?seed=1")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "Content-Type", "application/octet-stream")
	if resp.Body.Len() != 16 {
		t.Fatalf("bytes length = %d, want 16", resp.Body.Len())
	}

	resp = api.Get("/stream-bytes/10?chunk_size=3&seed=1")
	assertStatus(t, resp, http.StatusOK)
	if resp.Body.Len() != 10 {
		t.Fatalf("stream bytes length = %d, want 10", resp.Body.Len())
	}

	resp = api.Get("/range/100", "Range: bytes=0-9")
	assertStatus(t, resp, http.StatusPartialContent)
	if resp.Body.Len() != 10 {
		t.Fatalf("range length = %d, want 10", resp.Body.Len())
	}

	resp = api.Get("/range/100", "Range: bytes=-10")
	assertStatus(t, resp, http.StatusRequestedRangeNotSatisfiable)

	resp = api.Get("/drip?numbytes=3&duration=0s")
	assertStatus(t, resp, http.StatusOK)
	if resp.Body.String() != "***" {
		t.Fatalf("drip body = %q, want ***", resp.Body.String())
	}

	for _, route := range []struct {
		path     string
		encoding string
		read     func([]byte) ([]byte, error)
	}{
		{"/gzip", "gzip", gunzip},
		{"/deflate", "deflate", inflate},
		{"/brotli", "br", unbrotli},
	} {
		resp = api.Get(route.path)
		assertStatus(t, resp, http.StatusOK)
		assertHeader(t, resp, "Content-Encoding", route.encoding)
		decoded, err := route.read(resp.Body.Bytes())
		if err != nil {
			t.Fatalf("%s decode failed: %v", route.path, err)
		}
		if !strings.Contains(string(decoded), `"compressed":true`) {
			t.Fatalf("%s decoded body mismatch: %s", route.path, decoded)
		}
	}

	resp = api.Get("/problem")
	assertStatus(t, resp, http.StatusBadRequest)
	assertHeader(t, resp, "Content-Type", "application/problem+json")

	for _, route := range []struct {
		path        string
		contentType string
	}{
		{"/formats/json", "application/json"},
		{"/formats/yaml", "application/yaml"},
		{"/formats/cbor", "application/cbor"},
		{"/formats/vendor-json", "application/vnd.api+json"},
	} {
		resp = api.Get(route.path)
		assertStatus(t, resp, http.StatusOK)
		assertHeader(t, resp, "Content-Type", route.contentType)
	}
}

func TestStreamingEndpoints(t *testing.T) {
	api := newTestAPI(t)

	resp := api.Get("/sse/metrics?count=1", "Accept: text/event-stream")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "Content-Type", "text/event-stream")
	if !strings.Contains(resp.Body.String(), "event: metrics") {
		t.Fatalf("metrics SSE response mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/events?count=1", "Accept: text/event-stream")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), "event: event") {
		t.Fatalf("events SSE response mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/logs?count=1")
	assertStatus(t, resp, http.StatusOK)
	assertHeader(t, resp, "Content-Type", "application/x-ndjson")
	if !strings.Contains(resp.Body.String(), `"type":"login"`) {
		t.Fatalf("logs response mismatch: %s", resp.Body.String())
	}
}

func TestErrorPathsAndFixtureVariants(t *testing.T) {
	api := newTestAPI(t)

	for _, route := range []struct {
		method string
		path   string
		args   []any
		status int
	}{
		{http.MethodPost, "/login", []any{"Content-Type: application/x-www-form-urlencoded", strings.NewReader("%")}, http.StatusBadRequest},
		{http.MethodPost, "/uploads", []any{"Content-Type: multipart/form-data", strings.NewReader("bad")}, http.StatusBadRequest},
		{http.MethodGet, "/auth/bearer", nil, http.StatusUnauthorized},
		{http.MethodGet, "/auth/api-key-header", nil, http.StatusUnauthorized},
		{http.MethodGet, "/auth/api-key-query", nil, http.StatusUnauthorized},
		{http.MethodGet, "/slow?delay=bad", nil, http.StatusBadRequest},
		{http.MethodGet, "/slow?delay=11s", nil, http.StatusBadRequest},
		{http.MethodGet, "/drip?duration=bad", nil, http.StatusBadRequest},
		{http.MethodGet, "/drip?duration=31s", nil, http.StatusBadRequest},
		{http.MethodGet, "/drip?delay=bad", nil, http.StatusBadRequest},
		{http.MethodGet, "/drip?delay=11s", nil, http.StatusBadRequest},
		{http.MethodGet, "/base64/decode/not-valid!", nil, http.StatusBadRequest},
		{http.MethodGet, "/formats/bad", nil, http.StatusUnprocessableEntity},
		{http.MethodGet, "/images/bad", nil, http.StatusUnprocessableEntity},
	} {
		resp := api.Do(route.method, route.path, route.args...)
		assertStatus(t, resp, route.status)
	}

	resp := api.Get("/cache")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), `"cache":"fresh"`) {
		t.Fatalf("cache body mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/etag/docs", `If-Match: "other"`)
	assertStatus(t, resp, http.StatusPreconditionFailed)

	resp = api.Get("/images?cursor=def456&per_page=1")
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), `"format":"heic"`) {
		t.Fatalf("last image page mismatch: %s", resp.Body.String())
	}

	resp = api.Get("/images?cursor=unknown")
	assertStatus(t, resp, http.StatusOK)
	if strings.TrimSpace(resp.Body.String()) != "[]" {
		t.Fatalf("unknown image cursor body = %s, want []", resp.Body.String())
	}

	resp = api.Get("/anything/some-path")
	assertStatus(t, resp, http.StatusOK)
	if decodeJSON(t, resp)["path"] != "/anything/some-path" {
		t.Fatalf("anything path mismatch: %s", resp.Body.String())
	}

	resp = api.Do(http.MethodHead, "/head")
	assertStatus(t, resp, http.StatusOK)

	resp = api.Do(http.MethodOptions, "/options")
	assertStatus(t, resp, http.StatusOK)

	resp = api.Get("/bytes/4")
	assertStatus(t, resp, http.StatusOK)
	if resp.Body.Len() != 4 {
		t.Fatalf("unseeded bytes length = %d, want 4", resp.Body.Len())
	}

	resp = api.Post("/items", Item{Name: "Generated ID", Enabled: true})
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), `"id":"item-`) {
		t.Fatalf("generated item ID response mismatch: %s", resp.Body.String())
	}

	resp = api.Patch("/items/alpha", map[string]any{"tags": []any{1, "kept"}})
	assertStatus(t, resp, http.StatusOK)
	if !strings.Contains(resp.Body.String(), `"tags":["kept"]`) {
		t.Fatalf("patch tags response mismatch: %s", resp.Body.String())
	}
}

func TestOpenAPIAndHelperCoverage(t *testing.T) {
	api := newTestAPI(t)
	paths := api.OpenAPI().Paths

	if paths["/status/{code}"].Get.Responses["418"] == nil {
		t.Fatal("OpenAPI should document dynamic /status/{code} responses")
	}
	if paths["/redirect-to"].Get.Responses["307"] == nil || paths["/redirect-to"].Get.Responses["399"] == nil {
		t.Fatal("OpenAPI should document redirect-to status range")
	}
	if paths["/redirect/{n}"].Get.Responses["302"].Headers["Location"] == nil {
		t.Fatal("OpenAPI should document redirect Location header")
	}
	anythingPath := paths["/anything/{path}"].Get
	foundAnythingPathParam := false
	for _, param := range anythingPath.Parameters {
		if param.Name == "path" && param.In == "path" && param.Required {
			foundAnythingPathParam = true
			break
		}
	}
	if !foundAnythingPathParam {
		t.Fatal("OpenAPI should document the /anything/{path} path parameter")
	}

	for _, tt := range []struct {
		spec   string
		length int
		start  int
		end    int
		ok     bool
	}{
		{"0-9", 100, 0, 10, true},
		{"10-", 100, 10, 100, true},
		{"-10", 100, 0, 0, false},
		{"200-201", 100, 0, 0, false},
	} {
		start, end, ok := parseByteRange(tt.spec, tt.length)
		if start != tt.start || end != tt.end || ok != tt.ok {
			t.Fatalf("parseByteRange(%q, %d) = %d, %d, %v; want %d, %d, %v", tt.spec, tt.length, start, end, ok, tt.start, tt.end, tt.ok)
		}
	}

	for _, tt := range []struct {
		header string
		user   string
		pass   string
		ok     bool
	}{
		{"", "", "", false},
		{"Basic bad", "", "", false},
		{"Basic " + base64.StdEncoding.EncodeToString([]byte("u:p")), "u", "p", true},
	} {
		user, pass, ok := parseBasic(tt.header)
		if user != tt.user || pass != tt.pass || ok != tt.ok {
			t.Fatalf("parseBasic(%q) = %q, %q, %v; want %q, %q, %v", tt.header, user, pass, ok, tt.user, tt.pass, tt.ok)
		}
	}

	left, err := makeBytes(8, 42)
	if err != nil {
		t.Fatal(err)
	}
	right, err := makeBytes(8, 42)
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != string(right) {
		t.Fatal("seeded makeBytes should be deterministic")
	}

	sim := newMetricSimulator()
	first := sim.next().Region
	second := sim.next().Region
	if first == second {
		t.Fatalf("metric simulator should rotate regions, got %q twice", first)
	}
}

func TestAcceptImageFormat(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		format string
		ok     bool
	}{
		{"empty defaults to png", "", "png", true},
		{"client order breaks ties", "image/webp,image/png", "webp", true},
		{"q value wins", "image/png;q=0.4,image/webp;q=0.9", "webp", true},
		{"q zero is rejected", "image/png;q=0", "", false},
		{"specific beats wildcard", "image/*;q=0.8,image/heic;q=0.9", "heic", true},
		{"unsupported is rejected", "application/json", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, ok := acceptImageFormat(tt.accept)
			if format != tt.format || ok != tt.ok {
				t.Fatalf("acceptImageFormat(%q) = %q, %v; want %q, %v", tt.accept, format, ok, tt.format, tt.ok)
			}
		})
	}
}

func TestContentEncodingMiddleware(t *testing.T) {
	payload := strings.Repeat("hello ", 400)
	handler := ContentEncoding(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(payload))
	}))

	for _, tt := range []struct {
		acceptEncoding  string
		contentEncoding string
		read            func([]byte) ([]byte, error)
	}{
		{"gzip", "gzip", gunzip},
		{"br, gzip", "br", unbrotli},
	} {
		req, _ := http.NewRequest(http.MethodGet, "/large", nil)
		req.Header.Set("Accept-Encoding", tt.acceptEncoding)
		resp := &testResponseWriter{header: http.Header{}}
		handler.ServeHTTP(resp, req)

		if got := resp.header.Get("Content-Encoding"); got != tt.contentEncoding {
			t.Fatalf("Content-Encoding = %q, want %q", got, tt.contentEncoding)
		}
		decoded, err := tt.read(resp.body.Bytes())
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if string(decoded) != payload {
			t.Fatal("decoded payload mismatch")
		}
	}
}

func TestContentEncodingMiddlewareStatusAndSmallBody(t *testing.T) {
	handler := ContentEncoding(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("small"))
	}))

	req, _ := http.NewRequest(http.MethodGet, "/small", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp := &testResponseWriter{header: http.Header{}}
	handler.ServeHTTP(resp, req)

	if resp.status != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusCreated)
	}
	if got := resp.header.Get("Content-Encoding"); got != "" {
		t.Fatalf("small body Content-Encoding = %q, want empty", got)
	}
	if resp.body.String() != "small" {
		t.Fatalf("small body = %q", resp.body.String())
	}
}

func TestResponseGuardDiscardsNoBodyResponseWrites(t *testing.T) {
	handler := ResponseGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
		if _, err := w.Write([]byte(`{"error":"not modified"}`)); err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))

	resp := &strictResponseWriter{header: http.Header{}, method: http.MethodGet}
	req, _ := http.NewRequest(http.MethodGet, "/books/test", nil)
	handler.ServeHTTP(resp, req)

	if resp.status != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", resp.status, http.StatusNotModified)
	}
	if resp.body.Len() != 0 {
		t.Fatalf("body length = %d, want 0", resp.body.Len())
	}
	if resp.duplicateHeaders != 0 {
		t.Fatalf("duplicate WriteHeader calls = %d, want 0", resp.duplicateHeaders)
	}
}

func TestConditionalNotModifiedDoesNotWriteBody(t *testing.T) {
	handler, _ := newTestHandler(t)

	first := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/books/letters-from-an-astrophysicist", nil)
	handler.ServeHTTP(first, req)
	assertStatus(t, first, http.StatusOK)

	resp := &strictResponseWriter{header: http.Header{}, method: http.MethodGet}
	req, _ = http.NewRequest(http.MethodGet, "/books/letters-from-an-astrophysicist", nil)
	req.Header.Set("If-None-Match", first.Header().Get("ETag"))

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("conditional 304 panicked: %v", recovered)
		}
	}()
	handler.ServeHTTP(resp, req)

	if resp.status != http.StatusNotModified {
		t.Fatalf("status = %d, want %d; body: %s", resp.status, http.StatusNotModified, resp.body.String())
	}
	if resp.body.Len() != 0 {
		t.Fatalf("304 body length = %d, want 0: %s", resp.body.Len(), resp.body.String())
	}
	if resp.duplicateHeaders != 0 {
		t.Fatalf("duplicate WriteHeader calls = %d, want 0", resp.duplicateHeaders)
	}
}

func TestHeadDoesNotWriteBody(t *testing.T) {
	handler, _ := newTestHandler(t)

	resp := &strictResponseWriter{header: http.Header{}, method: http.MethodHead}
	req, _ := http.NewRequest(http.MethodHead, "/head", nil)

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("HEAD response panicked: %v", recovered)
		}
	}()
	handler.ServeHTTP(resp, req)

	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", resp.status, http.StatusOK, resp.body.String())
	}
	if resp.body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0: %s", resp.body.Len(), resp.body.String())
	}
	if resp.duplicateHeaders != 0 {
		t.Fatalf("duplicate WriteHeader calls = %d, want 0", resp.duplicateHeaders)
	}
}

func TestRangePartialContentDoesNotDoubleWriteHeader(t *testing.T) {
	handler, _ := newTestHandler(t)

	resp := &strictResponseWriter{header: http.Header{}, method: http.MethodGet}
	req, _ := http.NewRequest(http.MethodGet, "/range/100", nil)
	req.Header.Set("Range", "bytes=0-9")
	handler.ServeHTTP(resp, req)

	if resp.status != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d; body: %s", resp.status, http.StatusPartialContent, resp.body.String())
	}
	if resp.body.Len() != 10 {
		t.Fatalf("range body length = %d, want 10", resp.body.Len())
	}
	if resp.duplicateHeaders != 0 {
		t.Fatalf("duplicate WriteHeader calls = %d, want 0", resp.duplicateHeaders)
	}
}

func TestCORSMiddlewareActualRequest(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(http.StatusAccepted)
	}))

	req, _ := http.NewRequest(http.MethodGet, "/books", nil)
	req.Header.Set("Origin", "https://docs.rest.sh")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	assertStatus(t, resp, http.StatusAccepted)
	assertHeader(t, resp, "Access-Control-Allow-Origin", "*")
	assertHeader(t, resp, "Access-Control-Expose-Headers", "*")
	assertHeader(t, resp, "ETag", `"abc"`)
}

func TestCORSMiddlewarePreflight(t *testing.T) {
	called := false
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest(http.MethodOptions, "/items", nil)
	req.Header.Set("Origin", "https://docs.rest.sh")
	req.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	req.Header.Set("Access-Control-Request-Headers", "content-type, x-api-key")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	assertStatus(t, resp, http.StatusNoContent)
	if called {
		t.Fatal("preflight request reached the next handler")
	}
	assertHeader(t, resp, "Access-Control-Allow-Origin", "*")
	assertHeader(t, resp, "Access-Control-Allow-Methods", corsAllowMethods)
	assertHeader(t, resp, "Access-Control-Allow-Headers", "content-type, x-api-key")
	assertHeader(t, resp, "Access-Control-Allow-Private-Network", "true")
	assertHeader(t, resp, "Access-Control-Max-Age", "86400")
}

type testResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *testResponseWriter) Header() http.Header {
	return w.header
}

func (w *testResponseWriter) Write(data []byte) (int, error) {
	return w.body.Write(data)
}

func (w *testResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
}

type strictResponseWriter struct {
	header           http.Header
	body             bytes.Buffer
	method           string
	status           int
	wrote            bool
	duplicateHeaders int
}

func (w *strictResponseWriter) Header() http.Header {
	return w.header
}

func (w *strictResponseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if !bodyAllowed(w.method, w.status) {
		return 0, http.ErrBodyNotAllowed
	}
	return w.body.Write(data)
}

func (w *strictResponseWriter) WriteHeader(statusCode int) {
	if w.wrote {
		w.duplicateHeaders++
		return
	}
	w.status = statusCode
	w.wrote = true
}

func gunzip(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func inflate(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func unbrotli(data []byte) ([]byte, error) {
	return io.ReadAll(brotli.NewReader(bytes.NewReader(data)))
}

func TestStatusTextFallback(t *testing.T) {
	if got := statusText(299); got != "Status 299" {
		t.Fatalf("statusText fallback = %q", got)
	}
}
