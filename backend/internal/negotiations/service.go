package negotiations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"tg-rag-ingestor/backend/internal/hhclient"
	"tg-rag-ingestor/backend/internal/model"
)

type Service struct {
	client *hhclient.Client
	logger *slog.Logger
}

func New(client *hhclient.Client, logger *slog.Logger) *Service {
	return &Service{
		client: client,
		logger: logger,
	}
}

func (s *Service) FetchByVacancy(ctx context.Context, vacancyID string) ([]model.NegotiationCandidate, error) {
	firstPage, err := s.fetchNegotiationsPage(ctx, vacancyID, 0)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	out := make([]model.NegotiationCandidate, 0, firstPage.Found)
	appendCandidates := func(rawItems []json.RawMessage) {
		for _, raw := range rawItems {
			item, mapErr := mapNegotiationCandidate(raw)
			if mapErr != nil {
				s.logger.Warn("skip negotiation item: map failed", "error", mapErr)
				continue
			}
			if item == nil || item.ResumeID == "" {
				continue
			}
			key := item.ResumeID
			if key == "" {
				key = item.CandidateID
			}
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, *item)
		}
	}

	appendCandidates(firstPage.Items)

	if len(firstPage.Collections) > 0 {
		for _, collection := range firstPage.Collections {
			if collection.URL == "" {
				continue
			}
			if err := s.fetchCollection(ctx, collection.URL, appendCandidates); err != nil {
				s.logger.Warn("failed to fetch collection, continue partial success",
					"collection", collection.Name,
					"url", collection.URL,
					"error", err,
				)
			}
		}
	} else {
		for page := 1; page < firstPage.Pages; page++ {
			nextPage, pageErr := s.fetchNegotiationsPage(ctx, vacancyID, page)
			if pageErr != nil {
				s.logger.Warn("failed to fetch negotiations page, continue partial success",
					"page", page,
					"error", pageErr,
				)
				continue
			}
			appendCandidates(nextPage.Items)
		}
	}

	return out, nil
}

func (s *Service) fetchNegotiationsPage(ctx context.Context, vacancyID string, page int) (*negotiationsPage, error) {
	q := url.Values{}
	q.Set("vacancy_id", vacancyID)
	q.Set("per_page", strconv.Itoa(model.DefaultHHPerPage))
	q.Set("page", strconv.Itoa(page))

	var parsed negotiationsPage
	status, err := s.client.GetJSON(ctx, "/negotiations", q, &parsed)
	if err != nil {
		return nil, fmt.Errorf("fetch negotiations page %d failed: status=%d err=%w", page, status, err)
	}
	return &parsed, nil
}

// FetchCoverLetter fetches the applicant's initial cover letter.
// HH deprecated the old negotiation messages API in favor of chats, so prefer chat_id when available.
func (s *Service) FetchCoverLetter(ctx context.Context, candidate model.NegotiationCandidate) (string, error) {
	if chatID := strings.TrimSpace(candidate.ChatID); chatID != "" {
		text, err := s.fetchChatCoverLetter(ctx, chatID)
		if err == nil {
			return text, nil
		}
		s.logger.Warn("failed to fetch cover letter via chat api, falling back to legacy messages",
			"negotiation_id", candidate.NegotiationID,
			"chat_id", chatID,
			"error", err,
		)
	}
	return s.fetchLegacyCoverLetter(ctx, candidate.NegotiationID, candidate.MessagesURL)
}

func (s *Service) fetchChatCoverLetter(ctx context.Context, chatID string) (string, error) {
	q := url.Values{}
	q.Set("order", "next")
	q.Set("limit", "50")

	var msgs chatMessagesResponse
	status, err := s.client.GetJSON(ctx, "/common/chats/"+chatID+"/messages", q, &msgs)
	if err != nil {
		return "", fmt.Errorf("fetch chat messages: status=%d err=%w", status, err)
	}
	return extractInitialApplicantChatMessage(msgs.list()), nil
}

func (s *Service) fetchLegacyCoverLetter(ctx context.Context, negotiationID, messagesURL string) (string, error) {
	target := strings.TrimSpace(messagesURL)
	if target == "" {
		target = "/negotiations/" + negotiationID + "/messages"
	}

	q := url.Values{}
	q.Set("with_text_only", "true")
	q.Set("per_page", "20")
	q.Set("page", "0")

	var msgs messagesResponse
	var status int
	var err error
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		status, err = s.client.GetJSONURL(ctx, target, q, &msgs)
	} else {
		status, err = s.client.GetJSON(ctx, target, q, &msgs)
	}
	if err != nil {
		return "", fmt.Errorf("fetch messages: status=%d err=%w", status, err)
	}
	return extractInitialApplicantLegacyMessage(msgs.Items), nil
}

func extractInitialApplicantChatMessage(items []chatMessageItemDTO) string {
	for _, msg := range items {
		text := strings.TrimSpace(msg.Payload.Text)
		if text == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(msg.SenderDisplayInfo.Role), "APPLICANT") {
			return ""
		}
		return text
	}
	return ""
}

func extractInitialApplicantLegacyMessage(items []messageItemDTO) string {
	for _, msg := range items {
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(msg.Author.ParticipantType), "applicant") {
			return ""
		}
		return text
	}
	return ""
}

func (s *Service) fetchCollection(ctx context.Context, collectionURL string, onItems func([]json.RawMessage)) error {
	for page := 0; ; page++ {
		q := url.Values{}
		q.Set("per_page", strconv.Itoa(model.DefaultHHPerPage))
		q.Set("page", strconv.Itoa(page))

		var parsed negotiationsPage
		status, err := s.client.GetJSONURL(ctx, collectionURL, q, &parsed)
		if err != nil {
			return fmt.Errorf("fetch collection page %d failed: status=%d err=%w", page, status, err)
		}
		onItems(parsed.Items)

		if page+1 >= parsed.Pages || len(parsed.Items) == 0 {
			return nil
		}
	}
}
