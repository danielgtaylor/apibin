package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/conditional"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

// Rating is a point-in-time rating for a book.
type Rating struct {
	Date   time.Time `json:"date"`
	Rating float64   `json:"rating"`
}

// Book tracks metadata information about a book and its ratings.
type Book struct {
	Title         string    `json:"title"`
	Author        string    `json:"author,omitempty"`
	Published     time.Time `json:"published,omitempty"`
	Ratings       int       `json:"ratings,omitempty"`
	RatingAverage float64   `json:"rating_average,omitempty"`
	RecentRatings []Rating  `json:"recent_ratings,omitempty"`
	modified      time.Time `json:"-"`
}

// Version computes a base64 hash of the book's public fields.
func (b Book) Version() string {
	data, _ := json.Marshal(b)

	h := fnv.New128()
	h.Write(data)
	result := h.Sum(nil)

	return base64.StdEncoding.EncodeToString(result)
}

// BookSummary provides a link and version for the books list response.
type BookSummary struct {
	URL      string    `json:"url"`
	Version  string    `json:"version"`
	Modified time.Time `json:"modified"`
}

// booksMu controls access to the map/slice. This is necessary because maps &
// slices are not goroutine-safe and each incoming request may use a separate
// goroutine to handle it. The slice is used to provide a consistent list and
// deletion order since Go maps are unordered. On initial load from the
// unordered JSON an alphanumeric sort is used.
var booksMu = sync.RWMutex{}
var books map[string]*Book
var booksOrder = []string{}

//go:embed books.json
var booksBytes []byte

func init() {
	go func() {
		// Reset the books DB every 10 minutes to get a consistent state.
		for {
			// Load from the stored bytes
			var loaded map[string]*Book
			if err := json.Unmarshal(booksBytes, &loaded); err != nil {
				panic(err)
			}
			for _, b := range loaded {
				// Set the last-modified time for conditional update headers to service
				// startup. This will rev on restarts but is good enough for demonstration
				// purposes.
				b.modified = time.Now()
			}

			booksMu.Lock()
			books = loaded
			booksOrder = maps.Keys(books)
			sort.Strings(booksOrder)
			booksMu.Unlock()

			time.Sleep(10 * time.Minute)
		}
	}()

	// Update the Sapiens latest review date every 10 seconds to generate
	// frequent simulated server-side updates.
	go func() {
		for {
			time.Sleep(10 * time.Second)
			booksMu.Lock()
			b := books["sapiens"]
			if b != nil {
				b.modified = time.Now()
				b.RecentRatings = []Rating{
					{Date: time.Now(), Rating: 4.6},
				}
				books["sapiens"] = b
			}
			booksMu.Unlock()
		}
	}()
}

type ListResponse struct {
	Body []BookSummary
}

func (s *APIServer) RegisterListBooks(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-books",
		Method:      http.MethodGet,
		Path:        "/books",
		Tags:        []string{"Books"},
	}, func(ctx context.Context, input *struct{}) (*ListResponse, error) {
		booksMu.RLock()
		defer booksMu.RUnlock()

		// Return a list of summaries with metadata about each book.
		l := make([]BookSummary, 0, len(books))
		for _, k := range booksOrder {
			b := books[k]
			l = append(l, BookSummary{
				URL:      "/books/" + k,
				Version:  b.Version(),
				Modified: b.modified,
			})
		}

		return &ListResponse{Body: l}, nil
	})
}

type GetBookResponse struct {
	CacheControl string    `header:"Cache-Control"`
	ETag         string    `header:"Etag"`
	LastModified time.Time `header:"Last-Modified"`
	Vary         string    `header:"Vary"`

	Body *Book
}

func (s *APIServer) RegisterGetBook(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-book",
		Method:      http.MethodGet,
		Path:        "/books/{book-id}",
		Tags:        []string{"Books"},
	}, func(ctx context.Context, input *struct {
		conditional.Params
		ID string `path:"book-id"`
	}) (*GetBookResponse, error) {
		booksMu.RLock()
		defer booksMu.RUnlock()

		b := books[input.ID]
		if b == nil {
			return nil, huma.Error404NotFound(input.ID + " not found")
		}

		if err := input.PreconditionFailed(b.Version(), b.modified); err != nil {
			return nil, err
		}

		resp := &GetBookResponse{
			CacheControl: "max-age:0",
			ETag:         b.Version(),
			LastModified: b.modified,
			Vary:         "Accept, Accept-Encoding, Origin",
			Body:         b,
		}
		return resp, nil
	})
}

func (s *APIServer) RegisterPutBook(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "put-book",
		Method:      http.MethodPut,
		Path:        "/books/{book-id}",
		Tags:        []string{"Books"},
	}, func(ctx context.Context, input *struct {
		conditional.Params
		ID   string `path:"book-id"`
		Body Book
	}) (*struct{}, error) {
		booksMu.Lock()
		defer booksMu.Unlock()

		if input.HasConditionalParams() {
			existing := books[input.ID]
			if existing != nil {
				if err := input.PreconditionFailed(existing.Version(), existing.modified); err != nil {
					return nil, err
				}
			}
		}

		if books[input.ID] == nil {
			booksOrder = append(booksOrder, input.ID)
		}
		input.Body.modified = time.Now()
		books[input.ID] = &input.Body

		// Limit the total number of books by deleting the oldest first. These will
		// get reset periodically by the goroutine in `init()` above.
		for len(books) > 20 {
			delete(books, booksOrder[0])
			booksOrder = booksOrder[1:]
		}

		return nil, nil
	})
}

func (s *APIServer) RegisterDeleteBook(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "delete-book",
		Method:      http.MethodDelete,
		Path:        "/books/{book-id}",
		Tags:        []string{"Books"},
	}, func(ctx context.Context, input *struct {
		conditional.Params
		ID string `path:"book-id"`
	}) (*struct{}, error) {
		booksMu.Lock()
		defer booksMu.Unlock()

		if input.HasConditionalParams() {
			existing := books[input.ID]
			if existing != nil {
				if err := input.PreconditionFailed(existing.Version(), existing.modified); err != nil {
					return nil, err
				}
			}
		}

		// Remove the book from both the map and the slice.
		delete(books, input.ID)
		if idx := slices.Index(booksOrder, input.ID); idx > -1 {
			booksOrder = slices.Delete(booksOrder, idx, idx+1)
		}

		return nil, nil
	})
}
