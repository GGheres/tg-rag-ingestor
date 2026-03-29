package hh

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
)

// NegotiationsService fetches negotiations (responses) by vacancy.
type NegotiationsService struct {
	client *Client
	logger *slog.Logger
}

func NewNegotiationsService(client *Client, logger *slog.Logger) *NegotiationsService {
	return &NegotiationsService{client: client, logger: logger}
}

// FetchAllByVacancy fetches all negotiation items for a vacancy, going through
// collections and handling pagination. Returns deduplicated items by resume ID.
func (s *NegotiationsService) FetchAllByVacancy(ctx context.Context, vacancyID string) ([]NegotiationItem, error) {
	s.logger.Info("fetching negotiations", "vacancy_id", vacancyID)

	// Step 1: Get top-level negotiations to discover collections.
	q := url.Values{}
	q.Set("vacancy_id", vacancyID)
	q.Set("per_page", strconv.Itoa(defaultPerPage))
	q.Set("page", "0")

	body, status, err := s.client.Get(ctx, "/negotiations", q)
	if err != nil {
		return nil, fmt.Errorf("fetch negotiations: %w", err)
	}
	if status != http.StatusOK {
		apiErr := ParseAPIError(body)
		return nil, fmt.Errorf("negotiations API error: status=%d err=%+v", status, apiErr)
	}

	resp, err := ParseNegotiationsResponse(body)
	if err != nil {
		return nil, err
	}

	s.logger.Info("negotiations overview",
		"found", resp.Found,
		"pages", resp.Pages,
		"collections_count", len(resp.Collections),
		"items_in_first_page", len(resp.Items),
	)

	seen := make(map[string]bool)
	var allItems []NegotiationItem

	addItems := func(items []NegotiationItem) {
		for _, item := range items {
			if item.Resume == nil {
				continue
			}
			if seen[item.Resume.ID] {
				continue
			}
			seen[item.Resume.ID] = true
			allItems = append(allItems, item)
		}
	}

	// Step 2: If there are collections, fetch each collection with pagination.
	if len(resp.Collections) > 0 {
		for _, col := range resp.Collections {
			if col.Count == 0 {
				s.logger.Debug("skipping empty collection", "id", col.ID, "name", col.Name)
				continue
			}
			s.logger.Info("fetching collection", "id", col.ID, "name", col.Name, "count", col.Count, "url", col.URL)

			items, err := s.fetchCollectionAll(ctx, col.URL)
			if err != nil {
				s.logger.Error("failed to fetch collection", "id", col.ID, "error", err)
				// partial success: continue with other collections
				continue
			}
			addItems(items)
		}
	} else {
		// No collections — use top-level items with pagination.
		addItems(resp.Items)
		for page := 1; page < resp.Pages; page++ {
			items, err := s.fetchNegotiationsPage(ctx, vacancyID, page)
			if err != nil {
				s.logger.Error("failed to fetch page", "page", page, "error", err)
				continue
			}
			addItems(items)
		}
	}

	s.logger.Info("total unique candidates found", "count", len(allItems))
	return allItems, nil
}

func (s *NegotiationsService) fetchNegotiationsPage(ctx context.Context, vacancyID string, page int) ([]NegotiationItem, error) {
	q := url.Values{}
	q.Set("vacancy_id", vacancyID)
	q.Set("per_page", strconv.Itoa(defaultPerPage))
	q.Set("page", strconv.Itoa(page))

	body, status, err := s.client.Get(ctx, "/negotiations", q)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("negotiations page %d: status=%d", page, status)
	}

	resp, err := ParseNegotiationsResponse(body)
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (s *NegotiationsService) fetchCollectionAll(ctx context.Context, collectionURL string) ([]NegotiationItem, error) {
	var allItems []NegotiationItem

	for page := 0; ; page++ {
		q := url.Values{}
		q.Set("per_page", strconv.Itoa(defaultPerPage))
		q.Set("page", strconv.Itoa(page))

		body, status, err := s.client.GetURL(ctx, collectionURL, q)
		if err != nil {
			return allItems, fmt.Errorf("collection page %d: %w", page, err)
		}
		if status != http.StatusOK {
			return allItems, fmt.Errorf("collection page %d: status=%d", page, status)
		}

		resp, err := ParseCollectionResponse(body)
		if err != nil {
			return allItems, err
		}

		allItems = append(allItems, resp.Items...)

		if page+1 >= resp.Pages || len(resp.Items) == 0 {
			break
		}
	}

	return allItems, nil
}
