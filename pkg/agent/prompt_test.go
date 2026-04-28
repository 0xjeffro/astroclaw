package agent

import (
	"strings"
	"testing"
)

// Verifies that when SOUL is empty, the default identity is used.
func TestBuildSystemPrompt_DefaultIdentity(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{}, DefaultCharLimits())
	if !strings.Contains(prompt, defaultIdentity) {
		t.Error("prompt should contain default identity when SOUL is empty")
	}
}

// Verifies that when SOUL is provided, it replaces the default identity.
func TestBuildSystemPrompt_SoulOverridesIdentity(t *testing.T) {
	cfg := PromptConfig{Soul: "I am a pirate assistant. Arrr!"}
	prompt := BuildSystemPrompt(cfg, DefaultCharLimits())
	if !strings.Contains(prompt, "pirate assistant") {
		t.Error("prompt should contain SOUL content")
	}
	if strings.Contains(prompt, defaultIdentity) {
		t.Error("prompt should NOT contain default identity when SOUL is set")
	}
}

// Verifies that USER profile is injected under a "User Profile" heading.
func TestBuildSystemPrompt_UserProfile(t *testing.T) {
	cfg := PromptConfig{User: "Name: Jeffro. iClaw developer."}
	prompt := BuildSystemPrompt(cfg, DefaultCharLimits())
	if !strings.Contains(prompt, "## User Profile") {
		t.Error("prompt should contain User Profile heading")
	}
	if !strings.Contains(prompt, "Jeffro") {
		t.Error("prompt should contain user profile content")
	}
}

// Verifies that when USER is empty, no User Profile section appears.
func TestBuildSystemPrompt_NoUserProfile(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{}, DefaultCharLimits())
	if strings.Contains(prompt, "## User Profile") {
		t.Error("prompt should NOT contain User Profile when USER is empty")
	}
}

// Verifies that memories are wrapped in context fencing tags.
func TestBuildSystemPrompt_MemoryContextFencing(t *testing.T) {
	cfg := PromptConfig{Memories: "- User's project uses DSQL\n- User prefers Chinese"}
	prompt := BuildSystemPrompt(cfg, DefaultCharLimits())
	if !strings.Contains(prompt, "<memory-context>") {
		t.Error("prompt should contain opening memory-context tag")
	}
	if !strings.Contains(prompt, "</memory-context>") {
		t.Error("prompt should contain closing memory-context tag")
	}
	if !strings.Contains(prompt, "NOT new user input") {
		t.Error("prompt should contain injection prevention note")
	}
	if !strings.Contains(prompt, "User's project uses DSQL") {
		t.Error("prompt should contain memory content")
	}
}

// Verifies that when memories are empty, no memory section appears.
func TestBuildSystemPrompt_NoMemories(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{}, DefaultCharLimits())
	if strings.Contains(prompt, "<memory-context>") {
		t.Error("prompt should NOT contain memory-context when memories are empty")
	}
}

// Verifies that content exceeding character limits is truncated.
func TestBuildSystemPrompt_Truncation(t *testing.T) {
	longSoul := strings.Repeat("a", 5000)
	cfg := PromptConfig{Soul: longSoul}
	limits := CharLimits{Soul: 100, User: 100, Memories: 100}
	prompt := BuildSystemPrompt(cfg, limits)
	if !strings.Contains(prompt, "[truncated]") {
		t.Error("prompt should contain [truncated] marker for oversized content")
	}
	if strings.Contains(prompt, strings.Repeat("a", 5000)) {
		t.Error("prompt should NOT contain full oversized content")
	}
}

// Verifies that all hardcoded sections are present in the prompt.
func TestBuildSystemPrompt_AllSectionsPresent(t *testing.T) {
	prompt := BuildSystemPrompt(PromptConfig{}, DefaultCharLimits())

	sections := []string{
		"## Safety",
		"## Execution",
		"## Tool Usage",
		"## Memory",
		"## Runtime",
		"Current time:",
	}
	for _, s := range sections {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt should contain section %q", s)
		}
	}
}
