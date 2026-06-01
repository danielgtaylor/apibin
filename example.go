package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type Resume struct {
	Basics    Basics      `json:"basics" doc:"Core profile and contact information"`
	Work      []Work      `json:"work" minItems:"1" doc:"Professional work history"`
	Volunteer []Volunteer `json:"volunteer,omitempty" doc:"Volunteer experience"`
	Education []Education `json:"education,omitempty" doc:"Education history"`
	Skills    []Skill     `json:"skills,omitempty" doc:"Skills and keywords"`
	Languages []Language  `json:"languages,omitempty" doc:"Spoken languages"`
}

type Basics struct {
	Name     string    `json:"name" minLength:"1" example:"Thomas Davis" doc:"Full display name"`
	Label    string    `json:"label,omitempty" minLength:"1" example:"Web Developer" doc:"Short professional headline"`
	Email    string    `json:"email,omitempty" format:"email" example:"thomas@gmail.com" doc:"Contact email address"`
	Image    string    `json:"image,omitempty" format:"uri" doc:"URL (as per RFC 3986) to an image in JPEG or PNG format" example:"https://example.com/avatar.jpg"`
	Phone    string    `json:"phone,omitempty" doc:"Phone numbers are stored as strings so use any format you like, e.g. 712-117-2923"`
	URL      string    `json:"url,omitempty" format:"uri" doc:"URL (as per RFC 3986) to your website, e.g. personal homepage"`
	Summary  string    `json:"summary,omitempty" doc:"Write a short 2-3 sentence biography about yourself"`
	Location Location  `json:"location,omitempty" doc:"Primary location"`
	Profiles []Profile `json:"profiles,omitempty" maxItems:"10" doc:"Online social or professional profiles"`
	Updated  time.Time `json:"updated" doc:"Time when the resume data was last updated"`
	Verified bool      `json:"verified" doc:"Whether this example profile has been verified" example:"true"`
}

type Location struct {
	Address     string `json:"address,omitempty" doc:"To add multiple address lines, use \n. For example, 1234 Glücklichkeit Straße\nHinterhaus 5. Etage li."`
	PostalCode  string `json:"postalCode,omitempty" example:"94105" doc:"Postal or ZIP code"`
	City        string `json:"city,omitempty" example:"San Francisco" doc:"City or locality"`
	CountryCode string `json:"countryCode,omitempty" minLength:"2" maxLength:"2" doc:"Code as per ISO-3166-1 ALPHA-2, e.g. US, AU, IN" example:"US"`
	Region      string `json:"region,omitempty" doc:"The general region where you live. Can be a US state, or a province, for instance."`
}

type Profile struct {
	Network  string `json:"network,omitempty" minLength:"1" example:"Twitter" doc:"Profile network or service name"`
	Username string `json:"username,omitempty" minLength:"1" example:"neutralthoughts" doc:"Username on the profile network"`
	URL      string `json:"url,omitempty" format:"uri" example:"http://twitter.example.com/neutralthoughts"`
}

type Work struct {
	Name        string     `json:"name" minLength:"1" example:"Facebook" doc:"Employer or organization name"`
	Location    string     `json:"location,omitempty" example:"Menlo Park, CA"`
	Description string     `json:"description,omitempty" example:"Social Media Company"`
	Position    string     `json:"position" minLength:"1" example:"Software Engineer" doc:"Job title or role"`
	URL         string     `json:"url,omitempty" format:"uri" example:"https://facebook.com"`
	StartDate   *time.Time `json:"startDate,omitempty" doc:"Role start date"`
	EndDate     *time.Time `json:"endDate,omitempty" doc:"Role end date, omitted for current roles"`
	Summary     string     `json:"summary,omitempty" doc:"Give an overview of your responsibilities at the company"`
	Highlights  []string   `json:"highlights,omitempty" maxItems:"10" doc:"Specify multiple accomplishments"`
}

type Volunteer struct {
	Organization string     `json:"organization,omitempty" example:"Code for America" doc:"Volunteer organization name"`
	Position     string     `json:"position,omitempty" example:"Mentor" doc:"Volunteer role"`
	URL          string     `json:"url,omitempty" format:"uri" example:"https://www.codeforamerica.org"`
	StartDate    *time.Time `json:"startDate,omitempty" doc:"Volunteer role start date"`
	EndDate      *time.Time `json:"endDate,omitempty" doc:"Volunteer role end date"`
	Summary      string     `json:"summary,omitempty" doc:"Overview of the volunteer work"`
	Highlights   []string   `json:"highlights,omitempty" maxItems:"10" doc:"Notable volunteer accomplishments"`
}

type Education struct {
	Institution string     `json:"institution,omitempty" example:"University of Example" doc:"School or institution name"`
	Area        string     `json:"area,omitempty" example:"Computer Science" doc:"Field of study"`
	StudyType   string     `json:"studyType,omitempty" enum:"High School,Bachelor,Master,Doctorate,Certificate" example:"Bachelor" doc:"Type of study or credential"`
	StartDate   *time.Time `json:"startDate,omitempty" doc:"Education start date"`
	EndDate     *time.Time `json:"endDate,omitempty" doc:"Education end date"`
	Gpa         string     `json:"gpa,omitempty" example:"3.8" doc:"Grade point average or equivalent"`
	Courses     []string   `json:"courses,omitempty" maxItems:"20" doc:"Relevant courses"`
}

type Skill struct {
	Name     string   `json:"name,omitempty" example:"Web Development" doc:"Skill category name"`
	Level    string   `json:"level,omitempty" enum:"Beginner,Intermediate,Advanced,Expert" example:"Advanced" doc:"Proficiency level"`
	Keywords []string `json:"keywords,omitempty" minItems:"1" maxItems:"20" doc:"Specific technologies or practices"`
}

type Language struct {
	Language string `json:"language,omitempty" example:"English" doc:"Language name"`
	Fluency  string `json:"fluency,omitempty" enum:"Elementary,Limited working,Professional working,Full professional,Native or bilingual" example:"Native or bilingual" doc:"Language fluency"`
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

type ExampleResponse struct {
	ETag string `header:"ETag"`
	Body Resume
}

func (s *APIServer) RegisterExample(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-example",
		Method:      http.MethodGet,
		Path:        "/example",
		Description: "Example large structured data response",
		Tags:        []string{"Example"},
	}, func(ctx context.Context, i *struct{}) (*ExampleResponse, error) {
		return &ExampleResponse{
			ETag: quoteETag(exampleEtag),
			Body: example,
		}, nil
	})
}
