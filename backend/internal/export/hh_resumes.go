package export

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"tg-rag-ingestor/backend/internal/model"
	"tg-rag-ingestor/backend/internal/resumes"
)

const resumeSeparator = "=================================================="

type HHExporter struct {
	fs     model.FileSystem
	logger *slog.Logger
}

func NewHHExporter(fs model.FileSystem, logger *slog.Logger) *HHExporter {
	return &HHExporter{fs: fs, logger: logger}
}

func (e *HHExporter) WriteManifest(manifest *model.RunManifest, outputDir string) (string, error) {
	manifest.CreatedAt = time.Now()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	path := filepath.Join(outputDir, "manifest.json")
	if err := e.fs.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write manifest: %w", err)
	}
	return path, nil
}

func (e *HHExporter) WriteCombinedMarkdown(resumeItems []*model.Resume, vacancyID, outputDir string) (string, error) {
	var b strings.Builder
	for idx, item := range resumeItems {
		if idx > 0 {
			b.WriteString("\n\n")
		}
		writeResumeBlock(&b, item, vacancyID)
	}
	path := filepath.Join(outputDir, "combined_resumes.md")
	if err := e.fs.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write combined markdown: %w", err)
	}
	return path, nil
}

func (e *HHExporter) WriteCombinedHTML(resumeItems []*model.Resume, vacancyID, outputDir string) (string, error) {
	path := filepath.Join(outputDir, "combined_resumes.html")
	w, err := e.fs.Create(path)
	if err != nil {
		return "", fmt.Errorf("create combined html: %w", err)
	}
	defer w.Close()

	tpl, err := template.New("hh_combined").Funcs(template.FuncMap{
		"orNA":         orNA,
		"formatArea":   formatArea,
		"formatSalary": formatSalary,
		"formatPeriod": formatPeriod,
		"contacts":     resumes.FormatContacts,
		"join":         strings.Join,
	}).Parse(combinedHTMLTemplate)
	if err != nil {
		return "", fmt.Errorf("parse combined html template: %w", err)
	}

	payload := struct {
		VacancyID string
		Resumes   []*model.Resume
	}{
		VacancyID: vacancyID,
		Resumes:   resumeItems,
	}
	if err := tpl.Execute(w, payload); err != nil {
		return "", fmt.Errorf("render combined html: %w", err)
	}
	return path, nil
}

func (e *HHExporter) WriteCombinedPDF(ctx context.Context, htmlPath, outputDir string) (string, error) {
	output := filepath.Join(outputDir, "combined_resumes.pdf")
	chromeBins := []string{
		"google-chrome",
		"google-chrome-stable",
		"chromium",
		"chromium-browser",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}

	var chromeBin string
	for _, candidate := range chromeBins {
		if path, err := exec.LookPath(candidate); err == nil {
			chromeBin = path
			break
		}
		if _, err := os.Stat(candidate); err == nil {
			chromeBin = candidate
			break
		}
	}
	if chromeBin == "" {
		return "", fmt.Errorf("chrome/chromium not found")
	}

	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return "", fmt.Errorf("resolve html path: %w", err)
	}
	absPDF, err := filepath.Abs(output)
	if err != nil {
		return "", fmt.Errorf("resolve output pdf path: %w", err)
	}

	cmd := exec.CommandContext(ctx, chromeBin,
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--print-to-pdf="+absPDF,
		"--print-to-pdf-no-header",
		"file://"+absHTML,
	)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdf export via headless chrome failed: %w", err)
	}
	return output, nil
}

func writeResumeBlock(b *strings.Builder, resumeItem *model.Resume, vacancyID string) {
	b.WriteString(resumeSeparator + "\n")
	b.WriteString(fmt.Sprintf("КАНДИДАТ: %s\n", resumeItem.FIO()))
	b.WriteString(fmt.Sprintf("RESUME ID: %s\n", resumeItem.ID))
	b.WriteString(fmt.Sprintf("VACANCY ID: %s\n", vacancyID))
	b.WriteString(fmt.Sprintf("UPDATED AT: %s\n", orNA(resumeItem.UpdatedAt)))
	b.WriteString(fmt.Sprintf("SOURCE URL: %s\n", orNA(resumeItem.AlternateURL)))
	b.WriteString(resumeSeparator + "\n\n")

	b.WriteString(fmt.Sprintf("Желаемая должность: %s\n", orNA(resumeItem.Title)))
	b.WriteString(fmt.Sprintf("Локация: %s\n", formatArea(resumeItem.Area)))
	if resumeItem.Age != nil {
		b.WriteString(fmt.Sprintf("Возраст: %d\n", *resumeItem.Age))
	} else {
		b.WriteString("Возраст: [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	}
	b.WriteString(fmt.Sprintf("Зарплата: %s\n", formatSalary(resumeItem.Salary)))

	contacts := resumes.FormatContacts(resumeItem.Contact)
	if len(contacts) == 0 {
		b.WriteString("Контакты: [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	} else {
		b.WriteString("Контакты:\n")
		for _, c := range contacts {
			b.WriteString("- " + c + "\n")
		}
	}

	if len(resumeItem.SkillSet) > 0 {
		b.WriteString(fmt.Sprintf("Навыки: %s\n", strings.Join(resumeItem.SkillSet, ", ")))
	} else if strings.TrimSpace(resumeItem.Skills) != "" {
		b.WriteString(fmt.Sprintf("Навыки: %s\n", strings.TrimSpace(resumeItem.Skills)))
	} else {
		b.WriteString("Навыки: [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	}

	b.WriteString("Опыт работы:\n")
	if len(resumeItem.Experience) == 0 {
		b.WriteString("- [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	} else {
		for _, exp := range resumeItem.Experience {
			b.WriteString(fmt.Sprintf("- %s — %s (%s)\n", orNA(exp.Company), orNA(exp.Position), formatPeriod(exp.Start, exp.End)))
			if strings.TrimSpace(exp.Description) != "" {
				b.WriteString("  " + strings.TrimSpace(exp.Description) + "\n")
			}
		}
	}

	b.WriteString("Образование:\n")
	if resumeItem.Education == nil || (len(resumeItem.Education.Primary) == 0 && len(resumeItem.Education.Additional) == 0) {
		b.WriteString("- [НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	} else {
		for _, item := range resumeItem.Education.Primary {
			b.WriteString(fmt.Sprintf("- %s, %s (%d)\n", orNA(item.Name), orNA(item.Result), item.Year))
		}
		for _, item := range resumeItem.Education.Additional {
			b.WriteString(fmt.Sprintf("- [доп] %s, %s (%d)\n", orNA(item.Name), orNA(item.Result), item.Year))
		}
	}

	b.WriteString("О себе:\n")
	if strings.TrimSpace(resumeItem.Skills) == "" {
		b.WriteString("[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]\n")
	} else {
		b.WriteString(strings.TrimSpace(resumeItem.Skills) + "\n")
	}
}

func formatArea(area *model.NamedRef) string {
	if area == nil || strings.TrimSpace(area.Name) == "" {
		return "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]"
	}
	return strings.TrimSpace(area.Name)
}

func formatSalary(salary *model.Salary) string {
	if salary == nil || salary.Amount == 0 {
		return "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]"
	}
	if salary.Currency == "" {
		return fmt.Sprintf("%d", salary.Amount)
	}
	return fmt.Sprintf("%d %s", salary.Amount, salary.Currency)
}

func formatPeriod(start, end string) string {
	if strings.TrimSpace(start) == "" {
		start = "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]"
	}
	if strings.TrimSpace(end) == "" {
		end = "по настоящее время"
	}
	return start + " — " + end
}

func orNA(value string) string {
	if strings.TrimSpace(value) == "" {
		return "[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]"
	}
	return strings.TrimSpace(value)
}

func CollectExtractionFiles(fs model.FileSystem, extractionDir string) ([]string, error) {
	entries, err := fs.ReadDir(extractionDir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			if entry.Name() != "originals" {
				continue
			}
			originalsDir := filepath.Join(extractionDir, "originals")
			originalEntries, readErr := fs.ReadDir(originalsDir)
			if readErr != nil {
				continue
			}
			for _, original := range originalEntries {
				if original.IsDir() {
					continue
				}
				files = append(files, filepath.ToSlash(filepath.Join("originals", original.Name())))
			}
			continue
		}
		files = append(files, entry.Name())
	}
	return files, nil
}

var combinedHTMLTemplate = `<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>HeadHunter resumes {{.VacancyID}}</title>
  <style>
    body { font-family: "IBM Plex Sans", "Segoe UI", sans-serif; background: #f8fafc; color: #0f172a; margin: 0; padding: 1.2rem; }
    .container { max-width: 980px; margin: 0 auto; }
    .card { background: #fff; border: 1px solid #dbe1ef; border-radius: 10px; margin: 0 0 1rem; padding: 1rem; }
    .title { margin: 0 0 0.4rem; color: #1d4ed8; }
    .meta { color: #475569; font-size: 0.92rem; margin-bottom: 0.7rem; }
    .section { margin-bottom: 0.7rem; }
    .section h3 { margin: 0 0 0.35rem; font-size: 0.98rem; }
    .exp-item { margin-bottom: 0.55rem; padding-left: 0.7rem; border-left: 3px solid #93c5fd; }
    .na { color: #64748b; font-style: italic; }
  </style>
</head>
<body>
  <main class="container">
    <h1>HeadHunter резюме по вакансии {{.VacancyID}}</h1>
    <p>Кандидатов в объединенном файле: {{len .Resumes}}</p>
    {{range .Resumes}}
    <article class="card">
      <h2 class="title">{{.FIO}}</h2>
      <p class="meta">Resume ID: {{.ID}} | Updated: {{orNA .UpdatedAt}} | URL: {{orNA .AlternateURL}}</p>
      <section class="section">
        <h3>Основное</h3>
        <p><strong>Желаемая должность:</strong> {{orNA .Title}}</p>
        <p><strong>Локация:</strong> {{formatArea .Area}}</p>
        <p><strong>Возраст:</strong> {{if .Age}}{{.Age}}{{else}}<span class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</span>{{end}}</p>
        <p><strong>Зарплата:</strong> {{formatSalary .Salary}}</p>
      </section>
      <section class="section">
        <h3>Контакты</h3>
        {{$contacts := contacts .Contact}}
        {{if $contacts}}
          <ul>{{range $contacts}}<li>{{.}}</li>{{end}}</ul>
        {{else}}
          <p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>
        {{end}}
      </section>
      <section class="section">
        <h3>Навыки</h3>
        {{if .SkillSet}}
          <p>{{join .SkillSet ", "}}</p>
        {{else if .Skills}}
          <p>{{.Skills}}</p>
        {{else}}
          <p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>
        {{end}}
      </section>
      <section class="section">
        <h3>Опыт работы</h3>
        {{if .Experience}}
          {{range .Experience}}
          <div class="exp-item">
            <div><strong>{{orNA .Company}}</strong> — {{orNA .Position}}</div>
            <div>{{formatPeriod .Start .End}}</div>
            {{if .Description}}<div>{{.Description}}</div>{{end}}
          </div>
          {{end}}
        {{else}}
          <p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>
        {{end}}
      </section>
      <section class="section">
        <h3>Образование</h3>
        {{if .Education}}
          {{range .Education.Primary}}<p>{{orNA .Name}} — {{orNA .Result}} ({{.Year}})</p>{{end}}
          {{range .Education.Additional}}<p>[доп] {{orNA .Name}} — {{orNA .Result}} ({{.Year}})</p>{{end}}
        {{else}}
          <p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>
        {{end}}
      </section>
      <section class="section">
        <h3>О себе</h3>
        {{if .Skills}}<p>{{.Skills}}</p>{{else}}<p class="na">[НЕДОСТУПНО ЧЕРЕЗ API / СКРЫТО]</p>{{end}}
      </section>
    </article>
    {{end}}
  </main>
</body>
</html>`
