package settings

import (
	"errors"
	"time"

	"iclaw/pkg/app/settings/db"
)

var (
	ErrNotFound = errors.New("setting not found")
)

type PromptSetting struct {
	ID        string
	Name      string
	Value     string
	UpdatedAt time.Time
}

func PromptSettingFromDB(s db.AppSettingsPrompt) *PromptSetting {
	return &PromptSetting{
		ID:        s.ID,
		Name:      s.Name,
		Value:     s.Value,
		UpdatedAt: s.UpdatedAt,
	}
}
