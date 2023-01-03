package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"hash/fnv"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/danielgtaylor/huma"
	"github.com/danielgtaylor/huma/conditional"
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

func listBooks(ctx huma.Context) {
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

	ctx.WriteModel(http.StatusOK, l)
}

func getBook(ctx huma.Context, input struct {
	ID string `path:"book-id"`
	conditional.Params
}) {
	booksMu.RLock()
	defer booksMu.RUnlock()

	b := books[input.ID]
	if b == nil {
		ctx.WriteError(http.StatusNotFound, input.ID+" not found")
		return
	}

	if input.PreconditionFailed(ctx, b.Version(), b.modified) {
		return
	}

	ctx.Header().Set("Cache-Control", "max-age:0")
	ctx.Header().Set("Etag", b.Version())
	ctx.Header().Set("Last-Modified", b.modified.Format(http.TimeFormat))
	ctx.Header().Set("Vary", "Accept, Accept-Encoding, Origin")
	ctx.WriteModel(http.StatusOK, b)
}

func putBook(ctx huma.Context, input struct {
	ID   string `path:"book-id"`
	Body *Book
	conditional.Params
}) {
	booksMu.Lock()
	defer booksMu.Unlock()

	if input.HasConditionalParams() {
		existing := books[input.ID]
		if existing != nil && input.PreconditionFailed(ctx, existing.Version(), existing.modified) {
			return
		}
	}

	if books[input.ID] == nil {
		booksOrder = append(booksOrder, input.ID)
	}
	input.Body.modified = time.Now()
	books[input.ID] = input.Body

	// Limit the total number of books by deleting the oldest first. These will
	// get reset periodically by the goroutine in `init()` above.
	for len(books) > 20 {
		delete(books, booksOrder[0])
		booksOrder = booksOrder[1:]
	}

	ctx.WriteHeader(http.StatusNoContent)
}

func deleteBook(ctx huma.Context, input struct {
	ID string `path:"book-id"`
	conditional.Params
}) {
	booksMu.Lock()
	defer booksMu.Unlock()

	if input.HasConditionalParams() {
		existing := books[input.ID]
		if existing != nil && input.PreconditionFailed(ctx, existing.Version(), existing.modified) {
			return
		}
	}

	// Remove the book from both the map and the slice.
	delete(books, input.ID)
	if idx := slices.Index(booksOrder, input.ID); idx > -1 {
		booksOrder = slices.Delete(booksOrder, idx, idx+1)
	}

	ctx.WriteHeader(http.StatusNoContent)
}
