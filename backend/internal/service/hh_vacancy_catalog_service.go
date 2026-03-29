package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"

	"tg-rag-ingestor/backend/internal/auth"
	"tg-rag-ingestor/backend/internal/hhclient"
	"tg-rag-ingestor/backend/internal/model"
)

type HHVacancyCatalogService struct {
	cfg    model.HHConfig
	logger *slog.Logger
	auth   *auth.Manager
}

func NewHHVacancyCatalogService(cfg model.HHConfig, logger *slog.Logger) *HHVacancyCatalogService {
	cfg = cfg.WithDefaults()
	return &HHVacancyCatalogService{
		cfg:    cfg,
		logger: logger,
		auth:   auth.NewManager(cfg, logger),
	}
}

func (s *HHVacancyCatalogService) List(ctx context.Context) (*model.HHVacancyCatalog, error) {
	client := hhclient.New(s.cfg, s.auth, s.logger)

	var accountsResp struct {
		CurrentAccountID string `json:"current_account_id"`
		PrimaryAccountID string `json:"primary_account_id"`
		Items            []struct {
			ID       string `json:"id"`
			Employer struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"employer"`
		} `json:"items"`
	}
	if _, err := client.GetJSON(ctx, "/manager_accounts/mine", nil, &accountsResp); err != nil {
		return nil, fmt.Errorf("fetch manager accounts: %w", err)
	}

	catalog := &model.HHVacancyCatalog{
		Accounts:         make([]model.HHManagerAccount, 0, len(accountsResp.Items)),
		CurrentAccountID: strings.TrimSpace(accountsResp.CurrentAccountID),
		Vacancies:        make([]model.HHVacancyListItem, 0),
	}

	for _, account := range accountsResp.Items {
		accountID := strings.TrimSpace(account.ID)
		employerID := strings.TrimSpace(account.Employer.ID)
		employerName := strings.TrimSpace(account.Employer.Name)
		if accountID == "" || employerID == "" {
			continue
		}

		catalog.Accounts = append(catalog.Accounts, model.HHManagerAccount{
			ID:           accountID,
			EmployerID:   employerID,
			EmployerName: employerName,
			IsCurrent:    accountID == catalog.CurrentAccountID,
			IsPrimary:    accountID == strings.TrimSpace(accountsResp.PrimaryAccountID),
		})

		accountCfg := s.cfg
		accountCfg.ManagerAccountID = accountID
		accountClient := hhclient.New(accountCfg, s.auth, s.logger)

		activeItems, err := s.listActiveVacancies(ctx, accountClient, accountID, employerID, employerName)
		if err != nil {
			return nil, err
		}
		archivedItems, err := s.listArchivedVacancies(ctx, accountClient, accountID, employerID, employerName)
		if err != nil {
			return nil, err
		}
		hiddenItems, err := s.listHiddenVacancies(ctx, accountClient, accountID, employerID, employerName)
		if err != nil {
			return nil, err
		}

		catalog.Vacancies = append(catalog.Vacancies, activeItems...)
		catalog.Vacancies = append(catalog.Vacancies, archivedItems...)
		catalog.Vacancies = append(catalog.Vacancies, hiddenItems...)
	}

	sort.Slice(catalog.Accounts, func(i, j int) bool {
		if catalog.Accounts[i].IsCurrent != catalog.Accounts[j].IsCurrent {
			return catalog.Accounts[i].IsCurrent
		}
		return catalog.Accounts[i].EmployerName < catalog.Accounts[j].EmployerName
	})
	sort.Slice(catalog.Vacancies, func(i, j int) bool {
		if catalog.Vacancies[i].EmployerName != catalog.Vacancies[j].EmployerName {
			return catalog.Vacancies[i].EmployerName < catalog.Vacancies[j].EmployerName
		}
		if catalog.Vacancies[i].Status != catalog.Vacancies[j].Status {
			return vacancyStatusWeight(catalog.Vacancies[i].Status) < vacancyStatusWeight(catalog.Vacancies[j].Status)
		}
		return strings.ToLower(catalog.Vacancies[i].Name) < strings.ToLower(catalog.Vacancies[j].Name)
	})

	return catalog, nil
}

func (s *HHVacancyCatalogService) listActiveVacancies(
	ctx context.Context,
	client *hhclient.Client,
	accountID, employerID, employerName string,
) ([]model.HHVacancyListItem, error) {
	type activeCounters struct {
		Responses float64 `json:"responses"`
		Views     float64 `json:"views"`
	}
	type activeItem struct {
		ID           string         `json:"id"`
		Name         string         `json:"name"`
		AlternateURL string         `json:"alternate_url"`
		PublishedAt  string         `json:"published_at"`
		Counters     activeCounters `json:"counters"`
	}
	type response struct {
		Found   int          `json:"found"`
		Page    int          `json:"page"`
		Pages   int          `json:"pages"`
		PerPage int          `json:"per_page"`
		Items   []activeItem `json:"items"`
	}

	return s.fetchVacancies(ctx, client, fmt.Sprintf("/employers/%s/vacancies/active", employerID), url.Values{
		"per_page":       []string{"50"},
		"all_accessible": []string{"true"},
	}, func(raw any) model.HHVacancyListItem {
		item := raw.(activeItem)
		return model.HHVacancyListItem{
			ID:               strings.TrimSpace(item.ID),
			Name:             strings.TrimSpace(item.Name),
			Status:           "active",
			ManagerAccountID: accountID,
			EmployerID:       employerID,
			EmployerName:     employerName,
			AlternateURL:     strings.TrimSpace(item.AlternateURL),
			PublishedAt:      strings.TrimSpace(item.PublishedAt),
			Responses:        int(item.Counters.Responses),
			Views:            int(item.Counters.Views),
		}
	}, func() paginationResult[any] {
		return paginationResult[any]{
			newPage: func() any { return &response{} },
			pageInfo: func(raw any) (int, int) {
				resp := raw.(*response)
				return resp.Page, resp.Pages
			},
			items: func(raw any) []any {
				resp := raw.(*response)
				items := make([]any, 0, len(resp.Items))
				for _, item := range resp.Items {
					items = append(items, item)
				}
				return items
			},
		}
	})
}

func (s *HHVacancyCatalogService) listArchivedVacancies(
	ctx context.Context,
	client *hhclient.Client,
	accountID, employerID, employerName string,
) ([]model.HHVacancyListItem, error) {
	type archiveCounters struct {
		Responses float64 `json:"responses"`
	}
	type archiveItem struct {
		ID           string          `json:"id"`
		Name         string          `json:"name"`
		AlternateURL string          `json:"alternate_url"`
		CreatedAt    string          `json:"created_at"`
		ArchivedAt   string          `json:"archived_at"`
		Counters     archiveCounters `json:"counters"`
	}
	type response struct {
		Found   int           `json:"found"`
		Page    int           `json:"page"`
		Pages   int           `json:"pages"`
		PerPage int           `json:"per_page"`
		Items   []archiveItem `json:"items"`
	}

	return s.fetchVacancies(ctx, client, fmt.Sprintf("/employers/%s/vacancies/archived", employerID), url.Values{
		"per_page": []string{"1000"},
	}, func(raw any) model.HHVacancyListItem {
		item := raw.(archiveItem)
		return model.HHVacancyListItem{
			ID:               strings.TrimSpace(item.ID),
			Name:             strings.TrimSpace(item.Name),
			Status:           "archived",
			ManagerAccountID: accountID,
			EmployerID:       employerID,
			EmployerName:     employerName,
			AlternateURL:     strings.TrimSpace(item.AlternateURL),
			PublishedAt:      strings.TrimSpace(item.CreatedAt),
			ArchivedAt:       strings.TrimSpace(item.ArchivedAt),
			Responses:        int(item.Counters.Responses),
		}
	}, func() paginationResult[any] {
		return paginationResult[any]{
			newPage: func() any { return &response{} },
			pageInfo: func(raw any) (int, int) {
				resp := raw.(*response)
				return resp.Page, resp.Pages
			},
			items: func(raw any) []any {
				resp := raw.(*response)
				items := make([]any, 0, len(resp.Items))
				for _, item := range resp.Items {
					items = append(items, item)
				}
				return items
			},
		}
	})
}

func (s *HHVacancyCatalogService) listHiddenVacancies(
	ctx context.Context,
	client *hhclient.Client,
	accountID, employerID, employerName string,
) ([]model.HHVacancyListItem, error) {
	type hiddenCounters struct {
		Responses float64 `json:"responses"`
	}
	type hiddenItem struct {
		ID           string         `json:"id"`
		Name         string         `json:"name"`
		AlternateURL string         `json:"alternate_url"`
		CreatedAt    string         `json:"created_at"`
		ArchivedAt   string         `json:"archived_at"`
		Counters     hiddenCounters `json:"counters"`
	}
	type response struct {
		Found   int          `json:"found"`
		Page    int          `json:"page"`
		Pages   int          `json:"pages"`
		PerPage int          `json:"per_page"`
		Items   []hiddenItem `json:"items"`
	}

	return s.fetchVacancies(ctx, client, fmt.Sprintf("/employers/%s/vacancies/hidden", employerID), url.Values{
		"per_page": []string{"1000"},
	}, func(raw any) model.HHVacancyListItem {
		item := raw.(hiddenItem)
		return model.HHVacancyListItem{
			ID:               strings.TrimSpace(item.ID),
			Name:             strings.TrimSpace(item.Name),
			Status:           "hidden",
			ManagerAccountID: accountID,
			EmployerID:       employerID,
			EmployerName:     employerName,
			AlternateURL:     strings.TrimSpace(item.AlternateURL),
			PublishedAt:      strings.TrimSpace(item.CreatedAt),
			ArchivedAt:       strings.TrimSpace(item.ArchivedAt),
			Responses:        int(item.Counters.Responses),
		}
	}, func() paginationResult[any] {
		return paginationResult[any]{
			newPage: func() any { return &response{} },
			pageInfo: func(raw any) (int, int) {
				resp := raw.(*response)
				return resp.Page, resp.Pages
			},
			items: func(raw any) []any {
				resp := raw.(*response)
				items := make([]any, 0, len(resp.Items))
				for _, item := range resp.Items {
					items = append(items, item)
				}
				return items
			},
		}
	})
}

type paginationResult[T any] struct {
	newPage  func() T
	pageInfo func(T) (int, int)
	items    func(T) []any
}

func (s *HHVacancyCatalogService) fetchVacancies(
	ctx context.Context,
	client *hhclient.Client,
	path string,
	baseQuery url.Values,
	mapItem func(any) model.HHVacancyListItem,
	newPagination func() paginationResult[any],
) ([]model.HHVacancyListItem, error) {
	result := make([]model.HHVacancyListItem, 0)
	pager := newPagination()
	for page := 0; ; page++ {
		query := cloneValues(baseQuery)
		query.Set("page", fmt.Sprintf("%d", page))
		rawPage := pager.newPage()
		if _, err := client.GetJSON(ctx, path, query, rawPage); err != nil {
			return nil, fmt.Errorf("fetch vacancies page %d for %s: %w", page, path, err)
		}

		for _, item := range pager.items(rawPage) {
			result = append(result, mapItem(item))
		}

		currentPage, totalPages := pager.pageInfo(rawPage)
		if totalPages <= 0 || currentPage >= totalPages-1 {
			break
		}
	}
	return result, nil
}

func cloneValues(src url.Values) url.Values {
	dst := make(url.Values, len(src))
	for key, values := range src {
		cloned := make([]string, len(values))
		copy(cloned, values)
		dst[key] = cloned
	}
	return dst
}

func vacancyStatusWeight(status string) int {
	switch status {
	case "active":
		return 0
	case "archived":
		return 1
	case "hidden":
		return 2
	default:
		return 3
	}
}
