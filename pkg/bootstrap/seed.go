package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"

	"astroclaw/pkg/app/agents"
	"astroclaw/pkg/app/passwords"
	"astroclaw/pkg/app/settings"
	"astroclaw/pkg/app/system"
)

// seedSystemDataKey ensures the system-scope data key exists. Must
// run before any seedXxx call that touches system credentials.
func seedSystemDataKey(ctx context.Context, pwSvc *passwords.Service) error {
	if err := pwSvc.ProvisionSystemDataKey(ctx); err != nil {
		return fmt.Errorf("provision system data key: %w", err)
	}
	return nil
}

// seedCredentials writes system-scope credentials passed in as strings.
// Empty inputs are skipped so callers can seed a subset per environment.
func seedCredentials(ctx context.Context, pwSvc *passwords.Service, anthropicAPIKey string) error {
	if anthropicAPIKey != "" {
		if err := pwSvc.UpsertSystemCredential(ctx, passwords.SystemCredAnthropicAPIKey, "Anthropic API key", anthropicAPIKey); err != nil {
			return fmt.Errorf("seed %s: %w", passwords.SystemCredAnthropicAPIKey, err)
		}
		log.Printf("seeded %s", passwords.SystemCredAnthropicAPIKey)
	}
	return nil
}

// seedJWTSecret ensures the HMAC secret used to sign session JWTs
// exists. Generated on first run with crypto/rand, wrapped with the
// system data key, reused thereafter. Never appears in env or logs.
func seedJWTSecret(ctx context.Context, pwSvc *passwords.Service) error {
	if _, err := pwSvc.GetSystemCredential(ctx, passwords.SystemCredJWTSecret); err == nil {
		log.Printf("%s already exists, skipping seed", passwords.SystemCredJWTSecret)
		return nil
	} else if !errors.Is(err, passwords.ErrNotFound) {
		return fmt.Errorf("check %s: %w", passwords.SystemCredJWTSecret, err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("generate %s: %w", passwords.SystemCredJWTSecret, err)
	}
	secret := base64.RawStdEncoding.EncodeToString(raw)

	if err := pwSvc.UpsertSystemCredential(ctx, passwords.SystemCredJWTSecret,
		"HMAC secret used to sign session JWTs (auto-generated, do not log)", secret); err != nil {
		return fmt.Errorf("store %s: %w", passwords.SystemCredJWTSecret, err)
	}
	log.Printf("seeded %s", passwords.SystemCredJWTSecret)
	return nil
}

// seedDefaultUser creates the admin user on first run and (re)sets the
// password when a non-empty value is supplied. Empty password leaves
// any existing password untouched.
func seedDefaultUser(ctx context.Context, svc *system.Service, adminPassword string) error {
	admin, err := svc.GetAdmin(ctx)
	if err != nil {
		// TODO: admin email is a placeholder; expose it via Config later.
		admin, err = svc.CreateUser(ctx, "admin@astroclaw.local", "Admin", system.RoleAdmin)
		if err != nil {
			return fmt.Errorf("seed admin user: %w", err)
		}
		log.Printf("seeded admin user: %s (%s)", admin.Name, admin.ID)
	} else {
		log.Println("admin user already exists, skipping user seed")
	}

	if adminPassword != "" {
		if err := svc.SetUserPassword(ctx, admin.ID, adminPassword); err != nil {
			return fmt.Errorf("set admin password: %w", err)
		}
		log.Printf("set admin password (length %d) for %s", len(adminPassword), admin.ID)
	}
	return nil
}

// seedDefaultWorkspace creates a "Default" workspace and adds the
// admin as owner. Skipped when the admin already belongs to any
// workspace.
func seedDefaultWorkspace(ctx context.Context, svc *system.Service) error {
	admin, err := svc.GetAdmin(ctx)
	if err != nil {
		return fmt.Errorf("get admin for workspace seed: %w", err)
	}
	existing, err := svc.ListWorkspacesForUser(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("list workspaces for admin: %w", err)
	}
	if len(existing) > 0 {
		log.Println("admin already belongs to a workspace, skipping seed")
		return nil
	}

	w, err := svc.CreateWorkspace(ctx, "Default")
	if err != nil {
		return fmt.Errorf("seed default workspace: %w", err)
	}
	log.Printf("seeded default workspace: %s (%s)", w.Name, w.ID)

	if err := svc.AddMembership(ctx, admin.ID, w.ID, system.WorkspaceRoleOwner); err != nil {
		return fmt.Errorf("add admin to default workspace: %w", err)
	}
	log.Printf("added admin to default workspace as owner")
	return nil
}

// seedDefaultAgent creates the "genesis" agent in the default
// workspace and pins it as the default_agent_id workspace setting.
// Skipped when the workspace already has agents.
func seedDefaultAgent(ctx context.Context, sysSvc *system.Service, svc *agents.Service, settingsSvc *settings.Service) error {
	admin, err := sysSvc.GetAdmin(ctx)
	if err != nil {
		return fmt.Errorf("get admin for agent seed: %w", err)
	}
	workspaces, err := sysSvc.ListWorkspacesForUser(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("list admin workspaces: %w", err)
	}
	if len(workspaces) == 0 {
		return fmt.Errorf("admin has no workspace to seed agent into")
	}
	workspaceID := workspaces[0].ID

	existing, err := svc.ListAgentsByWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}
	if len(existing) > 0 {
		log.Printf("agents already exist in workspace %s, skipping seed", workspaceID)
		return nil
	}

	defaultSoul := "You are AstroClaw, a personal AI assistant. " +
		"Be genuinely helpful, not performatively helpful. Skip filler words like 'Great question!' and just help. " +
		"Have opinions. Be direct. Admit uncertainty when appropriate. " +
		"Be resourceful before asking. Try to figure it out, then ask if stuck. " +
		"Be concise when needed, thorough when it matters."

	// TODO: model should come from Config.
	a, err := svc.CreateAgent(ctx, workspaceID, "genesis", defaultSoul, "claude-haiku-4-5-20251001")
	if err != nil {
		return fmt.Errorf("seed genesis agent: %w", err)
	}
	log.Printf("seeded genesis agent: %s (%s) in workspace %s", a.Name, a.ID, workspaceID)

	if err := settingsSvc.UpsertWorkspaceSetting(ctx, workspaceID, settings.SettingWorkspaceDefaultAgentID, a.ID); err != nil {
		return fmt.Errorf("seed default_agent_id setting: %w", err)
	}
	log.Printf("seeded default_agent_id: %s", a.ID)
	return nil
}
