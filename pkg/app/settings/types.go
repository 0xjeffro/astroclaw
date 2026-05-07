package settings

import (
	"errors"
	"time"

	"astroclaw/pkg/app/settings/db"
)

var (
	ErrNotFound = errors.New("setting not found")
)

// Setting name constants.
const (
	SettingUserProfile    = "user_profile"
	SettingDefaultAgentID = "default_agent_id"
)

type KVSetting struct {
	ID        string
	Name      string
	Value     string
	UpdatedAt time.Time
}

func KVSettingFromDB(s db.AppSettingsKv) *KVSetting {
	return &KVSetting{
		ID:        s.ID,
		Name:      s.Name,
		Value:     s.Value,
		UpdatedAt: s.UpdatedAt,
	}
}
