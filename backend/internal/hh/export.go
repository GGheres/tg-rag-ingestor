package hh

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

const separator = "=================================================="

// GenerateCombinedText creates a plain text file with all resumes separated.
func GenerateCombinedText(resumes []*Resume, vacancyID, outputDir string) (string, error) {
	var sb strings.Builder

	for i, r := range resumes {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		writeResumeBlock(&sb, r, vacancyID)
	}

	path := filepath.Join(outputDir, "combined_resumes.txt")
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		return "", fmt.Errorf("write combined text: %w", err)
	}
	return path, nil
}

func writeResumeBlock(sb *strings.Builder, r *Resume, vacancyID string) {
	sb.WriteString(separator + "\n")
	sb.WriteString(fmt.Sprintf("КАНДИДАТ: %s\n", r.FIO()))
	sb.WriteString(fmt.Sprintf("RESUME ID: %s\n", r.ID))
	sb.WriteString(fmt.Sprintf("VACANCY ID: %s\n", vacancyID))
	sb.WriteString(fmt.Sprintf("UPDATED AT: %s\n", orNA(r.UpdatedAt)))
	sb.WriteString(fmt.Sprintf("SOURCE URL: %s\n", orNA(r.AlternateURL)))
	sb.WriteString(separator + "\n\n")

	sb.WriteString(fmt.Sprintf("Желаемая должность: %s\n", orNA(r.Title)))
	sb.WriteString(fmt.Sprintf("Локация: %s\n", formatIDName(r.Area)))

	if r.Age != nil {
		sb.WriteString(fmt.Sprintf("Возраст: %d\n", *r.Age))
	} else {
		sb.WriteString("Возраст: [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	}

	sb.WriteString(fmt.Sprintf("Зарплата: %s\n", formatSalary(r.Salary)))

	contacts := ExtractAllContacts(r.Contact)
	if len(contacts) > 0 {
		sb.WriteString("Контакты:\n")
		for _, c := range contacts {
			sb.WriteString("  " + c + "\n")
		}
	} else {
		sb.WriteString("Контакты: [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	}

	if len(r.SkillSet) > 0 {
		sb.WriteString(fmt.Sprintf("Навыки: %s\n", strings.Join(r.SkillSet, ", ")))
	} else if r.Skills != "" {
		sb.WriteString(fmt.Sprintf("Навыки: %s\n", r.Skills))
	} else {
		sb.WriteString("Навыки: [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	}

	sb.WriteString("\nОпыт работы:\n")
	if len(r.Experience) > 0 {
		for _, exp := range r.Experience {
			period := formatPeriod(exp.Start, exp.End)
			sb.WriteString(fmt.Sprintf("- %s — %s (%s)\n", orNA(exp.Company), orNA(exp.Position), period))
			if exp.Description != "" {
				sb.WriteString(fmt.Sprintf("  %s\n", exp.Description))
			}
		}
	} else {
		sb.WriteString("  [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	}

	sb.WriteString("\nОбразование:\n")
	if r.Education != nil {
		if r.Education.Level != nil {
			sb.WriteString(fmt.Sprintf("  Уровень: %s\n", r.Education.Level.Name))
		}
		for _, edu := range r.Education.Primary {
			sb.WriteString(fmt.Sprintf("- %s, %s (%d)\n", orNA(edu.Name), orNA(edu.Result), edu.Year))
		}
		for _, edu := range r.Education.Additional {
			sb.WriteString(fmt.Sprintf("- [доп] %s, %s (%d)\n", orNA(edu.Name), orNA(edu.Result), edu.Year))
		}
		if len(r.Education.Primary) == 0 && len(r.Education.Additional) == 0 {
			sb.WriteString("  [НЕТ ДАННЫХ]\n")
		}
	} else {
		sb.WriteString("  [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	}

	sb.WriteString("\nО себе:\n")
	if r.Skills != "" {
		sb.WriteString(r.Skills + "\n")
	} else {
		sb.WriteString("[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	}

	if len(r.Language) > 0 {
		sb.WriteString("\nЯзыки:\n")
		for _, lang := range r.Language {
			level := ""
			if lang.Level != nil {
				level = " (" + lang.Level.Name + ")"
			}
			sb.WriteString(fmt.Sprintf("- %s%s\n", lang.Name, level))
		}
	}
}

// GenerateCombinedHTML creates an HTML file with all resumes.
func GenerateCombinedHTML(resumes []*Resume, vacancyID, outputDir string) (string, error) {
	path := filepath.Join(outputDir, "combined_resumes.html")
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create html file: %w", err)
	}
	defer f.Close()

	tmpl, err := template.New("combined").Funcs(template.FuncMap{
		"orNA":         orNA,
		"formatIDName": formatIDName,
		"formatSalary": formatSalary,
		"formatPeriod": formatPeriod,
		"contacts":     ExtractAllContacts,
		"joinSkills":   func(s []string) string { return strings.Join(s, ", ") },
	}).Parse(htmlTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	data := struct {
		VacancyID string
		Resumes   []*Resume
	}{
		VacancyID: vacancyID,
		Resumes:   resumes,
	}

	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return path, nil
}

func orNA(s string) string {
	if s == "" {
		return "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]"
	}
	return s
}

func formatIDName(n *IDName) string {
	if n == nil {
		return "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]"
	}
	return n.Name
}

func formatSalary(s *Salary) string {
	if s == nil || s.Amount == 0 {
		return "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]"
	}
	return fmt.Sprintf("%d %s", s.Amount, s.Currency)
}

func formatPeriod(start, end string) string {
	s := orNA(start)
	e := end
	if e == "" {
		e = "по настоящее время"
	}
	return s + " — " + e
}

var htmlTemplate = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Резюме по вакансии {{.VacancyID}}</title>
<style>
  body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; max-width: 900px; margin: 0 auto; padding: 20px; background: #f5f5f5; color: #333; }
  .resume { background: #fff; border-radius: 8px; padding: 24px; margin-bottom: 24px; box-shadow: 0 1px 3px rgba(0,0,0,0.12); page-break-after: always; }
  .header { border-bottom: 2px solid #0066cc; padding-bottom: 12px; margin-bottom: 16px; }
  .header h2 { margin: 0 0 8px 0; color: #0066cc; }
  .meta { color: #666; font-size: 0.9em; }
  .meta span { margin-right: 16px; }
  .section { margin-bottom: 16px; }
  .section h3 { color: #444; border-bottom: 1px solid #eee; padding-bottom: 4px; }
  .experience-item { margin-bottom: 12px; padding-left: 12px; border-left: 3px solid #0066cc; }
  .experience-item .company { font-weight: bold; }
  .experience-item .period { color: #888; font-size: 0.9em; }
  .skills { display: flex; flex-wrap: wrap; gap: 6px; }
  .skill-tag { background: #e8f0fe; color: #0066cc; padding: 2px 10px; border-radius: 12px; font-size: 0.9em; }
  .na { color: #999; font-style: italic; }
  .contacts li { margin-bottom: 4px; }
  @media print { body { background: #fff; } .resume { box-shadow: none; border: 1px solid #ddd; } }
</style>
</head>
<body>
<h1>Резюме кандидатов — вакансия {{.VacancyID}}</h1>
<p>Всего кандидатов: {{len .Resumes}}</p>
{{range .Resumes}}
<div class="resume">
  <div class="header">
    <h2>{{.FIO}}</h2>
    <div class="meta">
      <span>Resume ID: {{.ID}}</span>
      <span>Обновлено: {{orNA .UpdatedAt}}</span>
      {{if .AlternateURL}}<span><a href="{{.AlternateURL}}" target="_blank">Открыть на hh.ru</a></span>{{end}}
    </div>
  </div>

  <div class="section">
    <h3>Основное</h3>
    <p><strong>Желаемая должность:</strong> {{orNA .Title}}</p>
    <p><strong>Локация:</strong> {{formatIDName .Area}}</p>
    <p><strong>Возраст:</strong> {{if .Age}}{{.Age}}{{else}}<span class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</span>{{end}}</p>
    <p><strong>Зарплата:</strong> {{formatSalary .Salary}}</p>
  </div>

  <div class="section">
    <h3>Контакты</h3>
    {{$contacts := contacts .Contact}}
    {{if $contacts}}
    <ul class="contacts">{{range $contacts}}<li>{{.}}</li>{{end}}</ul>
    {{else}}<p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>{{end}}
  </div>

  <div class="section">
    <h3>Навыки</h3>
    {{if .SkillSet}}
    <div class="skills">{{range .SkillSet}}<span class="skill-tag">{{.}}</span>{{end}}</div>
    {{else if .Skills}}<p>{{.Skills}}</p>
    {{else}}<p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>{{end}}
  </div>

  <div class="section">
    <h3>Опыт работы</h3>
    {{if .Experience}}
    {{range .Experience}}
    <div class="experience-item">
      <div class="company">{{orNA .Company}}</div>
      <div>{{orNA .Position}}</div>
      <div class="period">{{formatPeriod .Start .End}}</div>
      {{if .Description}}<p>{{.Description}}</p>{{end}}
    </div>
    {{end}}
    {{else}}<p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>{{end}}
  </div>

  <div class="section">
    <h3>Образование</h3>
    {{if .Education}}
      {{if .Education.Level}}<p>Уровень: {{.Education.Level.Name}}</p>{{end}}
      {{range .Education.Primary}}<p>{{orNA .Name}} — {{orNA .Result}} ({{.Year}})</p>{{end}}
      {{range .Education.Additional}}<p>[доп] {{orNA .Name}} — {{orNA .Result}} ({{.Year}})</p>{{end}}
    {{else}}<p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>{{end}}
  </div>

  <div class="section">
    <h3>О себе</h3>
    {{if .Skills}}<p>{{.Skills}}</p>{{else}}<p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>{{end}}
  </div>

  {{if .Language}}
  <div class="section">
    <h3>Языки</h3>
    <ul>{{range .Language}}<li>{{.Name}}{{if .Level}} ({{.Level.Name}}){{end}}</li>{{end}}</ul>
  </div>
  {{end}}
</div>
{{end}}
</body>
</html>`
