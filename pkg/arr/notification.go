package arr

import (
	"fmt"
	"net/http"
)

const webhookNotificationName = "Decypharr"

type notificationField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type notificationPayload struct {
	ID             int                 `json:"id,omitempty"`
	Name           string              `json:"name"`
	Implementation string              `json:"implementation"`
	ConfigContract string              `json:"configContract"`
	Fields         []notificationField `json:"fields"`

	OnDownload    bool `json:"onDownload"`
	OnUpgrade     bool `json:"onUpgrade"`
	OnRename      bool `json:"onRename"`
	IncludeHealthWarnings bool `json:"includeHealthWarnings"`

	// Sonarr-specific
	OnSeriesDelete               bool `json:"onSeriesDelete,omitempty"`
	OnEpisodeFileDelete          bool `json:"onEpisodeFileDelete,omitempty"`
	OnEpisodeFileDeleteForUpgrade bool `json:"onEpisodeFileDeleteForUpgrade,omitempty"`

	// Radarr-specific
	OnMovieDelete              bool `json:"onMovieDelete,omitempty"`
	OnMovieFileDelete          bool `json:"onMovieFileDelete,omitempty"`
	OnMovieFileDeleteForUpgrade bool `json:"onMovieFileDeleteForUpgrade,omitempty"`
}

// RegisterWebhook creates or updates a Decypharr webhook notification in the ARR instance.
func (a *Arr) RegisterWebhook(webhookURL string) error {
	// List existing notifications to check if ours already exists.
	var existing []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	resp, err := a.Request(http.MethodGet, "api/v3/notification", nil, &existing)
	if err != nil {
		return fmt.Errorf("failed to list notifications: %w", err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to list notifications: %s", resp.Status)
	}

	existingID := 0
	for _, n := range existing {
		if n.Name == webhookNotificationName {
			existingID = n.ID
			break
		}
	}

	payload := notificationPayload{
		Name:           webhookNotificationName,
		Implementation: "Webhook",
		ConfigContract: "WebhookSettings",
		Fields: []notificationField{
			{Name: "url", Value: webhookURL},
			{Name: "method", Value: "1"}, // POST
		},
		OnDownload:    true,
		OnUpgrade:     true,
		OnRename:      true,
		IncludeHealthWarnings: false,

		OnSeriesDelete:                true,
		OnEpisodeFileDelete:           true,
		OnEpisodeFileDeleteForUpgrade: true,

		OnMovieDelete:               true,
		OnMovieFileDelete:           true,
		OnMovieFileDeleteForUpgrade: true,
	}

	if existingID != 0 {
		payload.ID = existingID
		resp, err = a.Request(http.MethodPut, fmt.Sprintf("api/v3/notification/%d", existingID), payload, nil)
	} else {
		resp, err = a.Request(http.MethodPost, "api/v3/notification", payload, nil)
	}
	if err != nil {
		return fmt.Errorf("failed to register webhook: %w", err)
	}
	if resp.Body != nil {
		resp.Body.Close()
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("failed to register webhook: %s", resp.Status)
	}
	return nil
}
