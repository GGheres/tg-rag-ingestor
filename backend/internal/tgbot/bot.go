package tgbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tg-rag-ingestor/backend/internal/hhaccess"
	"tg-rag-ingestor/backend/internal/hhpublic"
	"tg-rag-ingestor/backend/internal/hhresumesearch"
	hhservice "tg-rag-ingestor/backend/internal/service"

	"tg-rag-ingestor/backend/internal/export"
	"tg-rag-ingestor/backend/internal/filescan"
	"tg-rag-ingestor/backend/internal/ingestion"
	"tg-rag-ingestor/backend/internal/model"
	"tg-rag-ingestor/backend/internal/models"
	"tg-rag-ingestor/backend/internal/storage"
	telegramingest "tg-rag-ingestor/backend/internal/telegram"
	"tg-rag-ingestor/backend/internal/youtube"
)

const (
	actionTGDocument = "📘 TG документ"
	actionTGSource   = "📡 TG источник"
	actionTGLinks    = "🔗 TG ссылки"
	actionYouTube    = "▶️ YouTube"
	actionAudioFile  = "🎙️ Аудио файл"
	actionJSONFile   = "🧩 JSON файл"
	actionFileUpload = "📁 Загрузка файлов"
	actionHH         = "🎯 HH резюме"
	actionHHCabinet  = "🗂️ Вакансии из кабинета"
	actionHHGlobal   = "🌍 Глобальный поиск вакансий"
	actionHHPeople   = "👤 Глобальный поиск сотрудников"
	actionHHStatus   = "🔐 Статус HH доступа"
	actionHHToday    = "📅 Сегодня"
	actionHH7Days    = "🗓️ 7 дней"
	actionHH30Days   = "🗓️ 30 дней"
	actionSources    = "🗂️ Источники"
	actionHelp       = "❓ Помощь"
	actionCancel     = "✖️ Отмена"

	maxTelegramDocumentBytes = 45 << 20
)

type state string

const (
	stateIdle             state = ""
	stateAwaitTGDocument  state = "await_tg_document"
	stateAwaitTGSource    state = "await_tg_source"
	stateAwaitTGLinks     state = "await_tg_links"
	stateAwaitYouTube     state = "await_youtube"
	stateAwaitAudioFile   state = "await_audio_file"
	stateAwaitJSONFile    state = "await_json_file"
	stateAwaitFileUpload  state = "await_file_upload"
	stateAwaitHHMode      state = "await_hh_mode"
	stateAwaitHHVacancyID   state = "await_hh_vacancy_id"
	stateAwaitHHExtractionDate state = "await_hh_extraction_date"
	stateAwaitHHPublic      state = "await_hh_public_query"
	stateAwaitHHPeople    state = "await_hh_people_query"
)

var fileUploadNativeExtensions = map[string]struct{}{
	".pdf": {}, ".docx": {}, ".pptx": {}, ".xlsx": {}, ".epub": {}, ".rtf": {}, ".odt": {},
	".png": {}, ".jpg": {}, ".jpeg": {}, ".avif": {}, ".gif": {}, ".bmp": {}, ".tif": {}, ".tiff": {}, ".webp": {},
}

var fileUploadTextExtensions = map[string]struct{}{
	".txt": {}, ".md": {}, ".markdown": {}, ".csv": {}, ".tsv": {},
	".json": {}, ".jsonl": {}, ".yaml": {}, ".yml": {}, ".xml": {},
	".html": {}, ".htm": {}, ".log": {}, ".ini": {}, ".cfg": {}, ".conf": {},
	".sql": {}, ".py": {}, ".go": {}, ".js": {}, ".ts": {}, ".jsx": {},
	".tsx": {}, ".css": {}, ".scss": {}, ".java": {}, ".kt": {}, ".c": {},
	".cpp": {}, ".h": {}, ".hpp": {}, ".sh": {}, ".bash": {}, ".zsh": {},
	".ps1": {}, ".rb": {}, ".php": {}, ".swift": {}, ".rs": {}, ".proto": {},
}

var audioExtensions = map[string]struct{}{
	".wav": {}, ".mp3": {}, ".m4a": {}, ".webm": {}, ".opus": {}, ".ogg": {}, ".flac": {}, ".mp4": {},
}

type Config struct {
	Token          string
	AllowedUserIDs string
}

type session struct {
	State            state
	HHOptions        []model.HHVacancyListItem
	HHSelectedVacancyID string
	HHSelectedVacancyName string
	HHPublicDateFrom string
	HHPublicDateTo   string
	HHPeopleMaxItems int
}

type Bot struct {
	client           *Client
	repo             *storage.Repository
	ingestionService *ingestion.Service
	youtubeService   *youtube.Service
	exportService    *export.Service
	fileScanService  *filescan.Service
	hhConfig         model.HHConfig
	logger           *slog.Logger

	allowedUsers map[int64]struct{}

	mu       sync.Mutex
	sessions map[int64]session
}

func New(cfg Config, repo *storage.Repository, ingestionService *ingestion.Service, youtubeService *youtube.Service, exportService *export.Service, fileScanService *filescan.Service, hhConfig model.HHConfig, logger *slog.Logger) (*Bot, error) {
	token := strings.TrimSpace(cfg.Token)
	if token == "" {
		return nil, errors.New("missing TG_BOT_TOKEN")
	}
	if repo == nil || ingestionService == nil || youtubeService == nil || exportService == nil || fileScanService == nil {
		return nil, errors.New("telegram bot dependencies are not fully configured")
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &Bot{
		client:           NewClient(token),
		repo:             repo,
		ingestionService: ingestionService,
		youtubeService:   youtubeService,
		exportService:    exportService,
		fileScanService:  fileScanService,
		hhConfig:         hhConfig,
		logger:           logger,
		allowedUsers:     parseAllowedUsers(cfg.AllowedUserIDs),
		sessions:         make(map[int64]session),
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := b.client.GetUpdates(ctx, offset, 60)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			b.logger.Error("telegram_bot_get_updates_failed", "error", err)
			time.Sleep(3 * time.Second)
			continue
		}

		for _, update := range updates {
			offset = update.UpdateID + 1
			if update.Message == nil {
				continue
			}
			b.handleMessage(update.Message)
		}
	}
}

func (b *Bot) handleMessage(msg *Message) {
	if msg == nil {
		return
	}
	if msg.Chat.Type != "private" {
		_ = b.reply(msg.Chat.ID, "Бот работает только в личных сообщениях.", nil)
		return
	}
	if !b.isAllowed(msg.From) {
		_ = b.reply(msg.Chat.ID, "Доступ к боту закрыт для этого аккаунта.", nil)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text != "" && strings.HasPrefix(text, "/") {
		b.handleCommand(msg, text)
		return
	}

	switch text {
	case actionCancel:
		b.setState(msg.Chat.ID, stateIdle)
		_ = b.reply(msg.Chat.ID, "Текущий сценарий сброшен.", mainKeyboard())
		return
	case actionHelp:
		_ = b.reply(msg.Chat.ID, helpText(), mainKeyboard())
		return
	case actionSources:
		b.sendSources(msg.Chat.ID)
		return
	case actionTGDocument:
		b.setState(msg.Chat.ID, stateAwaitTGDocument)
		_ = b.reply(msg.Chat.ID, "Пришлите ссылку на Telegram-канал или invite-ссылку. Я синхронизирую историю и соберу один RAG-документ.", mainKeyboard())
		return
	case actionTGSource:
		b.setState(msg.Chat.ID, stateAwaitTGSource)
		_ = b.reply(msg.Chat.ID, "Пришлите @username или ссылку на канал. Я создам syncable source и сразу запущу sync.", mainKeyboard())
		return
	case actionTGLinks:
		b.setState(msg.Chat.ID, stateAwaitTGLinks)
		_ = b.reply(msg.Chat.ID, "Пришлите ссылки на сообщения, по одной в строке. Все ссылки должны относиться к одному каналу.", mainKeyboard())
		return
	case actionYouTube:
		b.setState(msg.Chat.ID, stateAwaitYouTube)
		_ = b.reply(msg.Chat.ID, "Пришлите ссылку на YouTube-видео. Я создам source, скачаю аудио и прогоню транскрибацию.", mainKeyboard())
		return
	case actionAudioFile:
		b.setState(msg.Chat.ID, stateAwaitAudioFile)
		_ = b.reply(msg.Chat.ID, "Пришлите аудио, voice, video или document с аудиофайлом. Я загружу и расшифрую его.", mainKeyboard())
		return
	case actionJSONFile:
		b.setState(msg.Chat.ID, stateAwaitJSONFile)
		_ = b.reply(msg.Chat.ID, "Пришлите `.json` файлом или отправьте JSON текстом одним сообщением.", mainKeyboard())
		return
	case actionFileUpload:
		b.setState(msg.Chat.ID, stateAwaitFileUpload)
		_ = b.reply(msg.Chat.ID, fileUploadHelpText(), mainKeyboard())
		return
	case actionHH:
		b.setState(msg.Chat.ID, stateAwaitHHMode)
		_ = b.reply(msg.Chat.ID, hhModePromptText(), hhModeKeyboard())
		return
	case actionHHCabinet:
		b.setState(msg.Chat.ID, stateAwaitHHVacancyID)
		go b.promptHHVacancySelection(msg.Chat.ID)
		return
	case actionHHGlobal:
		b.startHHPublicSearchFlow(msg.Chat.ID)
		return
	case actionHHPeople:
		b.startHHPeopleSearchFlow(msg.Chat.ID)
		return
	case actionHHStatus:
		_ = b.reply(msg.Chat.ID, "Проверяю HH доступы, платные API-услуги и лимиты менеджера.", mainKeyboard())
		go b.runHHAccessStatus(msg.Chat.ID)
		return
	}

	switch b.getState(msg.Chat.ID) {
	case stateAwaitTGDocument:
		b.handleTGDocument(msg, text)
	case stateAwaitTGSource:
		b.handleTGSource(msg, text)
	case stateAwaitTGLinks:
		b.handleTGLinks(msg, text)
	case stateAwaitYouTube:
		b.handleYouTube(msg, text)
	case stateAwaitAudioFile:
		b.handleAudioUpload(msg)
	case stateAwaitJSONFile:
		b.handleJSONImport(msg, text)
	case stateAwaitFileUpload:
		b.handleFileUpload(msg)
	case stateAwaitHHMode:
		b.handleHHModeSelection(msg, text)
	case stateAwaitHHVacancyID:
		b.handleHHExtraction(msg, text)
	case stateAwaitHHExtractionDate:
		b.handleHHExtractionDateSelection(msg, text)
	case stateAwaitHHPublic:
		b.handleHHPublicSearch(msg, text)
	case stateAwaitHHPeople:
		b.handleHHPeopleSearch(msg, text)
	default:
		_ = b.reply(msg.Chat.ID, "Выберите сценарий с клавиатуры или используйте `/help`.", mainKeyboard())
	}
}

func (b *Bot) handleCommand(msg *Message, raw string) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return
	}

	cmd := strings.ToLower(strings.SplitN(fields[0], "@", 2)[0])
	arg := ""
	if len(fields) > 1 {
		arg = strings.TrimSpace(strings.Join(fields[1:], " "))
	}

	switch cmd {
	case "/start", "/menu":
		b.setState(msg.Chat.ID, stateIdle)
		_ = b.reply(msg.Chat.ID, welcomeText(msg.From), mainKeyboard())
	case "/help":
		_ = b.reply(msg.Chat.ID, helpText(), mainKeyboard())
	case "/cancel":
		b.setState(msg.Chat.ID, stateIdle)
		_ = b.reply(msg.Chat.ID, "Сценарий отменен.", mainKeyboard())
	case "/sources":
		b.sendSources(msg.Chat.ID)
	case "/sync":
		if arg == "" {
			_ = b.reply(msg.Chat.ID, "Использование: `/sync <source_id>`", mainKeyboard())
			return
		}
		_ = b.reply(msg.Chat.ID, fmt.Sprintf("Запустил sync для source `%s`.", arg), mainKeyboard())
		go b.runSyncAndExport(msg.Chat.ID, arg, "manual")
	case "/export":
		if arg == "" {
			_ = b.reply(msg.Chat.ID, "Использование: `/export <source_id>`", mainKeyboard())
			return
		}
		_ = b.reply(msg.Chat.ID, fmt.Sprintf("Готовлю TXT RAG export для source `%s`.", arg), mainKeyboard())
		go b.runSourceExport(msg.Chat.ID, arg, "manual_export")
	case "/status":
		if arg == "" {
			_ = b.reply(msg.Chat.ID, "Использование: `/status <source_id>`", mainKeyboard())
			return
		}
		b.sendSourceStatus(msg.Chat.ID, arg)
	case "/document":
		if arg == "" {
			_ = b.reply(msg.Chat.ID, "Использование: `/document <document_id>`", mainKeyboard())
			return
		}
		go b.sendDocumentByID(msg.Chat.ID, arg)
	case "/hh":
		if arg == "" {
			b.setState(msg.Chat.ID, stateAwaitHHMode)
			_ = b.reply(msg.Chat.ID, hhModePromptText(), hhModeKeyboard())
			return
		}
		go b.resolveAndRunHHExtraction(msg.Chat.ID, arg)
	default:
		_ = b.reply(msg.Chat.ID, "Неизвестная команда. Используйте `/help`.", mainKeyboard())
	}
}

func (b *Bot) handleTGDocument(msg *Message, text string) {
	if text == "" {
		_ = b.reply(msg.Chat.ID, "Нужна ссылка на канал или invite-ссылка.", mainKeyboard())
		return
	}
	b.setState(msg.Chat.ID, stateIdle)
	_ = b.reply(msg.Chat.ID, "Запустил импорт канала как одного документа. Это может занять несколько минут.", mainKeyboard())

	go func(chatID int64, raw string) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()

		source, err := b.ingestionService.CreateTelegramChannelDocumentSource(ctx, ingestion.CreateTelegramChannelDocumentSourceInput{
			URL: raw,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}
		b.runSyncAndExportForSource(ctx, chatID, source.ID, "telegram_channel_document")
	}(msg.Chat.ID, text)
}

func (b *Bot) handleTGSource(msg *Message, text string) {
	if text == "" {
		_ = b.reply(msg.Chat.ID, "Нужен @username или ссылка на канал.", mainKeyboard())
		return
	}
	b.setState(msg.Chat.ID, stateIdle)
	_ = b.reply(msg.Chat.ID, "Создаю Telegram source и запускаю sync.", mainKeyboard())

	go func(chatID int64, raw string) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()

		username, normalizedURL, err := telegramingest.ResolveUsername(telegramingest.ResolveInput{
			URL:      raw,
			Username: raw,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		source, err := b.repo.CreateSource(ctx, storage.CreateSourceInput{
			SourceType: "telegram_public_channel",
			Username:   ptrIfNotEmpty(username),
			URL:        normalizedURL,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		b.runSyncAndExportForSource(ctx, chatID, source.ID, "telegram_public_channel")
	}(msg.Chat.ID, text)
}

func (b *Bot) handleTGLinks(msg *Message, text string) {
	lines := splitLines(text)
	if len(lines) == 0 {
		_ = b.reply(msg.Chat.ID, "Пришлите хотя бы одну ссылку на сообщение.", mainKeyboard())
		return
	}
	b.setState(msg.Chat.ID, stateIdle)
	_ = b.reply(msg.Chat.ID, fmt.Sprintf("Импортирую %d message links и запускаю sync.", len(lines)), mainKeyboard())

	go func(chatID int64, links []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()

		source, err := b.ingestionService.CreateTelegramMessageLinkSource(ctx, ingestion.CreateTelegramMessageLinkSourceInput{
			MessageLinks: links,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}
		b.runSyncAndExportForSource(ctx, chatID, source.ID, "telegram_message_links")
	}(msg.Chat.ID, lines)
}

func (b *Bot) handleYouTube(msg *Message, text string) {
	if text == "" {
		_ = b.reply(msg.Chat.ID, "Нужна ссылка на YouTube-видео.", mainKeyboard())
		return
	}
	b.setState(msg.Chat.ID, stateIdle)
	_ = b.reply(msg.Chat.ID, "Создаю YouTube source и запускаю download/transcribe пайплайн.", mainKeyboard())

	go func(chatID int64, raw string) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()

		source, err := b.youtubeService.CreateYouTubeSource(ctx, youtube.CreateYouTubeSourceInput{
			URL: raw,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		result, err := b.youtubeService.DownloadSourceAudio(ctx, source.ID)
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		stats, _ := b.repo.GetSourceStats(ctx, source.ID)
		summary := fmt.Sprintf(
			"YouTube обработан.\nsource_id: `%s`\nstatus: `%s`\ndocuments: %d\nchunks: %d",
			result.Source.ID,
			result.Source.Status,
			stats.Documents,
			stats.Chunks,
		)
		_ = b.reply(chatID, summary, mainKeyboard())
		b.sendLatestDocumentForSource(ctx, chatID, source.ID, "youtube_transcript.txt")
	}(msg.Chat.ID, text)
}

func (b *Bot) handleAudioUpload(msg *Message) {
	b.startAudioUploadProcessing(msg, true)
}

func (b *Bot) startAudioUploadProcessing(msg *Message, resetState bool) {
	fileID, filename, err := extractTelegramFile(msg)
	if err != nil {
		_ = b.reply(msg.Chat.ID, err.Error(), mainKeyboard())
		return
	}

	if resetState {
		b.setState(msg.Chat.ID, stateIdle)
	}
	_ = b.reply(msg.Chat.ID, fmt.Sprintf("Получил `%s`. Скачиваю файл из Telegram и запускаю транскрибацию.", filename), mainKeyboard())

	go func(chatID int64, fileID string, filename string) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()

		fileMeta, err := b.client.GetFile(ctx, fileID)
		if err != nil {
			b.sendError(chatID, err)
			return
		}
		data, err := b.client.DownloadFile(ctx, fileMeta.FilePath)
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		result, err := b.youtubeService.UploadAndTranscribeAudio(ctx, youtube.UploadAudioInput{
			FileName: filename,
			FileData: data,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		summary := fmt.Sprintf(
			"Аудио обработано.\nsource_id: `%s`\ndocument_id: `%s`\nchunks: %d\nspeakers: %d",
			result.Source.ID,
			result.DocumentID,
			result.ChunkCount,
			len(result.SpeakerRoles),
		)
		_ = b.reply(chatID, summary, mainKeyboard())
		b.sendDocumentByID(chatID, result.DocumentID)
	}(msg.Chat.ID, fileID, filename)
}

func (b *Bot) handleJSONImport(msg *Message, text string) {
	var (
		filename string
		payload  []byte
		err      error
	)

	if msg.Document != nil {
		filename = safeFileName(msg.Document.FileName, "import.json")
		payload, err = b.downloadTelegramPayload(msg.Document.FileID)
		if err != nil {
			b.sendError(msg.Chat.ID, err)
			return
		}
	} else if text != "" {
		filename = "import.json"
		payload = []byte(text)
	} else {
		_ = b.reply(msg.Chat.ID, "Пришлите `.json` файлом или вставьте JSON текстом.", mainKeyboard())
		return
	}

	b.setState(msg.Chat.ID, stateIdle)
	_ = b.reply(msg.Chat.ID, "Импортирую JSON и подготавливаю результат.", mainKeyboard())

	go func(chatID int64, filename string, payload []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()

		result, err := b.ingestionService.ImportJSON(ctx, ingestion.ImportJSONInput{
			Filename: filename,
			Payload:  payload,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		summary := fmt.Sprintf(
			"JSON импорт завершен.\nsource_id: `%s`\nimported: %d\nprocessed: %d\nduplicates: %d\ntrash: %d\nchunks: %d",
			result.Source.ID,
			result.ImportedCount,
			result.ProcessedCount,
			result.DuplicateCount,
			result.TrashCount,
			result.ChunkCount,
		)
		_ = b.reply(chatID, summary, mainKeyboard())
		b.runSourceExport(chatID, result.Source.ID, "json_import")
	}(msg.Chat.ID, filename, payload)
}

func (b *Bot) handleFileUpload(msg *Message) {
	if msg == nil {
		return
	}

	if msg.Audio != nil || msg.Voice != nil || msg.Video != nil {
		_ = b.reply(msg.Chat.ID, "Похоже, это аудиофайл. Обрабатываю его через режим `🎙️ Аудио файл`.", mainKeyboard())
		b.startAudioUploadProcessing(msg, false)
		return
	}

	if msg.Document == nil {
		_ = b.reply(msg.Chat.ID, "Пришлите файл как `document`. Картинки лучше отправлять именно как файл, не как сжатое фото.", mainKeyboard())
		return
	}

	fileName := safeFileName(msg.Document.FileName, "uploaded_file.bin")
	if isAudioDocument(fileName, msg.Document.MimeType) {
		_ = b.reply(msg.Chat.ID, fmt.Sprintf("Файл `%s` похож на аудио. Перенаправляю его в пайплайн `🎙️ Аудио файл`.", fileName), mainKeyboard())
		b.startAudioUploadProcessing(msg, false)
		return
	}
	if !isSupportedFileUpload(fileName, msg.Document.MimeType) {
		_ = b.reply(msg.Chat.ID, unsupportedFileUploadMessage(fileName), mainKeyboard())
		return
	}
	_ = b.reply(msg.Chat.ID, fmt.Sprintf("Получил `%s`. Загружаю файл на сервер и запускаю обработку.", fileName), mainKeyboard())

	go func(chatID int64, doc *Document, fileName string) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()

		fileMeta, err := b.client.GetFile(ctx, doc.FileID)
		if err != nil {
			b.sendError(chatID, err)
			return
		}
		data, err := b.client.DownloadFile(ctx, fileMeta.FilePath)
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		tempDir, err := os.MkdirTemp("", "tg-bot-upload-*")
		if err != nil {
			b.sendError(chatID, err)
			return
		}
		defer os.RemoveAll(tempDir)

		filePath := filepath.Join(tempDir, fileName)
		if err := os.WriteFile(filePath, data, 0o644); err != nil {
			b.sendError(chatID, err)
			return
		}

		scanResult, err := b.fileScanService.ScanDirectory(ctx, filescan.ScanDirectoryInput{
			Path: tempDir,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		result, err := b.exportService.ExportFilesystemRAG(ctx, export.FilesystemRAGRequest{
			ScanResult:     scanResult,
			IncludeSkipped: true,
		})
		if err != nil {
			b.sendError(chatID, err)
			return
		}

		summary := fmt.Sprintf(
			"Файл обработан на сервере.\nfile: `%s`\nfiles_scanned: %d\nfiles_exported: %d\nfiles_skipped: %d\nrows: %d",
			fileName,
			result.ScannedFiles,
			result.ExportedFiles,
			result.SkippedFiles,
			result.RowCount,
		)
		_ = b.reply(chatID, summary, mainKeyboard())
		b.sendFile(chatID, result.FilePath, "filesystem_rag")
		b.promptFileUploadNextAction(chatID)
	}(msg.Chat.ID, msg.Document, fileName)
}

func (b *Bot) handleHHExtraction(msg *Message, text string) {
	go b.resolveAndRunHHExtraction(msg.Chat.ID, text)
}

func (b *Bot) handleHHModeSelection(msg *Message, text string) {
	switch strings.TrimSpace(text) {
	case actionHHCabinet:
		b.setState(msg.Chat.ID, stateAwaitHHVacancyID)
		go b.promptHHVacancySelection(msg.Chat.ID)
	case actionHHGlobal:
		b.startHHPublicSearchFlow(msg.Chat.ID)
	case actionHHPeople:
		b.startHHPeopleSearchFlow(msg.Chat.ID)
	case actionHHStatus:
		_ = b.reply(msg.Chat.ID, "Проверяю HH доступы, платные API-услуги и лимиты менеджера.", mainKeyboard())
		go b.runHHAccessStatus(msg.Chat.ID)
	default:
		_ = b.reply(msg.Chat.ID, "Выберите режим HH из клавиатуры ниже.", hhModeKeyboard())
	}
}

func (b *Bot) handleHHPublicSearch(msg *Message, text string) {
	query := strings.TrimSpace(text)
	if query == "" {
		_ = b.reply(msg.Chat.ID, "Нужен поисковый запрос. Можно просто прислать `golang`.", hhPublicKeyboard())
		return
	}
	switch query {
	case actionHHToday:
		dateFrom, dateTo := hhPresetRange(1)
		b.setHHPublicPreset(msg.Chat.ID, dateFrom, dateTo)
		_ = b.reply(msg.Chat.ID, "Период переключен на `Сегодня`. Теперь пришлите текст запроса, например `golang`.", hhPublicKeyboard())
		return
	case actionHH7Days:
		dateFrom, dateTo := hhPresetRange(7)
		b.setHHPublicPreset(msg.Chat.ID, dateFrom, dateTo)
		_ = b.reply(msg.Chat.ID, "Период переключен на `7 дней`. Теперь пришлите текст запроса, например `product manager`.", hhPublicKeyboard())
		return
	case actionHH30Days:
		dateFrom, dateTo := hhPresetRange(30)
		b.setHHPublicPreset(msg.Chat.ID, dateFrom, dateTo)
		_ = b.reply(msg.Chat.ID, "Период переключен на `30 дней`. Теперь пришлите текст запроса.", hhPublicKeyboard())
		return
	case "♾️ За все время":
		b.setHHPublicPreset(msg.Chat.ID, "", "")
		_ = b.reply(msg.Chat.ID, "Период переключен на `За все время`. Теперь пришлите текст запроса.", hhPublicKeyboard())
		return
	}
	currentSession := b.getSession(msg.Chat.ID)
	b.setState(msg.Chat.ID, stateIdle)
	_ = b.reply(msg.Chat.ID, "Запускаю глобальный HH поиск и импорт в RAG. Это может занять несколько минут.", mainKeyboard())
	go b.runHHPublicSearch(msg.Chat.ID, query, currentSession)
}

func (b *Bot) handleHHPeopleSearch(msg *Message, text string) {
	query := strings.TrimSpace(text)
	if query == "" {
		_ = b.reply(msg.Chat.ID, "Нужен поисковый запрос по сотрудникам. Можно просто прислать `golang`.", mainKeyboard())
		return
	}
	currentSession := b.getSession(msg.Chat.ID)
	b.setState(msg.Chat.ID, stateIdle)
	_ = b.reply(msg.Chat.ID, "Запускаю глобальный HH поиск сотрудников и импорт резюме в RAG. Это может занять несколько минут.", mainKeyboard())
	go b.runHHPeopleSearch(msg.Chat.ID, query, currentSession)
}

func (b *Bot) runSyncAndExport(chatID int64, sourceID string, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	b.runSyncAndExportForSource(ctx, chatID, sourceID, reason)
}

func (b *Bot) runSyncAndExportForSource(ctx context.Context, chatID int64, sourceID string, reason string) {
	result, err := b.ingestionService.SyncSource(ctx, sourceID, ingestion.SyncOptions{})
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	stats, _ := b.repo.GetSourceStats(ctx, sourceID)
	summary := fmt.Sprintf(
		"Sync завершен.\nsource_id: `%s`\nreason: `%s`\npages: %d\nmessages: %d\nprocessed: %d\nduplicates: %d\ntrash: %d\nchunks: %d\nstats.documents: %d",
		sourceID,
		reason,
		result.PagesFetched,
		result.MessagesFetched,
		result.ProcessedCount,
		result.DuplicateCount,
		result.TrashCount,
		result.ChunkCount,
		stats.Documents,
	)
	_ = b.reply(chatID, summary, mainKeyboard())
	b.runSourceExport(chatID, sourceID, "post_sync")
}

func (b *Bot) runSourceExport(chatID int64, sourceID string, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := b.exportService.ExportJSONL(ctx, export.JSONLRequest{
		SourceID:          &sourceID,
		Mode:              "documents",
		Format:            "txt_rag",
		IncludeDuplicates: true,
		IncludeTrash:      true,
	})
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	_ = b.reply(chatID, fmt.Sprintf("TXT RAG export готов.\nsource_id: `%s`\nreason: `%s`\nrows: %d", sourceID, reason, result.RowCount), mainKeyboard())
	b.sendFile(chatID, result.FilePath, "source_export")
	b.promptNextAction(chatID)
}

func (b *Bot) runHHExtraction(chatID int64, vacancyID string) {
	b.runHHExtractionWithDates(chatID, vacancyID, "", "")
}

func (b *Bot) runHHExtractionWithDates(chatID int64, vacancyID string, dateFrom, dateTo string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	if strings.TrimSpace(b.hhConfig.AccessToken) == "" && strings.TrimSpace(b.hhConfig.RefreshToken) == "" {
		b.sendError(chatID, errors.New("HH не настроен: добавьте HH_ACCESS_TOKEN или HH_REFRESH_TOKEN в .env"))
		return
	}

	extractionID := fmt.Sprintf("%s_%d", vacancyID, time.Now().Unix())
	outputDir := filepath.Join(b.hhConfig.OutputDir, extractionID)
	svc := hhservice.NewHHExtractionService(b.hhConfig, model.OSFileSystem{}, b.logger)
	manifest, files, err := svc.Run(ctx, model.ExtractionRequest{
		VacancyID:     vacancyID,
		DateFrom:      dateFrom,
		DateTo:        dateTo,
		OutputDir:     outputDir,
		SaveOriginals: b.hhConfig.SaveOriginalsDefault,
	})
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	summary := fmt.Sprintf(
		"HH extraction завершен.\nextraction_id: `%s`\nvacancy_id: `%s`\nfound: %d\nprocessed: %d\nsucceeded: %d\nfailed: %d",
		extractionID,
		vacancyID,
		manifest.TotalFound,
		manifest.Processed,
		manifest.Succeeded,
		manifest.Failed,
	)
	_ = b.reply(chatID, summary, mainKeyboard())

	for _, name := range preferredHHFiles(files) {
		b.sendFile(chatID, filepath.Join(outputDir, name), "hh_result")
	}
	b.promptNextAction(chatID)
}

func (b *Bot) runHHPublicSearch(chatID int64, rawQuery string, currentSession session) {
	req, err := parseHHPublicSearchQuery(rawQuery, currentSession.HHPublicDateFrom, currentSession.HHPublicDateTo)
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	svc := hhpublic.New(b.hhConfig, b.ingestionService, b.logger)
	result, err := svc.Import(ctx, hhpublic.ImportRequest{
		Text:     req.Text,
		DateFrom: req.DateFrom,
		DateTo:   req.DateTo,
		MaxItems: req.MaxItems,
		Title:    req.Title,
	})
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	summary := fmt.Sprintf(
		"HH глобальный поиск завершен.\nsource_id: `%s`\nquery: `%s`\nimported: %d\nprocessed: %d\nduplicates: %d\nchunks: %d\nhh_requests: %d",
		result.Source.ID,
		req.Text,
		result.ImportedCount,
		result.ProcessedCount,
		result.DuplicateCount,
		result.ChunkCount,
		result.FetchStats.RequestsMade,
	)
	_ = b.reply(chatID, summary, mainKeyboard())
	b.runSourceExport(chatID, result.Source.ID, "hh_public_search")
}

func (b *Bot) runHHPeopleSearch(chatID int64, rawQuery string, currentSession session) {
	req, err := parseHHPeopleSearchQuery(rawQuery, currentSession.HHPeopleMaxItems)
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	svc := hhresumesearch.New(b.hhConfig, b.ingestionService, b.logger)
	result, err := svc.Import(ctx, hhresumesearch.ImportRequest{
		Text:     req.Text,
		MaxItems: req.MaxItems,
		Title:    req.Title,
	})
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	summary := fmt.Sprintf(
		"HH глобальный поиск сотрудников завершен.\nsource_id: `%s`\nquery: `%s`\nimported: %d\nprocessed: %d\nduplicates: %d\nchunks: %d\nhh_requests: %d",
		result.Source.ID,
		req.Text,
		result.ImportedCount,
		result.ProcessedCount,
		result.DuplicateCount,
		result.ChunkCount,
		result.FetchStats.RequestsMade,
	)
	_ = b.reply(chatID, summary, mainKeyboard())
	b.runSourceExport(chatID, result.Source.ID, "hh_global_resume_search")
}

func (b *Bot) runHHAccessStatus(chatID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	svc := hhaccess.New(b.hhConfig, b.logger)
	status, err := svc.GetStatus(ctx)
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	lines := []string{
		"HH доступ и лимиты:",
		fmt.Sprintf("auth_type: `%s`", status.CurrentUser.AuthType),
		fmt.Sprintf("employer: `%s` (`%s`)", status.CurrentUser.Employer.Name, status.CurrentUser.Employer.ID),
		fmt.Sprintf("manager_id: `%s`", status.CurrentUser.Manager.ID),
		fmt.Sprintf("paid_services_active: `%d`", len(status.PayableActions.Items)),
	}

	if len(status.PayableActions.Items) == 0 {
		lines = append(lines, "Активных платных API-услуг HH сейчас нет.")
	} else {
		lines = append(lines, "", "Активные HH API-услуги:")
		for _, item := range status.PayableActions.Items {
			lines = append(lines, fmt.Sprintf("- `%s`: %s", item.ID, item.Description))
		}
	}

	if len(status.MethodAccess.Items) > 0 {
		lines = append(lines, "", "Доступ к группам платных методов:")
		for _, item := range status.MethodAccess.Items {
			state := "нет"
			if item.Access.HasAccess {
				state = "да"
			}
			lines = append(lines, fmt.Sprintf("- `%s`: %s -> `%s`", item.ID, item.Description, state))
		}
	}

	limit := status.ResumeLimits.Limits["resume_view"]
	spend := status.ResumeLimits.Spend["resume_view"]
	left := status.ResumeLimits.Left["resume_view"]
	lines = append(lines, "", fmt.Sprintf("resume_view: limit `%d`, spent `%d`, left `%d`", limit, spend, left))

	_ = b.reply(chatID, strings.Join(lines, "\n"), mainKeyboard())
	b.promptNextAction(chatID)
}

func (b *Bot) promptHHVacancySelection(chatID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	catalog, err := b.loadHHVacancyCatalog(ctx)
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	options := shortlistHHVacancies(catalog.Vacancies, 8)
	b.setSession(chatID, session{
		State:     stateAwaitHHVacancyID,
		HHOptions: options,
	})

	lines := []string{
		"Подтянул доступные вакансии HH.",
		"Пришлите `номер`, `vacancy_id` или текст вроде `Data scientist`.",
	}
	if len(options) == 0 {
		lines = append(lines, "Каталог пуст. Можно прислать `vacancy_id` вручную.")
	} else {
		lines = append(lines, "", "Ближайшие варианты:")
		lines = append(lines, formatHHVacancyOptions(options)...)
	}
	_ = b.reply(chatID, strings.Join(lines, "\n"), mainKeyboard())
}

func (b *Bot) resolveAndRunHHExtraction(chatID int64, rawQuery string) {
	query := strings.TrimSpace(rawQuery)
	if query == "" {
		_ = b.reply(chatID, "Нужен `номер`, `vacancy_id` или название вакансии.", mainKeyboard())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	catalog, err := b.loadHHVacancyCatalog(ctx)
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	currentSession := b.getSession(chatID)
	selected, candidates := resolveHHVacancyQuery(query, currentSession.HHOptions, catalog.Vacancies)
	if selected == nil {
		if len(candidates) == 0 {
			_ = b.reply(chatID, fmt.Sprintf("По запросу `%s` вакансии не найдены. Пришлите точнее `vacancy_id` или часть названия.", query), mainKeyboard())
			return
		}
		candidates = shortlistHHVacancies(candidates, 8)
		b.setSession(chatID, session{
			State:     stateAwaitHHVacancyID,
			HHOptions: candidates,
		})
		lines := []string{
			fmt.Sprintf("Нашел несколько совпадений по `%s`.", query),
			"Пришлите номер из списка или точный `vacancy_id`.",
			"",
			"Варианты:",
		}
		lines = append(lines, formatHHVacancyOptions(candidates)...)
		_ = b.reply(chatID, strings.Join(lines, "\n"), mainKeyboard())
		return
	}

	sess := b.getSession(chatID)
	sess.State = stateAwaitHHExtractionDate
	sess.HHSelectedVacancyID = selected.ID
	sess.HHSelectedVacancyName = selected.Name
	b.setSession(chatID, sess)

	_ = b.reply(chatID, fmt.Sprintf("Выбрана вакансия: `%s`\nЗа какой период выгружать отклики (новые резюме)?", selected.Name), hhPublicKeyboard())
}

func (b *Bot) handleHHExtractionDateSelection(msg *Message, text string) {
	query := strings.TrimSpace(text)
	var dateFrom, dateTo string

	switch query {
	case actionHHToday:
		dateFrom, dateTo = hhPresetRange(1)
	case actionHH7Days:
		dateFrom, dateTo = hhPresetRange(7)
	case actionHH30Days:
		dateFrom, dateTo = hhPresetRange(30)
	case "♾️ За все время":
		dateFrom, dateTo = "", ""
	default:
		_ = b.reply(msg.Chat.ID, "Выберите период с помощью клавиатуры.", hhPublicKeyboard())
		return
	}

	sess := b.getSession(msg.Chat.ID)
	b.setState(msg.Chat.ID, stateIdle)
	_ = b.reply(msg.Chat.ID, fmt.Sprintf("Запустил HH extraction для `%s` (vacancy_id `%s`) за выбранный период.", sess.HHSelectedVacancyName, sess.HHSelectedVacancyID), mainKeyboard())
	
	go b.runHHExtractionWithDates(msg.Chat.ID, sess.HHSelectedVacancyID, dateFrom, dateTo)
}

func (b *Bot) loadHHVacancyCatalog(ctx context.Context) (*model.HHVacancyCatalog, error) {
	if strings.TrimSpace(b.hhConfig.AccessToken) == "" && strings.TrimSpace(b.hhConfig.RefreshToken) == "" {
		return nil, errors.New("HH не настроен: добавьте HH_ACCESS_TOKEN или HH_REFRESH_TOKEN в .env")
	}
	return hhservice.NewHHVacancyCatalogService(b.hhConfig, b.logger).List(ctx)
}

func (b *Bot) sendSources(chatID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	items, err := b.repo.ListSources(ctx)
	if err != nil {
		b.sendError(chatID, err)
		return
	}
	if len(items) == 0 {
		_ = b.reply(chatID, "Источников пока нет.", mainKeyboard())
		return
	}

	limit := min(10, len(items))
	lines := []string{"Последние источники:"}
	for _, item := range items[:limit] {
		title := sourceLabel(item)
		lines = append(lines, fmt.Sprintf("- %s | type=%s | status=%s | id=%s", title, item.SourceType, item.Status, item.ID))
	}
	lines = append(lines, "", "Команды:", "/status <source_id>", "/sync <source_id>", "/export <source_id>")
	_ = b.reply(chatID, strings.Join(lines, "\n"), mainKeyboard())
}

func (b *Bot) sendSourceStatus(chatID int64, sourceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	source, err := b.repo.GetSource(ctx, sourceID)
	if err != nil {
		b.sendError(chatID, err)
		return
	}
	stats, err := b.repo.GetSourceStats(ctx, sourceID)
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	lines := []string{
		fmt.Sprintf("source_id: `%s`", source.ID),
		fmt.Sprintf("title: %s", sourceLabel(source)),
		fmt.Sprintf("type: %s", source.SourceType),
		fmt.Sprintf("status: %s", source.Status),
		fmt.Sprintf("raw_messages: %d", stats.RawMessages),
		fmt.Sprintf("documents: %d", stats.Documents),
		fmt.Sprintf("chunks: %d", stats.Chunks),
	}
	if source.LastError != nil && strings.TrimSpace(*source.LastError) != "" {
		lines = append(lines, fmt.Sprintf("last_error: %s", strings.TrimSpace(*source.LastError)))
	}
	_ = b.reply(chatID, strings.Join(lines, "\n"), mainKeyboard())
}

func (b *Bot) sendDocumentByID(chatID int64, documentID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	doc, err := b.repo.GetDocumentByID(ctx, documentID)
	if err != nil {
		b.sendError(chatID, err)
		return
	}

	filename := safeFileName(doc.ExternalDocID, "document.txt")
	if !strings.HasSuffix(strings.ToLower(filename), ".txt") {
		filename += ".txt"
	}

	if err := b.client.SendDocumentBytes(ctx, chatID, filename, []byte(doc.TextClean), fmt.Sprintf("document_id=%s", doc.ID)); err != nil {
		b.sendError(chatID, err)
		return
	}
	b.promptNextAction(chatID)
}

func (b *Bot) sendLatestDocumentForSource(ctx context.Context, chatID int64, sourceID string, fallbackName string) {
	docs, err := b.repo.ListDocumentsBySource(ctx, sourceID, 1)
	if err != nil || len(docs) == 0 {
		return
	}

	filename := fallbackName
	if custom := safeFileName(docs[0].ExternalDocID, "document.txt"); custom != "" {
		filename = custom
		if !strings.HasSuffix(strings.ToLower(filename), ".txt") {
			filename += ".txt"
		}
	}
	if err := b.client.SendDocumentBytes(ctx, chatID, filename, []byte(docs[0].TextClean), fmt.Sprintf("source_id=%s", sourceID)); err == nil {
		b.promptNextAction(chatID)
	}
}

func (b *Bot) sendFile(chatID int64, path string, caption string) {
	info, err := os.Stat(path)
	if err != nil {
		b.sendError(chatID, err)
		return
	}
	if info.Size() > maxTelegramDocumentBytes {
		_ = b.reply(chatID, fmt.Sprintf("Файл готов, но превышает лимит Telegram (%d MB): %s", maxTelegramDocumentBytes>>20, path), mainKeyboard())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := b.client.SendDocumentPath(ctx, chatID, path, caption); err != nil {
		b.sendError(chatID, err)
	}
}

func (b *Bot) downloadTelegramPayload(fileID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fileMeta, err := b.client.GetFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	return b.client.DownloadFile(ctx, fileMeta.FilePath)
}

func (b *Bot) reply(chatID int64, text string, keyboard *ReplyKeyboardMarkup) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return b.client.SendMessage(ctx, chatID, text, keyboard)
}

func (b *Bot) sendError(chatID int64, err error) {
	if err == nil {
		return
	}
	b.logger.Error("telegram_bot_operation_failed", "chat_id", chatID, "error", err)
	_ = b.reply(chatID, "Ошибка: "+strings.TrimSpace(err.Error()), mainKeyboard())
}

func (b *Bot) promptNextAction(chatID int64) {
	_ = b.reply(chatID, "Готово. Выберите следующее действие из меню ниже.", mainKeyboard())
}

func (b *Bot) promptFileUploadNextAction(chatID int64) {
	_ = b.reply(chatID, "Готово. Можно загрузить следующий файл или выбрать другое действие из меню ниже.", mainKeyboard())
}

func (b *Bot) isAllowed(user *User) bool {
	if len(b.allowedUsers) == 0 {
		return true
	}
	if user == nil {
		return false
	}
	_, ok := b.allowedUsers[user.ID]
	return ok
}

func (b *Bot) getState(chatID int64) state {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[chatID].State
}

func (b *Bot) setState(chatID int64, next state) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current := b.sessions[chatID]
	current.State = next
	if next == stateIdle {
		current.HHOptions = nil
		current.HHPublicDateFrom = ""
		current.HHPublicDateTo = ""
		current.HHPeopleMaxItems = 0
	}
	b.sessions[chatID] = current
}

func (b *Bot) getSession(chatID int64) session {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[chatID]
}

func (b *Bot) setSession(chatID int64, next session) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessions[chatID] = next
}

func (b *Bot) setHHPublicPreset(chatID int64, dateFrom string, dateTo string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	current := b.sessions[chatID]
	current.State = stateAwaitHHPublic
	current.HHPublicDateFrom = strings.TrimSpace(dateFrom)
	current.HHPublicDateTo = strings.TrimSpace(dateTo)
	b.sessions[chatID] = current
}

func mainKeyboard() *ReplyKeyboardMarkup {
	return &ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]KeyboardButton{
			{{Text: actionTGDocument}, {Text: actionTGSource}},
			{{Text: actionTGLinks}, {Text: actionYouTube}},
			{{Text: actionAudioFile}, {Text: actionJSONFile}},
			{{Text: actionFileUpload}, {Text: actionHH}},
			{{Text: actionSources}, {Text: actionHelp}},
			{{Text: actionCancel}},
		},
	}
}

func hhModeKeyboard() *ReplyKeyboardMarkup {
	return &ReplyKeyboardMarkup{
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
		Keyboard: [][]KeyboardButton{
			{{Text: actionHHCabinet}},
			{{Text: actionHHGlobal}},
			{{Text: actionHHPeople}},
			{{Text: actionHHStatus}},
			{{Text: actionCancel}},
		},
	}
}

func hhPublicKeyboard() *ReplyKeyboardMarkup {
	return &ReplyKeyboardMarkup{
		ResizeKeyboard: true,
		Keyboard: [][]KeyboardButton{
			{{Text: actionHHToday}, {Text: actionHH7Days}, {Text: actionHH30Days}},
			{{Text: "♾️ За все время"}},
			{{Text: actionCancel}},
		},
	}
}

func welcomeText(user *User) string {
	name := "коллега"
	if user != nil && strings.TrimSpace(user.FirstName) != "" {
		name = strings.TrimSpace(user.FirstName)
	}
	return fmt.Sprintf("%s, бот подключен к текущему ingestion service.\nВыберите тип парсинга с клавиатуры или используйте `/help`.", name)
}

func helpText() string {
	return strings.Join([]string{
		"Поддерживаемые сценарии:",
		"- `TG документ`: канал целиком в один RAG-документ.",
		"- `TG источник`: syncable Telegram source с последующим sync.",
		"- `TG ссылки`: импорт конкретных сообщений по ссылкам.",
		"- `YouTube`: URL -> аудио -> транскрибация.",
		"- `Аудио файл`: загрузка файла и расшифровка.",
		"- `JSON файл`: импорт JSON файлом или текстом.",
		"- `Загрузка файлов`: отправка файла в Telegram и обработка на сервере.",
		"- `HH резюме`: вакансии из кабинета, глобальный поиск вакансий и глобальный поиск сотрудников.",
		"",
		"Команды:",
		"- `/sources`",
		"- `/status <source_id>`",
		"- `/sync <source_id>`",
		"- `/export <source_id>`",
		"- `/document <document_id>`",
		"- `/hh <vacancy_id|название>`",
		"- `/cancel`",
	}, "\n")
}

func hhModePromptText() string {
	return strings.Join([]string{
		"Выберите HH режим:",
		"- `Вакансии из кабинета`: extraction резюме по вашим HH вакансиям.",
		"- `Глобальный поиск вакансий`: импорт публичных HH вакансий в RAG.",
		"- `Глобальный поиск сотрудников`: поиск резюме HH и импорт в RAG.",
		"- `Статус HH доступа`: активные услуги, доступ к платным методам и лимиты.",
	}, "\n")
}

func hhPublicSearchHelpText() string {
	return strings.Join([]string{
		"Пришлите запрос для глобального HH поиска.",
		"По умолчанию используется период `7 дней`.",
		"",
		"Быстрые кнопки ниже меняют период: `Сегодня`, `7 дней`, `30 дней`.",
		"",
		"Упрощенный формат:",
		"- `golang`",
		"- `data scientist`",
		"",
		"Расширенный формат:",
		"- `golang | 2026-04-01`",
		"- `golang | 2026-04-01 | 2026-04-07`",
		"- `golang | 2026-04-01 | 2026-04-07 | 300`",
		"",
		"Где последнее число, если указано, это `max_items`.",
	}, "\n")
}

func hhPeopleSearchHelpText() string {
	return strings.Join([]string{
		"Пришлите запрос для глобального HH поиска сотрудников.",
		"",
		"Упрощенный формат:",
		"- `golang`",
		"- `product manager`",
		"",
		"Расширенный формат:",
		"- `golang | 100`",
		"- `data scientist | 250`",
		"",
		"Где последнее число, если указано, это `max_items`.",
		"",
		"Важно: этот сценарий требует платного доступа HH к поиску резюме.",
	}, "\n")
}

func parseAllowedUsers(raw string) map[int64]struct{} {
	allowed := make(map[int64]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			continue
		}
		allowed[id] = struct{}{}
	}
	return allowed
}

func fileUploadHelpText() string {
	return strings.Join([]string{
		"Пришлите файл, и я обработаю его на сервере.",
		"Можно отправлять несколько файлов подряд, не выходя из этого режима.",
		"",
		"Поддерживаются:",
		"- документы: `pdf`, `docx`, `pptx`, `xlsx`, `epub`, `rtf`, `odt`",
		"- изображения: `png`, `jpg`, `jpeg`, `avif`, `gif`, `bmp`, `tif`, `tiff`, `webp`",
		"- текстовые файлы: `txt`, `md`, `json`, `jsonl`, `yaml`, `xml`, `html`, `csv`, `log`, а также кодовые файлы",
		"",
		"Картинки отправляйте как файл (`document`), а не как обычное сжатое фото.",
		"После загрузки я верну готовый RAG-export файлом.",
	}, "\n")
}

func isSupportedFileUpload(fileName string, mimeType string) bool {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(fileName)))
	if _, ok := fileUploadNativeExtensions[ext]; ok {
		return true
	}
	if _, ok := fileUploadTextExtensions[ext]; ok {
		return true
	}

	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	return strings.HasPrefix(mimeType, "text/") ||
		strings.Contains(mimeType, "html") ||
		strings.Contains(mimeType, "xml") ||
		mimeType == "application/json" ||
		mimeType == "application/javascript" ||
		mimeType == "application/x-javascript"
}

func isAudioDocument(fileName string, mimeType string) bool {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(fileName)))
	if _, ok := audioExtensions[ext]; ok {
		return true
	}

	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	return strings.HasPrefix(mimeType, "audio/") || strings.HasPrefix(mimeType, "video/")
}

func unsupportedFileUploadMessage(fileName string) string {
	return strings.Join([]string{
		fmt.Sprintf("Файл `%s` сейчас не поддерживается в режиме `📁 Загрузка файлов`.", fileName),
		"",
		"Поддерживаемые расширения:",
		"- документы: `pdf`, `docx`, `pptx`, `xlsx`, `epub`, `rtf`, `odt`",
		"- изображения: `png`, `jpg`, `jpeg`, `avif`, `gif`, `bmp`, `tif`, `tiff`, `webp`",
		"- текст и код: `txt`, `md`, `json`, `jsonl`, `yaml`, `xml`, `html`, `csv`, `log`, `py`, `go`, `js`, `ts` и похожие",
		"",
		"Если это аудио, используйте `🎙️ Аудио файл`.",
	}, "\n")
}

func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func extractTelegramFile(msg *Message) (string, string, error) {
	if msg == nil {
		return "", "", errors.New("пустое сообщение")
	}
	if msg.Document != nil {
		return msg.Document.FileID, safeFileName(msg.Document.FileName, "upload.bin"), nil
	}
	if msg.Audio != nil {
		return msg.Audio.FileID, safeFileName(msg.Audio.FileName, "audio.mp3"), nil
	}
	if msg.Voice != nil {
		return msg.Voice.FileID, "voice.ogg", nil
	}
	if msg.Video != nil {
		return msg.Video.FileID, safeFileName(msg.Video.FileName, "video.mp4"), nil
	}
	return "", "", errors.New("ожидаю audio/voice/video/document файл")
}

func preferredHHFiles(files []string) []string {
	if len(files) == 0 {
		return nil
	}

	preferredOrder := []string{
		"manifest.json",
		"combined_resumes.md",
		"combined_resumes.html",
		"combined_resumes.pdf",
	}
	available := make(map[string]struct{}, len(files))
	for _, file := range files {
		available[file] = struct{}{}
	}

	out := make([]string, 0, len(preferredOrder))
	for _, name := range preferredOrder {
		if _, ok := available[name]; ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func sourceLabel(source models.Source) string {
	switch {
	case source.Title != nil && strings.TrimSpace(*source.Title) != "":
		return strings.TrimSpace(*source.Title)
	case source.Username != nil && strings.TrimSpace(*source.Username) != "":
		return strings.TrimSpace(*source.Username)
	default:
		return source.URL
	}
}

func safeFileName(value string, fallback string) string {
	value = strings.TrimSpace(filepath.Base(value))
	if value == "." || value == "/" || value == "" {
		return fallback
	}
	return value
}

func ptrIfNotEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

type hhPublicSearchQuery struct {
	Text     string
	DateFrom string
	DateTo   string
	MaxItems int
	Title    string
}

type hhPeopleSearchQuery struct {
	Text     string
	MaxItems int
	Title    string
}

func parseHHPublicSearchQuery(raw string, presetFrom string, presetTo string) (hhPublicSearchQuery, error) {
	parts := strings.Split(raw, "|")
	for idx := range parts {
		parts[idx] = strings.TrimSpace(parts[idx])
	}
	if len(parts) == 0 || parts[0] == "" {
		return hhPublicSearchQuery{}, errors.New("пустой поисковый запрос HH")
	}

	req := hhPublicSearchQuery{
		Text:     parts[0],
		MaxItems: 300,
		Title:    "HH public vacancies: " + parts[0],
	}
	req.DateFrom = strings.TrimSpace(presetFrom)
	req.DateTo = strings.TrimSpace(presetTo)
	if len(parts) >= 2 && parts[1] != "" {
		req.DateFrom = parts[1]
		req.DateTo = parts[1]
	}
	if len(parts) >= 3 && parts[2] != "" {
		req.DateTo = parts[2]
	}
	if len(parts) >= 4 && parts[3] != "" {
		value, err := strconv.Atoi(parts[3])
		if err != nil || value <= 0 {
			return hhPublicSearchQuery{}, errors.New("max_items должен быть положительным числом")
		}
		req.MaxItems = value
	}
	return req, nil
}

func parseHHPeopleSearchQuery(raw string, presetMaxItems int) (hhPeopleSearchQuery, error) {
	parts := strings.Split(raw, "|")
	for idx := range parts {
		parts[idx] = strings.TrimSpace(parts[idx])
	}
	if len(parts) == 0 || parts[0] == "" {
		return hhPeopleSearchQuery{}, errors.New("пустой поисковый запрос HH по сотрудникам")
	}

	req := hhPeopleSearchQuery{
		Text:     parts[0],
		MaxItems: 100,
		Title:    "HH global resumes: " + parts[0],
	}
	if presetMaxItems > 0 {
		req.MaxItems = presetMaxItems
	}
	if len(parts) >= 2 && parts[1] != "" {
		value, err := strconv.Atoi(parts[1])
		if err != nil || value <= 0 {
			return hhPeopleSearchQuery{}, errors.New("max_items должен быть положительным числом")
		}
		req.MaxItems = value
	}
	return req, nil
}

func hhPresetRange(days int) (string, string) {
	if days <= 0 {
		days = 7
	}
	now := time.Now()
	to := now.Format("2006-01-02")
	from := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	return from, to
}

func (b *Bot) startHHPublicSearchFlow(chatID int64) {
	dateFrom, dateTo := hhPresetRange(7)
	b.setHHPublicPreset(chatID, dateFrom, dateTo)
	_ = b.reply(chatID, hhPublicSearchHelpText(), hhPublicKeyboard())
}

func (b *Bot) startHHPeopleSearchFlow(chatID int64) {
	current := b.getSession(chatID)
	current.State = stateAwaitHHPeople
	current.HHPeopleMaxItems = 100
	b.setSession(chatID, current)
	_ = b.reply(chatID, hhPeopleSearchHelpText(), mainKeyboard())
}

func shortlistHHVacancies(items []model.HHVacancyListItem, limit int) []model.HHVacancyListItem {
	if limit <= 0 || len(items) == 0 {
		return nil
	}

	scored := append([]model.HHVacancyListItem(nil), items...)
	sort.SliceStable(scored, func(i, j int) bool {
		left := hhStatusWeight(scored[i].Status)
		right := hhStatusWeight(scored[j].Status)
		if left != right {
			return left < right
		}
		if scored[i].Responses != scored[j].Responses {
			return scored[i].Responses > scored[j].Responses
		}
		if scored[i].Views != scored[j].Views {
			return scored[i].Views > scored[j].Views
		}
		return strings.ToLower(scored[i].Name) < strings.ToLower(scored[j].Name)
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

func resolveHHVacancyQuery(query string, options []model.HHVacancyListItem, all []model.HHVacancyListItem) (*model.HHVacancyListItem, []model.HHVacancyListItem) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	if idx, err := strconv.Atoi(query); err == nil && idx >= 1 && idx <= len(options) {
		selected := options[idx-1]
		return &selected, nil
	}

	for _, item := range all {
		if strings.EqualFold(strings.TrimSpace(item.ID), query) {
			selected := item
			return &selected, nil
		}
	}

	type scoredVacancy struct {
		item  model.HHVacancyListItem
		score int
	}

	normalizedQuery := normalizeHHQuery(query)
	queryTokens := splitHHTokens(normalizedQuery)
	if normalizedQuery == "" {
		return nil, nil
	}

	scored := make([]scoredVacancy, 0, len(all))
	for _, item := range all {
		score := hhVacancyMatchScore(item, normalizedQuery, queryTokens)
		if score > 0 {
			scored = append(scored, scoredVacancy{item: item, score: score})
		}
	}
	if len(scored) == 0 {
		return nil, nil
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if hhStatusWeight(scored[i].item.Status) != hhStatusWeight(scored[j].item.Status) {
			return hhStatusWeight(scored[i].item.Status) < hhStatusWeight(scored[j].item.Status)
		}
		if scored[i].item.Responses != scored[j].item.Responses {
			return scored[i].item.Responses > scored[j].item.Responses
		}
		return strings.ToLower(scored[i].item.Name) < strings.ToLower(scored[j].item.Name)
	})

	if len(scored) == 1 || scored[0].score >= scored[1].score+80 {
		selected := scored[0].item
		return &selected, nil
	}

	candidates := make([]model.HHVacancyListItem, 0, min(8, len(scored)))
	for _, item := range scored[:min(8, len(scored))] {
		candidates = append(candidates, item.item)
	}
	return nil, candidates
}

func formatHHVacancyOptions(items []model.HHVacancyListItem) []string {
	lines := make([]string, 0, len(items))
	for idx, item := range items {
		lines = append(lines, fmt.Sprintf(
			"%d. %s | %s | responses=%d | id=%s",
			idx+1,
			item.Name,
			item.Status,
			item.Responses,
			item.ID,
		))
	}
	return lines
}

func hhVacancyMatchScore(item model.HHVacancyListItem, query string, tokens []string) int {
	name := normalizeHHQuery(item.Name)
	employer := normalizeHHQuery(item.EmployerName)
	id := strings.TrimSpace(item.ID)

	score := 0
	matched := false
	switch {
	case strings.EqualFold(id, query):
		score += 1000
		matched = true
	case name == query:
		score += 500
		matched = true
	case strings.Contains(name, query):
		score += 320
		matched = true
	case strings.Contains(employer, query):
		score += 140
		matched = true
	}

	matchedTokens := 0
	for _, token := range tokens {
		switch {
		case strings.Contains(name, token):
			score += 70
			matchedTokens++
			matched = true
		case strings.Contains(employer, token):
			score += 25
			matchedTokens++
			matched = true
		}
	}
	if !matched {
		return 0
	}
	if len(tokens) > 0 && matchedTokens == len(tokens) {
		score += 120
	}
	score += max(0, 25-hhStatusWeight(item.Status)*5)
	return score
}

func normalizeHHQuery(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"\n", " ",
		"\t", " ",
		"-", " ",
		"_", " ",
		"/", " ",
		"\\", " ",
		".", " ",
		",", " ",
		":", " ",
		";", " ",
		"(", " ",
		")", " ",
	)
	value = replacer.Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func splitHHTokens(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Fields(value)
}

func hhStatusWeight(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return 0
	case "hidden":
		return 1
	case "archived":
		return 2
	default:
		return 3
	}
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
