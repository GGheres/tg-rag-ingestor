package tgbot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const telegramAPIBase = "https://api.telegram.org"

type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

type ReplyKeyboardMarkup struct {
	Keyboard        [][]KeyboardButton `json:"keyboard"`
	ResizeKeyboard  bool               `json:"resize_keyboard,omitempty"`
	OneTimeKeyboard bool               `json:"one_time_keyboard,omitempty"`
}

type KeyboardButton struct {
	Text string `json:"text"`
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

type Message struct {
	MessageID int64         `json:"message_id"`
	From      *User         `json:"from,omitempty"`
	Chat      Chat          `json:"chat"`
	Text      string        `json:"text,omitempty"`
	Document  *Document     `json:"document,omitempty"`
	Audio     *Audio        `json:"audio,omitempty"`
	Voice     *Voice        `json:"voice,omitempty"`
	Video     *Video        `json:"video,omitempty"`
	Caption   string        `json:"caption,omitempty"`
	Date      int64         `json:"date"`
	Entities  []MessagePart `json:"entities,omitempty"`
}

type MessagePart struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	Bot       bool   `json:"is_bot,omitempty"`
}

type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
	Title    string `json:"title,omitempty"`
}

type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Audio struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Voice struct {
	FileID   string `json:"file_id"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type Video struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size,omitempty"`
}

type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
	Result      T      `json:"result"`
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSeconds int) ([]Update, error) {
	values := url.Values{}
	values.Set("offset", strconv.FormatInt(offset, 10))
	values.Set("timeout", strconv.Itoa(timeoutSeconds))
	values.Set("allowed_updates", `["message"]`)

	var resp apiResponse[[]Update]
	if err := c.doJSON(ctx, http.MethodGet, "getUpdates?"+values.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("telegram getUpdates failed: %s", resp.Description)
	}
	return resp.Result, nil
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, keyboard *ReplyKeyboardMarkup) error {
	payload := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if keyboard != nil {
		payload["reply_markup"] = keyboard
	}

	var resp apiResponse[Message]
	if err := c.doJSON(ctx, http.MethodPost, "sendMessage", payload, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("telegram sendMessage failed: %s", resp.Description)
	}
	return nil
}

func (c *Client) SendDocumentPath(ctx context.Context, chatID int64, path string, caption string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return c.sendDocument(ctx, chatID, filepath.Base(path), file, caption)
}

func (c *Client) SendDocumentBytes(ctx context.Context, chatID int64, filename string, data []byte, caption string) error {
	return c.sendDocument(ctx, chatID, filename, bytes.NewReader(data), caption)
}

func (c *Client) GetFile(ctx context.Context, fileID string) (File, error) {
	payload := map[string]any{"file_id": fileID}
	var resp apiResponse[File]
	if err := c.doJSON(ctx, http.MethodPost, "getFile", payload, &resp); err != nil {
		return File{}, err
	}
	if !resp.OK {
		return File{}, fmt.Errorf("telegram getFile failed: %s", resp.Description)
	}
	return resp.Result, nil
}

func (c *Client) DownloadFile(ctx context.Context, filePath string) ([]byte, error) {
	url := fmt.Sprintf("%s/file/bot%s/%s", telegramAPIBase, c.token, strings.TrimPrefix(filePath, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("telegram file download failed: http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) sendDocument(ctx context.Context, chatID int64, filename string, reader io.Reader, caption string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		return err
	}
	if strings.TrimSpace(caption) != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}

	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, reader); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.methodURL("sendDocument"), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("telegram sendDocument failed: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var decoded apiResponse[Message]
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if !decoded.OK {
		return fmt.Errorf("telegram sendDocument failed: %s", decoded.Description)
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method string, apiMethod string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.methodURL(apiMethod), body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("telegram api failed: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) methodURL(method string) string {
	return fmt.Sprintf("%s/bot%s/%s", telegramAPIBase, c.token, method)
}
