// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package usersettings

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/userconfig"
)

// The closed value sets docker-agent's TUI accepts (pkg/tui/messages/settings.go). The
// TUI silently falls back to the default for anything else, so the provider rejects
// unknown values instead. coverage_test.go checks these lists against the docker-agent
// source.
var (
	SafetyModes            = []string{"strict", "balanced", "restricted", "autonomous"}
	BusySendModes          = []string{"steer", "queue"}
	InterruptConfirmations = []string{"always", "double-tap", "none"}
	SidebarPositions       = []string{"right", "left", "top", "bottom"}
	SectionSpacings        = []string{"normal", "compact", "relaxed"}
	SidebarInfoModes       = []string{"compact", "detailed"}
)

// KeybindingActions are the TUI actions that can be remapped (pkg/tui/core/keys.go,
// `core.ValidActions()`). docker-agent ignores unknown actions with a log warning.
var KeybindingActions = []string{
	"clear_queue", "commands", "cycle_agent", "edit_external", "editor_newline", "editor_send", "help",
	"history_search", "model_picker", "quit", "suspend", "switch_focus", "toggle_hide_tool_results",
	"toggle_sidebar", "toggle_yolo",
}

// keyModifiers and namedKeys mirror docker-agent's key validator (pkg/tui/core/keys.go).
var (
	keyModifiers = []string{"alt", "ctrl", "hyper", "meta", "shift", "super"}
	namedKeys    = []string{
		"backspace", "begin", "capslock", "comma", "delete", "div", "down", "end", "enter", "equal", "esc",
		"find", "home", "insert", "left", "menu", "minus", "mul", "mute", "numlock", "pause", "period",
		"pgdown", "pgup", "plus", "printscreen", "right", "scrolllock", "select", "sep", "space", "tab", "up",
	}
)

// Validate applies docker-agent's checks to a decoded user configuration. Errors are
// settings docker-agent rejects when loading the file, or ignores (usually with only a
// log line): invalid safety modes, alias names and paths, global providers, hooks,
// sandbox hosts and TUI enum values. Warnings are settings docker-agent accepts but
// partially ignores, such as an unknown keybinding action.
func Validate(cfg *userconfig.Config) (errs, warnings []string) {
	errorf := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	warnf := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	if s := cfg.Settings; s != nil {
		// userconfig.Load rejects the whole file for an invalid safety mode.
		if err := s.Safety.Validate(); err != nil {
			errorf("settings.safety: %v", err)
		}
		oneOf(errorf, "settings.busy_send_mode", s.BusySendMode, BusySendModes)
		oneOf(errorf, "settings.interrupt_confirmation", s.InterruptConfirmation, InterruptConfirmations)
		if s.TabTitleMaxLength < 0 {
			errorf("settings.tab_title_max_length must not be negative, got %d", s.TabTitleMaxLength)
		}
		if s.SoundThreshold < 0 {
			errorf("settings.sound_threshold must not be negative, got %d", s.SoundThreshold)
		}
		if l := s.Layout; l != nil {
			oneOf(errorf, "settings.layout.sidebar_position", l.SidebarPosition, SidebarPositions)
			oneOf(errorf, "settings.layout.section_spacing", l.SectionSpacing, SectionSpacings)
			oneOf(errorf, "settings.layout.sidebar_info_mode", l.SidebarInfoMode, SidebarInfoModes)
		}
		// docker-agent drops the whole hooks section with a warning when it is invalid
		// (Settings.GlobalHooks).
		if s.Hooks != nil {
			if err := s.Hooks.Validate(); err != nil {
				errorf("settings.%v", err)
			}
		}
		validateKeybindings(s.Keybindings, errorf, warnf)
	}

	for _, name := range slices.Sorted(maps.Keys(cfg.Aliases)) {
		alias := cfg.Aliases[name]
		if alias == nil {
			errorf("aliases.%s: path is required", name)
			continue
		}
		// SetAlias is what `docker agent alias add` runs: name syntax, non-empty path and
		// safety mode.
		scratch := &userconfig.Config{Aliases: map[string]*userconfig.Alias{}}
		if err := scratch.SetAlias(name, alias); err != nil {
			errorf("aliases.%s: %v", name, err)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(cfg.Providers)) {
		p := cfg.Providers[name]
		if strings.TrimSpace(name) == "" {
			errorf("providers: provider name cannot be empty")
			continue
		}
		// docker-agent skips invalid global providers with a warning when merging them into
		// an agent configuration (config.MergeGlobalProviders).
		if err := config.ValidateProviderConfig(name, p); err != nil {
			errorf("providers.%s: %v", name, err)
		}
		if err := p.Auth.Validate(p.Provider); err != nil {
			errorf("providers.%s: %v", name, err)
		}
	}

	if m := cfg.DefaultModel; m != nil {
		if m.Provider == "" || m.Model == "" {
			errorf("default_model: provider and model are required; docker-agent ignores a default model " +
				"without them")
		}
		probe := &latest.Config{
			Providers: cfg.Providers,
			Models:    map[string]latest.ModelConfig{"default_model": m.ModelConfig},
		}
		if err := probe.Validate(); err != nil {
			errorf("%s", strings.Replace(err.Error(), "models.default_model", "default_model", 1))
		}
	}

	if len(cfg.SandboxAllowlist) > 0 {
		seen := map[string]bool{}
		for i, h := range cfg.SandboxAllowlist {
			switch {
			case strings.TrimSpace(h) == "":
				errorf("sandbox_allowlist[%d]: host must not be empty", i)
			case h != strings.TrimSpace(h):
				errorf("sandbox_allowlist[%d]: %q has leading or trailing whitespace", i, h)
			case seen[h]:
				warnf("sandbox_allowlist[%d]: %q is listed more than once", i, h)
			}
			seen[h] = true
		}
		// The same check `docker agent sandbox allow` applies.
		if _, err := (&userconfig.Config{}).AddSandboxHosts(cfg.SandboxAllowlist...); err != nil {
			errorf("sandbox_allowlist: %v", err)
		}
	}

	if b := cfg.Board; b != nil {
		for i, p := range b.Projects {
			if strings.TrimSpace(p.Name) == "" {
				errorf("board.projects[%d]: name is required", i)
			}
			if strings.TrimSpace(p.Path) == "" {
				errorf("board.projects[%d]: path is required", i)
			}
		}
		ids := map[string]int{}
		for i, c := range b.Columns {
			id := strings.TrimSpace(c.ID)
			if id == "" && strings.TrimSpace(c.Name) == "" {
				errorf("board.columns[%d]: id or name is required", i)
				continue
			}
			if id == "" {
				continue
			}
			if j, dup := ids[id]; dup {
				errorf("board.columns[%d]: id %q is already used by board.columns[%d]; docker-agent "+
					"drops the duplicate", i, id, j)
			}
			ids[id] = i
		}
	}

	if h := cfg.CredentialHelper; h != nil && strings.TrimSpace(h.Command) == "" {
		errorf("credential_helper.command is required")
	}
	return errs, warnings
}

func oneOf(errorf func(string, ...any), field, value string, valid []string) {
	if value != "" && !slices.Contains(valid, value) {
		errorf("%s: invalid value %q (valid: %s)", field, value, strings.Join(valid, ", "))
	}
}

func validateKeybindings(bindings []userconfig.Keybinding, errorf, warnf func(string, ...any)) {
	for i, b := range bindings {
		field := fmt.Sprintf("settings.keybindings[%d]", i)
		if strings.TrimSpace(b.Action) == "" {
			errorf("%s: action is required", field)
			continue
		}
		if !slices.Contains(KeybindingActions, b.Action) {
			warnf("%s: docker-agent does not know the action %q and ignores it (valid: %s)",
				field, b.Action, strings.Join(KeybindingActions, ", "))
		}
		if len(b.Keys) == 0 {
			errorf("%s: at least one key is required", field)
		}
		for j, k := range b.Keys {
			if !validKey(strings.TrimSpace(k)) {
				warnf("%s.keys[%d]: docker-agent ignores the malformed key %q (use the Bubble Tea format, "+
					"for example `ctrl+k`, `alt+enter` or `f2`)", field, j, k)
			}
		}
	}
}

// validKey mirrors docker-agent's validateKey.
func validKey(k string) bool {
	if k == "" || strings.ContainsAny(k, " \t") {
		return false
	}
	parts := strings.Split(k, "+")
	for _, m := range parts[:len(parts)-1] {
		if !slices.Contains(keyModifiers, m) {
			return false
		}
	}
	base := parts[len(parts)-1]
	switch {
	case base == "":
		return false
	case len([]rune(base)) == 1:
		return true
	case isFunctionKey(base):
		return true
	}
	return slices.Contains(namedKeys, base)
}

// isFunctionKey reports whether s is f1..f63 (mirrors docker-agent).
func isFunctionKey(s string) bool {
	if len(s) < 2 || s[0] != 'f' {
		return false
	}
	n := 0
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
		if n > 63 {
			return false
		}
	}
	return n >= 1
}
