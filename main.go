package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/danielgtaylor/huma/v2/negotiation"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

var docs = strings.Replace(`[![HUMA Powered](https://img.shields.io/badge/Powered%20By-Huma-ff5f87)](https://huma.rocks/) [![Works With Restish](https://img.shields.io/badge/Works%20With-Restish-ff5f87)](https://rest.sh/) [![GitHub](https://img.shields.io/github/license/danielgtaylor/apibin)](https://github.com/danielgtaylor/apibin)

Provides a simple, modern, example API that offers these features:

- HTTP, HTTPS (TLS), and [HTTP/2](https://http2.github.io/)
- [OpenAPI 3](https://www.openapis.org/) & [JSON Schema](https://json-schema.org/)
- Client-driven content negotiation
	- ^gzip^ & ^br^ content encoding for large responses
	- ^JSON^, ^YAML^, & ^CBOR^ formats
- Conditional requests via ^ETag^ or ^LastModified^
- Echo back request info to help debugging
- Cached responses to test proxy & client-side caching
- Example structured data
	- Shows off ^object^, ^array^, ^string^, ^date^, ^binary^, ^integer^, ^number^, ^boolean^, etc.
- A sample CRUD API for books & reviews with simulated server-side updates
- Image responses ^JPEG^, ^WEBP^, ^GIF^, ^PNG^ & ^HEIC^
- [RFC7807](https://datatracker.ietf.org/doc/html/rfc7807) structured errors

This project is open source: [https://github.com/danielgtaylor/apibin](https://github.com/danielgtaylor/apibin)

You can run it localy via Docker:

^^^sh
# Start the server
$ docker run -p 8888:8888 ghcr.io/danielgtaylor/apibin:latest

# Make a request
$ restish :8888/types
^^^
`, "^", "`", -1)

type CachedModel struct {
	Generated time.Time `json:"generated" doc:"Time when this response was generated"`
	Until     time.Time `json:"until" doc:"When the cache will be invalidated"`
}

type ImageItem struct {
	Name   string `json:"name"`
	Format string `json:"format" enum:"jpeg,webp,gif,png,heic"`
	Self   string `json:"self" format:"uri-reference"`
}

type SubObject struct {
	Binary     []byte    `json:"binary"`
	BinaryLong []byte    `json:"binary_long"`
	Date       time.Time `json:"date"`
	DateTime   time.Time `json:"date_time"`
	URL        string    `json:"url" format:"uri"`
}

type TypesModel struct {
	Nullable *struct{} `json:"nullable"`
	Boolean  bool      `json:"boolean"`
	Integer  int64     `json:"integer"`
	Number   float64   `json:"number"`
	String   string    `json:"string"`
	Tags     []string  `json:"tags"`
	Object   SubObject `json:"object"`
}

type TypesResponse struct {
	Body TypesModel
}

type APIServer struct{}

func (s *APIServer) RegisterTypes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-types-example",
		Method:      http.MethodGet,
		Path:        "/types",
		Description: "Example structured data types",
		Tags:        []string{"Types"},
	}, func(ctx context.Context, i *struct{}) (*TypesResponse, error) {
		return &TypesResponse{
			Body: TypesModel{
				Boolean: true,
				Integer: 42,
				Number:  123.45,
				String:  "Hello, world!",
				Tags:    []string{"example", "short"},
				Object: SubObject{
					Binary:     []byte{222, 173, 192, 222},
					BinaryLong: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
					Date:       time.Now().UTC().Truncate(24 * time.Hour),
					DateTime:   time.Now().UTC(),
					URL:        "https://rest.sh/",
				},
			},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "put-types-example",
		Method:      http.MethodPut,
		Path:        "/types",
		Description: "Example write for edits",
		Tags:        []string{"Types"},
	}, s.echoHandler)
}

type CachedResponse struct {
	CacheControl string `header:"Cache-Control"`
	Body         CachedModel
}

func (s *APIServer) RegisterCached(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-cached",
		Method:      http.MethodGet,
		Path:        "/cached/{seconds}",
		Description: "Cached response example",
		Tags:        []string{"Caching"},
	}, func(ctx context.Context, input *struct {
		Seconds int  `path:"seconds" minimum:"1" maximum:"300" doc:"Number of seconds to cache"`
		Private bool `query:"private" doc:"Disabled shared caches like CDNs"`
	}) (*CachedResponse, error) {
		header := fmt.Sprintf("max-age=%d", input.Seconds)
		if input.Private {
			header = "private, " + header
		}
		return &CachedResponse{
			CacheControl: header,
			Body: CachedModel{
				Generated: time.Now(),
				Until:     time.Now().Add(time.Duration(input.Seconds) * time.Second),
			},
		}, nil
	})
}

type StatusResponse struct {
	Status     int
	RetryAfter string `header:"Retry-After"`
	XRetryIn   string `header:"X-Retry-In"`
}

func (s *APIServer) RegisterStatus(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-status",
		Method:      http.MethodGet,
		Path:        "/status/{code}",
		Description: "Status code example",
		Tags:        []string{"Status"},
	}, func(ctx context.Context, input *struct {
		Code       int    `path:"code" minimum:"100" maximum:"599" doc:"Status code to return"`
		RetryAfter string `query:"retry-after" doc:"Retry-After header value"`
		XRetryIn   string `query:"x-retry-in" doc:"X-Retry-In header value"`
	}) (*StatusResponse, error) {
		return &StatusResponse{
			Status:     input.Code,
			RetryAfter: input.RetryAfter,
			XRetryIn:   input.XRetryIn,
		}, nil
	})
}

type ListImagesResponse struct {
	Link string `header:"Link"`
	Body []ImageItem
}

func (s *APIServer) RegisterListImages(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-images",
		Method:      http.MethodGet,
		Path:        "/images",
		Description: "List available images",
		Tags:        []string{"Images"},
	}, func(ctx context.Context, input *struct {
		Cursor string `query:"cursor" doc:"Pagination cursor"`
	}) (*ListImagesResponse, error) {
		// Return different pages based on the cursor.
		resp := &ListImagesResponse{}
		if input.Cursor == "" {
			resp.Link = "</images?cursor=abc123>; rel=\"next\""
			resp.Body = []ImageItem{
				{
					Name:   "Dragonfly macro",
					Format: "jpeg",
					Self:   "/images/jpeg",
				},
				{
					Name:   "Origami under blacklight",
					Format: "webp",
					Self:   "/images/webp",
				},
			}
		} else if input.Cursor == "abc123" {
			resp.Link = "</images?cursor=def456>; rel=\"next\""
			resp.Body = []ImageItem{{
				Name:   "Andy Warhol mural in Miami",
				Format: "gif",
				Self:   "/images/gif",
			},
				{
					Name:   "Station in Prague",
					Format: "png",
					Self:   "/images/png",
				},
			}
		} else if input.Cursor == "def456" {
			resp.Body = []ImageItem{
				{
					Name:   "Chihuly glass in boats",
					Format: "heic",
					Self:   "/images/heic",
				},
			}
		}
		return resp, nil
	})
}

type GetImageResponse struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

func (s *APIServer) RegisterGetImage(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-image",
		Method:      http.MethodGet,
		Path:        "/images/{type}",
		Description: "Get an image",
		Tags:        []string{"Images"},
	}, func(ctx context.Context, i *struct {
		Type string `path:"type" enum:"jpeg,webp,png,gif,heic"`
	}) (*GetImageResponse, error) {
		var body []byte
		switch i.Type {
		case "jpeg":
			body = exampleJPEG
		case "webp":
			body = exampleWEBP
		case "png":
			body = examplePNG
		case "gif":
			body = exampleGIF
		case "heic":
			body = exampleHeic
		}
		return &GetImageResponse{
			ContentType: "image/" + i.Type,
			Body:        body,
		}, nil
	})
}

type Options struct {
	Host string `doc:"Host to listen on"`
	Port int    `default:"8888" doc:"Port to listen on"`
}

func main() {
	var api huma.API

	cli := huma.NewCLI(func(hooks huma.Hooks, opts *Options) {
		router := chi.NewMux()

		router.Use(middleware.Recoverer)
		router.Use(ContentEncoding)

		router.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/" && strings.Contains(r.Header.Get("User-Agent"), "Mozilla") && negotiation.SelectQValueFast(r.Header.Get("Accept"), []string{"text/html", "application/json", "application/cbor"}) == "text/html" {
					r.URL.Path = "/docs"
				}

				next.ServeHTTP(w, r)
			})
		})

		config := huma.DefaultConfig("Example API", "1.0.0")
		config.Info.Description = docs
		config.Servers = []*huma.Server{
			{URL: "https://api.rest.sh"},
		}

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

		api = humachi.New(router, config)

		server := APIServer{}
		huma.AutoRegister(api, &server)

		httpServer := http.Server{
			Addr:              fmt.Sprintf("%s:%d", opts.Host, opts.Port),
			ReadTimeout:       5 * time.Second,
			ReadHeaderTimeout: 1 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       30 * time.Second,
			Handler:           router,
		}

		hooks.OnStart(func() {
			fmt.Println("Starting server on http://" + httpServer.Addr)
			httpServer.ListenAndServe()
		})

		hooks.OnStop(func() {
			httpServer.Shutdown(context.Background())
		})
	})

	cli.Root().AddCommand(&cobra.Command{
		Use:   "openapi",
		Short: "Generate OpenAPI spec",
		Run: func(cmd *cobra.Command, args []string) {
			b, _ := json.MarshalIndent(api.OpenAPI(), "", "  ")
			fmt.Println(string(b))
		},
	})

	cli.Run()
}
