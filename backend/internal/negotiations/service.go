package negotiations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"

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
