package negotiations

import (
	"encoding/json"
	"fmt"
	"strings"

	"tg-rag-ingestor/backend/internal/model"
)

type negotiationsPage struct {
	Found       int                           `json:"found"`
	Pages       int                           `json:"pages"`
	PerPage     int                           `json:"per_page"`
	Page        int                           `json:"page"`
	Collections []model.NegotiationCollection `json:"collections"`
	Items       []json.RawMessage             `json:"items"`
}

type negotiationItemDTO struct {
	ID          string      `json:"id"`
	UpdatedAt   string      `json:"updated_at"`
	CreatedAt   string      `json:"created_at"`
	Message     string      `json:"message"`
	MessagesURL string      `json:"messages_url"`
	ChatID      json.Number `json:"chat_id"`
	Resume      struct {
		ID           string `json:"id"`
		URL          string `json:"url"`
		AlternateURL string `json:"alternate_url"`
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		MiddleName   string `json:"middle_name"`
	} `json:"resume"`
	Applicant struct {
		ID         string `json:"id"`
		FirstName  string `json:"first_name"`
		LastName   string `json:"last_name"`
		MiddleName string `json:"middle_name"`
		Name       string `json:"name"`
	} `json:"applicant"`
}

type messagesResponse struct {
	Items []messageItemDTO `json:"items"`
}

type messageItemDTO struct {
	Text   string `json:"text"`
	Author struct {
		ParticipantType string `json:"participant_type"`
	} `json:"author"`
}

type chatMessagesResponse struct {
	Messages []chatMessageItemDTO `json:"messages"`
	Items    []chatMessageItemDTO `json:"items"`
}

type chatMessageItemDTO struct {
	Payload struct {
		Text string `json:"text"`
	} `json:"payload"`
	SenderDisplayInfo struct {
		Role string `json:"role"`
	} `json:"sender_display_info"`
}

func (r chatMessagesResponse) list() []chatMessageItemDTO {
	if len(r.Messages) > 0 {
		return r.Messages
	}
	return r.Items
}

// ADAPT_TO_REAL_API_RESPONSE
func parseNegotiationsPage(data []byte) (*negotiationsPage, error) {
	var page negotiationsPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("parse negotiations page: %w", err)
	}
	return &page, nil
}

// ADAPT_TO_REAL_API_RESPONSE
func mapNegotiationCandidate(raw json.RawMessage) (*model.NegotiationCandidate, error) {
	var dto negotiationItemDTO
	if err := json.Unmarshal(raw, &dto); err != nil {
		return nil, fmt.Errorf("parse negotiation item: %w", err)
	}

	candidateID := dto.Applicant.ID
	if candidateID == "" {
		candidateID = dto.ID
	}
	resumeID := dto.Resume.ID
	if resumeID == "" {
		return nil, nil
	}
	fio := model.BuildFIO(dto.Resume.FirstName, dto.Resume.LastName, dto.Resume.MiddleName)
	if fio == "[ФИО НЕДОСТУПНО]" {
		fio = model.BuildFIO(dto.Applicant.FirstName, dto.Applicant.LastName, dto.Applicant.MiddleName)
	}
	if fio == "[ФИО НЕДОСТУПНО]" && dto.Applicant.Name != "" {
		fio = dto.Applicant.Name
	}
	updatedAt := dto.UpdatedAt
	if updatedAt == "" {
		updatedAt = dto.CreatedAt
	}

	return &model.NegotiationCandidate{
		CandidateID:   candidateID,
		NegotiationID: dto.ID,
		ResumeID:      resumeID,
		ResumeAPIURL:  strings.TrimSpace(dto.Resume.URL),
		FIO:           fio,
		UpdatedAt:     updatedAt,
		CoverLetter:   strings.TrimSpace(dto.Message),
		MessagesURL:   dto.MessagesURL,
		ChatID:        strings.TrimSpace(dto.ChatID.String()),
	}, nil
}
