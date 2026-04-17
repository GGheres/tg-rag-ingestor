package hhaccess

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"tg-rag-ingestor/backend/internal/auth"
	"tg-rag-ingestor/backend/internal/hhclient"
	"tg-rag-ingestor/backend/internal/model"
)

type Service struct {
	client *hhclient.Client
	logger *slog.Logger
}

type CurrentUser struct {
	AuthType   string         `json:"auth_type"`
	IsEmployer bool           `json:"is_employer"`
	Employer   *EmployerRef   `json:"employer"`
	Manager    *ManagerRef    `json:"manager"`
	Email      string         `json:"email"`
	Raw        map[string]any `json:"raw,omitempty"`
}

type EmployerRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ManagerRef struct {
	ID                  string `json:"id"`
	HasAdminRights      bool   `json:"has_admin_rights"`
	HasMultipleAccounts bool   `json:"has_multiple_manager_accounts"`
	IsMainContactPerson bool   `json:"is_main_contact_person"`
	ManagerSettingsURL  string `json:"manager_settings_url"`
}

type PayableActionsResponse struct {
	Items []PayableAction `json:"items"`
}

type PayableAction struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Access      struct {
		HasAccess bool `json:"has_access"`
	} `json:"access"`
}

type MethodAccessResponse struct {
	Items []MethodAccessItem `json:"items"`
}

type MethodAccessItem struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Access      struct {
		HasAccess bool `json:"has_access"`
	} `json:"access"`
}

type ResumeLimitsResponse struct {
	Limits map[string]int `json:"limits"`
	Spend  map[string]int `json:"spend"`
	Left   map[string]int `json:"left"`
}

type StatusResult struct {
	CurrentUser    CurrentUser            `json:"current_user"`
	PayableActions PayableActionsResponse `json:"payable_actions"`
	MethodAccess   MethodAccessResponse   `json:"method_access"`
	ResumeLimits   ResumeLimitsResponse   `json:"resume_limits"`
}

func New(cfg model.HHConfig, logger *slog.Logger) *Service {
	cfg = cfg.WithDefaults()
	authManager := auth.NewManager(cfg, logger)
	return &Service{
		client: hhclient.New(cfg, authManager, logger),
		logger: logger,
	}
}

func (s *Service) GetCurrentUser(ctx context.Context) (*CurrentUser, error) {
	var raw map[string]any
	status, err := s.client.GetJSON(ctx, "/me", nil, &raw)
	if err != nil {
		return nil, fmt.Errorf("fetch current hh user failed: status=%d err=%w", status, err)
	}

	user := &CurrentUser{
		AuthType:   stringValue(raw["auth_type"]),
		IsEmployer: boolValue(raw["is_employer"]),
		Email:      stringValue(raw["email"]),
		Raw:        raw,
	}
	if employerMap, ok := raw["employer"].(map[string]any); ok {
		user.Employer = &EmployerRef{
			ID:   stringValue(employerMap["id"]),
			Name: stringValue(employerMap["name"]),
		}
	}
	if managerMap, ok := raw["manager"].(map[string]any); ok {
		user.Manager = &ManagerRef{
			ID:                  stringValue(managerMap["id"]),
			HasAdminRights:      boolValue(managerMap["has_admin_rights"]),
			HasMultipleAccounts: boolValue(managerMap["has_multiple_manager_accounts"]),
			IsMainContactPerson: boolValue(managerMap["is_main_contact_person"]),
			ManagerSettingsURL:  stringValue(managerMap["manager_settings_url"]),
		}
	}
	return user, nil
}

func (s *Service) GetPayableActions(ctx context.Context, employerID string) (*PayableActionsResponse, error) {
	var out PayableActionsResponse
	status, err := s.client.GetJSON(ctx, "/employers/"+strings.TrimSpace(employerID)+"/services/payable_api_actions/active", nil, &out)
	if err != nil {
		return nil, fmt.Errorf("fetch hh payable actions failed: status=%d err=%w", status, err)
	}
	return &out, nil
}

func (s *Service) GetMethodAccess(ctx context.Context, employerID string, managerID string) (*MethodAccessResponse, error) {
	var out MethodAccessResponse
	status, err := s.client.GetJSON(ctx, "/employers/"+strings.TrimSpace(employerID)+"/managers/"+strings.TrimSpace(managerID)+"/method_access", nil, &out)
	if err != nil {
		return nil, fmt.Errorf("fetch hh method access failed: status=%d err=%w", status, err)
	}
	return &out, nil
}

func (s *Service) GetResumeLimits(ctx context.Context, employerID string, managerID string) (*ResumeLimitsResponse, error) {
	var out ResumeLimitsResponse
	status, err := s.client.GetJSON(ctx, "/employers/"+strings.TrimSpace(employerID)+"/managers/"+strings.TrimSpace(managerID)+"/limits/resume", nil, &out)
	if err != nil {
		return nil, fmt.Errorf("fetch hh resume limits failed: status=%d err=%w", status, err)
	}
	return &out, nil
}

func (s *Service) GetStatus(ctx context.Context) (*StatusResult, error) {
	user, err := s.GetCurrentUser(ctx)
	if err != nil {
		return nil, err
	}
	if user.Employer == nil || strings.TrimSpace(user.Employer.ID) == "" {
		return nil, fmt.Errorf("current hh user has no employer context")
	}
	if user.Manager == nil || strings.TrimSpace(user.Manager.ID) == "" {
		return nil, fmt.Errorf("current hh user has no manager context")
	}

	payable, err := s.GetPayableActions(ctx, user.Employer.ID)
	if err != nil {
		return nil, err
	}
	methodAccess, err := s.GetMethodAccess(ctx, user.Employer.ID, user.Manager.ID)
	if err != nil {
		return nil, err
	}
	limits, err := s.GetResumeLimits(ctx, user.Employer.ID, user.Manager.ID)
	if err != nil {
		return nil, err
	}

	return &StatusResult{
		CurrentUser:    *user,
		PayableActions: *payable,
		MethodAccess:   *methodAccess,
		ResumeLimits:   *limits,
	}, nil
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func boolValue(v any) bool {
	value, _ := v.(bool)
	return value
}

func WithManagerAccountQuery(basePath, managerAccountID string) string {
	q := url.Values{}
	if strings.TrimSpace(managerAccountID) != "" {
		q.Set("manager_account_id", strings.TrimSpace(managerAccountID))
	}
	if encoded := q.Encode(); encoded != "" {
		return basePath + "?" + encoded
	}
	return basePath
}
