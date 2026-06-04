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
	SettingUserProfile             = "user_profile"
	SettingWorkspaceDefaultAgentID = "default_agent_id"
)

// SystemSetting is a deploy-wide config entry.
type SystemSetting struct {
	Name      string
	Value     string
	UpdatedAt time.Time
}

func SystemSettingFromDB(s db.AppSettingsSystem) *SystemSetting {
	return &SystemSetting{
		Name:      s.Name,
		Value:     s.Value,
		UpdatedAt: s.UpdatedAt,
	}
}

// WorkspaceSetting is a setting scoped to a single workspace.
type WorkspaceSetting struct {
	WorkspaceID string
	Name        string
	Value       string
	UpdatedAt   time.Time
}

func WorkspaceSettingFromDB(s db.AppSettingsWorkspace) *WorkspaceSetting {
	return &WorkspaceSetting{
		WorkspaceID: s.WorkspaceID,
		Name:        s.Name,
		Value:       s.Value,
		UpdatedAt:   s.UpdatedAt,
	}
}

// UserSetting is a setting scoped to a single user.
type UserSetting struct {
	UserID    string
	Name      string
	Value     string
	UpdatedAt time.Time
}

func UserSettingFromDB(s db.AppSettingsUser) *UserSetting {
	return &UserSetting{
		UserID:    s.UserID,
		Name:      s.Name,
		Value:     s.Value,
		UpdatedAt: s.UpdatedAt,
	}
}
