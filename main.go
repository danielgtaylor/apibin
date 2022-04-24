package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma"
	"github.com/danielgtaylor/huma/cli"
	"github.com/danielgtaylor/huma/negotiation"
	"github.com/danielgtaylor/huma/responses"
)

var docs = strings.Replace(`Example API
[![HUMA Powered](https://img.shields.io/badge/Powered%20By-Huma-ff5f87)](https://huma.rocks/) [![Works With Restish](https://img.shields.io/badge/Works%20With-Restish-ff5f87)](https://rest.sh/) [![GitHub](https://img.shields.io/github/license/danielgtaylor/apibin)](https://github.com/danielgtaylor/apibin)

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

func main() {
	app := cli.NewRouter(docs, "1.0.0")

	// Serve the docs at the root of the API if hit by a browser.
	app.Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			if r.Method == http.MethodGet && r.URL.Path == "/" && strings.Contains(r.Header.Get("User-Agent"), "Mozilla") && negotiation.SelectQValue(r.Header.Get("Accept"), []string{"text/html", "application/json", "application/cbor"}) == "text/html" {
				huma.SwaggerUIHandler(app.Router).ServeHTTP(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	// Echo back what the user sends in.
	echo := app.Resource("/")
	echo.Tags("Echo")
	echoResp := huma.
		NewResponse(0, "Default response").
		Headers("Content-Type", "Cache-Control", "Link", "Last-Modified", "Etag", "Vary").
		Model(EchoModel{})
	echo.Get("get-echo", "Echo GET", echoResp).Run(echoHandler)
	echo.Post("post-echo", "Echo POST", echoResp).Run(echoHandler)
	echo.Put("put-echo", "Echo PUT", echoResp).Run(echoHandler)
	echo.Patch("patch-echo", "Echo PATCH", echoResp).Run(echoHandler)
	echo.Delete("delete-echo", "Echo DELETE", echoResp).Run(echoHandler)

	// Example data
	types := app.Resource("/types")
	types.Tags("Example")
	types.Get("get-types-example", "Example structured data types",
		responses.OK().Headers("Last-Modified", "Etag").Model(TypesModel{}),
	).Run(func(ctx huma.Context) {
		ctx.WriteModel(http.StatusOK, TypesModel{
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
		})
	})
	types.Put("put-types-example", "Example write for edits", echoResp).Run(echoHandler)

	example := app.Resource("/example")
	example.Tags("Example")
	example.Get("get-example", "Example large structured data response",
		responses.OK().Headers("Last-Modified", "Etag").Model(Resume{}),
	).Run(exampleHandler)

	// Cached response
	cached := app.Resource("/cached/{seconds}")
	cached.Tags("Cached")
	cached.Get("get-cached", "Cached response example",
		responses.OK().Headers("Cache-Control").Model(CachedModel{}),
	).Run(func(ctx huma.Context, input struct {
		Seconds int  `path:"seconds" minimum:"1" maximum:"300" doc:"Number of seconds to cache"`
		Private bool `query:"private" doc:"Disabled shared caches like CDNs"`
	}) {
		header := fmt.Sprintf("max-age=%d", input.Seconds)
		if input.Private {
			header = "private, " + header
		}
		ctx.Header().Set("Cache-Control", header)
		ctx.WriteModel(http.StatusOK, CachedModel{
			Generated: time.Now(),
			Until:     time.Now().Add(time.Duration(input.Seconds) * time.Second),
		})
	})

	// Example paginated response (via rel=next)
	images := app.Resource("/images")
	images.Tags("Images")
	images.Get("list-images", "Paginated list of all images",
		responses.OK().Headers("Link").Model([]ImageItem{}),
	).Run(func(ctx huma.Context, input struct {
		Cursor string `query:"cursor" doc:"Pagination cursor"`
	}) {
		// Return different pages based on the cursor.
		if input.Cursor == "" {
			ctx.Header().Set("Link", "</images?cursor=abc123>; rel=\"next\"")
			ctx.WriteModel(http.StatusOK, []ImageItem{
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
			})
		} else if input.Cursor == "abc123" {
			ctx.Header().Set("Link", "</images?cursor=def456>; rel=\"next\"")
			ctx.WriteModel(http.StatusOK, []ImageItem{
				{
					Name:   "Andy Warhol mural in Miami",
					Format: "gif",
					Self:   "/images/gif",
				},
				{
					Name:   "Station in Prague",
					Format: "png",
					Self:   "/images/webp",
				},
			})
		} else if input.Cursor == "def456" {
			ctx.WriteModel(http.StatusOK, []ImageItem{
				{
					Name:   "Chihuly glass in boats",
					Format: "heic",
					Self:   "/images/heic",
				},
			})

		} else {
			ctx.WriteModel(http.StatusOK, []ImageItem{})
		}
	})

	// Image binary response
	images.SubResource("/{type}").Get("get-image", "Get an image",
		responses.OK().Headers("Content-Type"),
	).Run(func(ctx huma.Context, input struct {
		Type string `path:"type" enum:"jpeg,webp,png,gif,heic"`
	}) {
		ctx.Header().Set("Content-Type", "image/"+input.Type)

		switch input.Type {
		case "jpeg":
			ctx.Write(exampleJPEG)
		case "webp":
			ctx.Write(exampleWEBP)
		case "png":
			ctx.Write(examplePNG)
		case "gif":
			ctx.Write(exampleGIF)
		case "heic":
			ctx.Write(exampleHeic)
		}
	})

	app.Run()
}
