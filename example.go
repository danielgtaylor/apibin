package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma"
)

// type Date time.Time

// func (d *Date) UnmarshalJSON(b []byte) error {
// 	parsed, _ := time.Parse("2006-01-02", strings.Trim(string(b), `"`))
// 	*d = Date(parsed)
// 	return nil
// }

// func (d Date) MarshalJSON() ([]byte, error) {
// 	if time.Time(d).IsZero() {
// 		return []byte{}, nil
// 	}
// 	return []byte(`"` + time.Time(d).Format("2006-01-02") + `"`), nil
// }

// func (d *Date) MarshalYAML() (interface{}, error) {
// 	return time.Time(*d), nil
// }

// func (d Date) MarshalCBOR() ([]byte, error) {
// 	opts := cbor.CanonicalEncOptions()
// 	opts.Time = cbor.TimeRFC3339Nano
// 	opts.TimeTag = cbor.EncTagRequired
// 	mode, _ := opts.EncMode()
// 	return mode.Marshal(time.Time(d))
// }

type Resume struct {
	Basics    Basics      `json:"basics"`
	Work      []Work      `json:"work"`
	Volunteer []Volunteer `json:"volunteer,omitempty"`
	Education []Education `json:"education,omitempty"`
	Skills    []Skill     `json:"skills,omitempty"`
	Languages []Language  `json:"languages,omitempty"`
}

type Basics struct {
	Name     string    `json:"name"`
	Label    string    `json:"label,omitempty" example:"Web Developer"`
	Email    string    `json:"email,omitempty" example:"thomas@gmail.com"`
	Image    string    `json:"image,omitempty" doc:"URL (as per RFC 3986) to a image in JPEG or PNG format"`
	Phone    string    `json:"phone,omitempty" doc:"Phone numbers are stored as strings so use any format you like, e.g. 712-117-2923"`
	URL      string    `json:"url,omitempty" format:"uri" doc:"URL (as per RFC 3986) to your website, e.g. personal homepage"`
	Summary  string    `json:"summary,omitempty" doc:"Write a short 2-3 sentence biography about yourself"`
	Location Location  `json:"location,omitempty"`
	Profiles []Profile `json:"profiles,omitempty"`
	Updated  time.Time `json:"updated"`
	Verified bool      `json:"verified"`
}

type Location struct {
	Address     string `json:"address,omitempty" doc:"To add multiple address lines, use \n. For example, 1234 Glücklichkeit Straße\nHinterhaus 5. Etage li."`
	PostalCode  string `json:"postalCode,omitempty"`
	City        string `json:"city,omitempty"`
	CountryCode string `json:"countryCode,omitempty" doc:"code as per ISO-3166-1 ALPHA-2, e.g. US, AU, IN"`
	Region      string `json:"region,omitempty" doc:"The general region where you live. Can be a US state, or a province, for instance."`
}

type Profile struct {
	Network  string `json:"network,omitempty" example:"Twitter"`
	Username string `json:"username,omitempty" example:"neutralthoughts"`
	URL      string `json:"url,omitempty" format:"uri" example:"http://twitter.example.com/neutralthoughts"`
}

type Work struct {
	Name        string     `json:"name" example:"Facebook"`
	Location    string     `json:"location,omitempty" example:"Menlo Park, CA"`
	Description string     `json:"description,omitempty" example:"Social Media Company"`
	Position    string     `json:"position" example:"Software Engineer"`
	URL         string     `json:"url,omitempty" format:"uri" example:"https://facebook.com"`
	StartDate   *time.Time `json:"startDate,omitempty"`
	EndDate     *time.Time `json:"endDate,omitempty"`
	Summary     string     `json:"summary,omitempty" doc:"Give an overview of your responsibilities at the company"`
	Highlights  []string   `json:"highlights,omitempty" doc:"Specify multiple accomplishments"`
}

type Volunteer struct {
	Organization string     `json:"organization,omitempty"`
	Position     string     `json:"position,omitempty"`
	URL          string     `json:"url,omitempty"`
	StartDate    *time.Time `json:"startDate,omitempty"`
	EndDate      *time.Time `json:"endDate,omitempty"`
	Summary      string     `json:"summary,omitempty"`
	Highlights   []string   `json:"highlights,omitempty"`
}

type Education struct {
	Institution string     `json:"institution,omitempty"`
	Area        string     `json:"area,omitempty"`
	StudyType   string     `json:"studyType,omitempty"`
	StartDate   *time.Time `json:"startDate,omitempty"`
	EndDate     *time.Time `json:"endDate,omitempty"`
	Gpa         string     `json:"gpa,omitempty"`
	Courses     []string   `json:"courses,omitempty"`
}

type Skill struct {
	Name     string   `json:"name,omitempty"`
	Level    string   `json:"level,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
}

type Language struct {
	Language string `json:"language,omitempty"`
	Fluency  string `json:"fluency,omitempty"`
}

var example Resume

//go:embed example.json
var exampleBytes []byte
var exampleEtag = genETagBytes(exampleBytes)

//go:embed images/dragonfly.jpg
var exampleJPEG []byte

//go:embed images/origami.webp
var exampleWEBP []byte

//go:embed images/soup.gif
var exampleGIF []byte

//go:embed images/station.png
var examplePNG []byte

//go:embed images/glass.heic
var exampleHeic []byte

func init() {
	if err := json.Unmarshal(exampleBytes, &example); err != nil {
		panic(err)
	}
}

func exampleHandler(ctx huma.Context) {
	ctx.Header().Set("ETag", `"`+exampleEtag+`"`)
	ctx.WriteModel(http.StatusOK, example)
}
