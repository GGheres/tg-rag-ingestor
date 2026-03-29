package hh

import "time"

// --- Config ---

type Config struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	RedirectURI  string
	UserAgent    string // Required: "AppName/1.0 (email@example.com)"
	VacancyID    string
	OutputDir    string
	DryRun       bool
	ExportPDF    bool
	Concurrency  int
}

// --- OAuth2 ---

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// --- Negotiations API ---

// NegotiationsResponse is the top-level response from GET /negotiations
// ADAPT_TO_REAL_API_RESPONSE: the exact shape of collections may vary.
type NegotiationsResponse struct {
	Found       int                `json:"found"`
	Pages       int                `json:"pages"`
	PerPage     int                `json:"per_page"`
	Page        int                `json:"page"`
	Items       []NegotiationItem  `json:"items"`
	Collections []CollectionRef    `json:"collections"`
}

type CollectionRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Count int    `json:"count"`
}

// CollectionResponse is the response from fetching a specific collection URL.
// ADAPT_TO_REAL_API_RESPONSE: may have same structure as NegotiationsResponse.
type CollectionResponse struct {
	Found   int               `json:"found"`
	Pages   int               `json:"pages"`
	PerPage int               `json:"per_page"`
	Page    int               `json:"page"`
	Items   []NegotiationItem `json:"items"`
}

type NegotiationItem struct {
	ID        string           `json:"id"`
	State     IDName           `json:"state"`
	CreatedAt string           `json:"created_at"`
	UpdatedAt string           `json:"updated_at"`
	Resume    *ResumeShort     `json:"resume"`
	Vacancy   *VacancyRef      `json:"vacancy"`
}

type ResumeShort struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	AlternateURL string `json:"alternate_url"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	MiddleName   string `json:"middle_name"`
}

type VacancyRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type IDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// --- Resume (full) ---

type Resume struct {
	ID           string          `json:"id"`
	FirstName    string          `json:"first_name"`
	LastName     string          `json:"last_name"`
	MiddleName   string          `json:"middle_name"`
	Title        string          `json:"title"`
	Age          *int            `json:"age"`
	Area         *IDName         `json:"area"`
	Salary       *Salary         `json:"salary"`
	Experience   []Experience    `json:"experience"`
	Education    *Education      `json:"education"`
	SkillSet     []string        `json:"skill_set"`
	Skills       string          `json:"skills"`
	Contact      []Contact       `json:"contact"`
	Download     *DownloadLinks  `json:"download"`
	AlternateURL string          `json:"alternate_url"`
	UpdatedAt    string          `json:"updated_at"`
	CreatedAt    string          `json:"created_at"`
	Photo        *Photo          `json:"photo"`
	Gender       *IDName         `json:"gender"`
	BirthDate    string          `json:"birth_date"`
	Certificate  []Certificate   `json:"certificate"`
	Language     []Language      `json:"language"`
	Metro        *IDName         `json:"metro"`
	Citizenship  []IDName        `json:"citizenship"`
	WorkTicket   []IDName        `json:"work_ticket"`
	TravelTime   *IDName         `json:"travel_time"`
	Schedule     *IDName         `json:"schedule"`
	Employment   *IDName         `json:"employment"`
}

type Salary struct {
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type Experience struct {
	Company     string  `json:"company"`
	CompanyURL  string  `json:"company_url"`
	Area        *IDName `json:"area"`
	Position    string  `json:"position"`
	Start       string  `json:"start"`
	End         string  `json:"end"`
	Description string  `json:"description"`
	Industries  []IDName `json:"industries"`
}

type Education struct {
	Level      *IDName           `json:"level"`
	Primary    []EducationEntry  `json:"primary"`
	Additional []EducationEntry  `json:"additional"`
}

type EducationEntry struct {
	Name         string `json:"name"`
	Organization string `json:"organization"`
	Result       string `json:"result"`
	Year         int    `json:"year"`
}

type Contact struct {
	Type      IDName  `json:"type"`
	Value     any     `json:"value"`
	Preferred bool    `json:"preferred"`
	Comment   string  `json:"comment"`
}

type DownloadLinks struct {
	PDF *DownloadLink `json:"pdf"`
	RTF *DownloadLink `json:"rtf"`
}

type DownloadLink struct {
	URL string `json:"url"`
}

type Photo struct {
	Small  string `json:"small"`
	Medium string `json:"medium"`
}

type Certificate struct {
	Title        string `json:"title"`
	AchievedAt   string `json:"achieved_at"`
	Type         string `json:"type"`
	Owner        string `json:"owner"`
	URL          string `json:"url"`
	Organization string `json:"organization"`
}

type Language struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Level *IDName `json:"level"`
}

// --- HH API Error ---

type APIError struct {
	Errors []APIErrorDetail `json:"errors"`
}

type APIErrorDetail struct {
	Type    string `json:"type"`
	Value   string `json:"value"`
	Message string `json:"message"`
}

// --- Internal models ---

type CandidateResult struct {
	CandidateID       string `json:"candidate_id"`
	ResumeID          string `json:"resume_id"`
	FIO               string `json:"fio"`
	Status            string `json:"status"` // "ok", "error", "skipped", "partial"
	DownloadedOriginal bool  `json:"downloaded_original"`
	IncludedInCombined bool  `json:"included_in_combined"`
	ErrorMessage       string `json:"error_message,omitempty"`
	OriginalFile       string `json:"original_file,omitempty"`
}

type Manifest struct {
	VacancyID   string            `json:"vacancy_id"`
	CreatedAt   time.Time         `json:"created_at"`
	TotalFound  int               `json:"total_found"`
	Processed   int               `json:"processed"`
	Succeeded   int               `json:"succeeded"`
	Failed      int               `json:"failed"`
	DryRun      bool              `json:"dry_run"`
	Candidates  []CandidateResult `json:"candidates"`
}

// FIO returns full name from resume parts.
func FIO(firstName, lastName, middleName string) string {
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

// ResumeShort.FIO returns the full name from short resume.
func (r *ResumeShort) FIO() string {
	if r == nil {
		return "[ФИО НЕДОСТУПНО]"
	}
	return FIO(r.FirstName, r.LastName, r.MiddleName)
}

// Resume.FIO returns the full name from full resume.
func (r *Resume) FIO() string {
	return FIO(r.FirstName, r.LastName, r.MiddleName)
}
