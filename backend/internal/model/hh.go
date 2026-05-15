package model

import (
	"context"
	"time"
)

const (
	DefaultHHBaseURL      = "https://api.hh.ru"
	DefaultHHTokenURL     = "https://api.hh.ru/token"
	DefaultHHAuthURL      = "https://hh.ru/oauth/authorize"
	DefaultHHRPS          = 5
	DefaultHHRetryMax     = 4
	DefaultHHConcurrency  = 4
	DefaultHHPerPage      = 50
	DefaultHHOutputDir    = "./data/hh_extractions"
	DefaultHTTPTimeoutSec = 30
)

type HHConfig struct {
	ClientID         string
	ClientSecret     string
	AccessToken      string
	RefreshToken     string
	RedirectURI      string
	UserAgent        string
	ManagerAccountID string

	BaseURL  string
	TokenURL string
	AuthURL  string

	OutputDir            string
	RequestTimeoutSec    int
	RateLimitPerSecond   int
	RetryMax             int
	RetryBaseBackoffMS   int
	RetryMaxBackoffMS    int
	Concurrency          int
	SaveOriginalsDefault bool
	OnTokenRefresh       func(context.Context, TokenResponse) error
}

func (c HHConfig) WithDefaults() HHConfig {
	if c.BaseURL == "" {
		c.BaseURL = DefaultHHBaseURL
	}
	if c.TokenURL == "" {
		c.TokenURL = DefaultHHTokenURL
	}
	if c.AuthURL == "" {
		c.AuthURL = DefaultHHAuthURL
	}
	if c.OutputDir == "" {
		c.OutputDir = DefaultHHOutputDir
	}
	if c.RequestTimeoutSec <= 0 {
		c.RequestTimeoutSec = DefaultHTTPTimeoutSec
	}
	if c.RateLimitPerSecond <= 0 {
		c.RateLimitPerSecond = DefaultHHRPS
	}
	if c.RetryMax <= 0 {
		c.RetryMax = DefaultHHRetryMax
	}
	if c.RetryBaseBackoffMS <= 0 {
		c.RetryBaseBackoffMS = 400
	}
	if c.RetryMaxBackoffMS <= 0 {
		c.RetryMaxBackoffMS = 15000
	}
	if c.Concurrency <= 0 {
		c.Concurrency = DefaultHHConcurrency
	}
	return c
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

type HHErrorResponse struct {
	Errors []HHErrorDetail `json:"errors"`
}

type HHErrorDetail struct {
	Type    string `json:"type"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

func (e *HHErrorResponse) HasType(t string) bool {
	if e == nil {
		return false
	}
	for _, item := range e.Errors {
		if item.Type == t {
			return true
		}
	}
	return false
}

type NegotiationCollection struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Count int    `json:"count"`
}

type NegotiationCandidate struct {
	CandidateID   string
	NegotiationID string
	ResumeID      string
	ResumeAPIURL  string
	FIO           string
	UpdatedAt     string
	CoverLetter   string
	MessagesURL   string
	ChatID        string
}

type Resume struct {
	ID           string         `json:"id"`
	FirstName    string         `json:"first_name"`
	LastName     string         `json:"last_name"`
	MiddleName   string         `json:"middle_name"`
	Title        string         `json:"title"`
	Area         *NamedRef      `json:"area"`
	Age          *int           `json:"age"`
	Salary       *Salary        `json:"salary"`
	Contact      []Contact      `json:"contact"`
	SkillSet     []string       `json:"skill_set"`
	Skills       string         `json:"skills"`
	Experience   []Experience   `json:"experience"`
	Education    *Education     `json:"education"`
	Download     *DownloadLinks `json:"download"`
	AlternateURL string         `json:"alternate_url"`
	UpdatedAt    string         `json:"updated_at"`
	CreatedAt    string         `json:"created_at"`
	CoverLetter  string         `json:"-"`
}

type NamedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Salary struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type Contact struct {
	Type      NamedRef `json:"type"`
	Value     any      `json:"value"`
	Preferred bool     `json:"preferred"`
	Comment   string   `json:"comment"`
}

type Experience struct {
	Company     string `json:"company"`
	Position    string `json:"position"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Description string `json:"description"`
}

type Education struct {
	Level      *NamedRef         `json:"level"`
	Primary    []EducationRecord `json:"primary"`
	Additional []EducationRecord `json:"additional"`
}

type EducationRecord struct {
	Name         string `json:"name"`
	Organization string `json:"organization"`
	Result       string `json:"result"`
	Year         int    `json:"year"`
}

type DownloadLinks struct {
	PDF *DownloadLink `json:"pdf"`
	RTF *DownloadLink `json:"rtf"`
}

type DownloadLink struct {
	URL string `json:"url"`
}

func (r Resume) FIO() string {
	return BuildFIO(r.FirstName, r.LastName, r.MiddleName)
}

func BuildFIO(firstName, lastName, middleName string) string {
	name := lastName
	if firstName != "" {
		if name != "" {
			name += " "
		}
		name += firstName
	}
	if middleName != "" {
		if name != "" {
			name += " "
		}
		name += middleName
	}
	if name == "" {
		return "[ФИО НЕДОСТУПНО]"
	}
	return name
}

type CandidateManifest struct {
	CandidateID        string `json:"candidate_id"`
	ResumeID           string `json:"resume_id"`
	FIO                string `json:"fio"`
	Status             string `json:"status"`
	DownloadedOriginal bool   `json:"downloaded_original"`
	IncludedInCombined bool   `json:"included_in_combined"`
	ErrorMessage       string `json:"error_message,omitempty"`
	OriginalFile       string `json:"original_file,omitempty"`
}

type RunManifest struct {
	VacancyID  string              `json:"vacancy_id"`
	Provider   string              `json:"provider"`
	CreatedAt  time.Time           `json:"created_at"`
	TotalFound int                 `json:"total_found"`
	Processed  int                 `json:"processed"`
	Succeeded  int                 `json:"succeeded"`
	Failed     int                 `json:"failed"`
	DryRun     bool                `json:"dry_run"`
	Candidates []CandidateManifest `json:"candidates"`
}

type ExtractionRequest struct {
	VacancyID        string
	ManagerAccountID string
	DateFrom         string
	DateTo           string
	DryRun           bool
	ExportPDF        bool
	SaveOriginals    bool
	SaveAsMarkdown   bool
	CoverLetterOnly  bool
	OutputDir        string
}

type ExtractionResult struct {
	ExtractionID string   `json:"extraction_id"`
	VacancyID    string   `json:"vacancy_id"`
	Status       string   `json:"status"`
	OutputDir    string   `json:"output_dir"`
	Files        []string `json:"files,omitempty"`
}

type HHManagerAccount struct {
	ID           string `json:"id"`
	EmployerID   string `json:"employer_id"`
	EmployerName string `json:"employer_name"`
	IsCurrent    bool   `json:"is_current"`
	IsPrimary    bool   `json:"is_primary"`
}

type HHVacancyListItem struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	ManagerAccountID string `json:"manager_account_id"`
	EmployerID       string `json:"employer_id"`
	EmployerName     string `json:"employer_name"`
	AlternateURL     string `json:"alternate_url,omitempty"`
	PublishedAt      string `json:"published_at,omitempty"`
	ArchivedAt       string `json:"archived_at,omitempty"`
	Responses        int    `json:"responses,omitempty"`
	Views            int    `json:"views,omitempty"`
}

type HHVacancyCatalog struct {
	Accounts         []HHManagerAccount  `json:"accounts"`
	CurrentAccountID string              `json:"current_account_id"`
	Vacancies        []HHVacancyListItem `json:"vacancies"`
}
