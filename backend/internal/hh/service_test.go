package hh

import (
	"strings"
	"testing"
)

func TestFIO(t *testing.T) {
	tests := []struct {
		first, last, middle string
		want                string
	}{
		{"Иван", "Иванов", "Иванович", "Иванов Иван Иванович"},
		{"Мария", "Петрова", "", "Петрова Мария"},
		{"", "Сидоров", "", "Сидоров"},
		{"", "", "", "[ФИО НЕДОСТУПНО]"},
		{"John", "", "", "John"},
	}

	for _, tt := range tests {
		got := FIO(tt.first, tt.last, tt.middle)
		if got != tt.want {
			t.Errorf("FIO(%q, %q, %q) = %q, want %q", tt.first, tt.last, tt.middle, got, tt.want)
		}
	}
}

func TestExtractContactValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"email string", "test@example.com", "test@example.com"},
		{"phone map with formatted", map[string]any{
			"country":   "7",
			"city":      "495",
			"number":    "1234567",
			"formatted": "+7 (495) 123-45-67",
		}, "+7 (495) 123-45-67"},
		{"phone map without formatted", map[string]any{
			"country": "7",
			"city":    "495",
			"number":  "1234567",
		}, "+7 (495) 1234567"},
		{"nil value", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Contact{Value: tt.value}
			got := ExtractContactValue(c)
			if got != tt.want {
				t.Errorf("ExtractContactValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractAllContacts(t *testing.T) {
	contacts := []Contact{
		{Type: IDName{ID: "email", Name: "Email"}, Value: "test@example.com"},
		{Type: IDName{ID: "cell", Name: "Мобильный телефон"}, Value: "+7 999 123 45 67", Comment: "WhatsApp"},
		{Type: IDName{ID: "empty", Name: "Empty"}, Value: nil},
	}

	result := ExtractAllContacts(contacts)
	if len(result) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(result))
	}
	if result[0] != "Email: test@example.com" {
		t.Errorf("unexpected first contact: %s", result[0])
	}
	if !strings.Contains(result[1], "WhatsApp") {
		t.Errorf("expected WhatsApp comment, got: %s", result[1])
	}
}

func TestWriteResumeBlock(t *testing.T) {
	r := &Resume{
		ID:        "abc123",
		FirstName: "Иван",
		LastName:  "Иванов",
		Title:     "Go Developer",
		Age:       intPtr(30),
		Area:      &IDName{ID: "1", Name: "Москва"},
		Salary:    &Salary{Amount: 300000, Currency: "RUR"},
		SkillSet:  []string{"Go", "PostgreSQL", "Docker"},
		Experience: []Experience{
			{
				Company:  "Acme Corp",
				Position: "Senior Developer",
				Start:    "2020-01",
				End:      "2024-01",
			},
		},
		Education: &Education{
			Level: &IDName{ID: "higher", Name: "Высшее"},
			Primary: []EducationEntry{
				{Name: "МГУ", Result: "Информатика", Year: 2018},
			},
		},
		AlternateURL: "https://hh.ru/resume/abc123",
		UpdatedAt:    "2024-01-15T10:00:00+0300",
	}

	var sb strings.Builder
	writeResumeBlock(&sb, r, "12345")
	result := sb.String()

	checks := []string{
		"КАНДИДАТ: Иванов Иван",
		"RESUME ID: abc123",
		"VACANCY ID: 12345",
		"Желаемая должность: Go Developer",
		"Локация: Москва",
		"Возраст: 30",
		"Зарплата: 300000 RUR",
		"Go, PostgreSQL, Docker",
		"Acme Corp",
		"Senior Developer",
		"МГУ",
	}

	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("expected %q in output, not found.\nFull output:\n%s", check, result)
		}
	}
}

func TestWriteResumeBlock_MissingFields(t *testing.T) {
	r := &Resume{
		ID:        "empty123",
		FirstName: "",
		LastName:  "",
	}

	var sb strings.Builder
	writeResumeBlock(&sb, r, "99999")
	result := sb.String()

	if !strings.Contains(result, "[ФИО НЕДОСТУПНО]") {
		t.Error("expected [ФИО НЕДОСТУПНО] for empty name")
	}
	if !strings.Contains(result, "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]") {
		t.Error("expected [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО] for missing fields")
	}
}

func TestFormatSalary(t *testing.T) {
	if got := formatSalary(nil); got != "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]" {
		t.Errorf("nil salary: got %q", got)
	}
	if got := formatSalary(&Salary{Amount: 0}); got != "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]" {
		t.Errorf("zero salary: got %q", got)
	}
	if got := formatSalary(&Salary{Amount: 250000, Currency: "RUR"}); got != "250000 RUR" {
		t.Errorf("got %q, want 250000 RUR", got)
	}
}

func TestGenerateCombinedText(t *testing.T) {
	resumes := []*Resume{
		{
			ID:        "r1",
			FirstName: "Анна",
			LastName:  "Петрова",
			Title:     "Backend Developer",
		},
		{
			ID:        "r2",
			FirstName: "Борис",
			LastName:  "Сидоров",
			Title:     "DevOps Engineer",
		},
	}

	dir := t.TempDir()
	path, err := GenerateCombinedText(resumes, "v123", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
}

func TestGenerateCombinedHTML(t *testing.T) {
	resumes := []*Resume{
		{
			ID:        "r1",
			FirstName: "Анна",
			LastName:  "Петрова",
			Title:     "Backend Developer",
			SkillSet:  []string{"Go", "Python"},
		},
	}

	dir := t.TempDir()
	path, err := GenerateCombinedHTML(resumes, "v123", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("expected non-empty path")
	}
}

func intPtr(i int) *int {
	return &i
}
