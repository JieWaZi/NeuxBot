package agent

import (
	"slices"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// SessionSetting requests one exact native option without guessing a provider's choices.
type SessionSetting struct {
	// ID is the provider-issued setting identifier.
	ID string
	// Value preserves select or tri-state boolean intent.
	Value AgentOptionValue
}

func (t *Turn) applySettings(client AgentClient, request ThreadRequest) error {
	if request.Mode != "" && request.Mode != t.effective.Mode {
		if !slices.ContainsFunc(t.effective.Modes, func(c AgentChoice) bool { return c.ID == request.Mode }) {
			return status_error.RuntimePolicyUnsupported
		}
		if err := client.SetSessionMode(t.ctx, t.binding.Session.SessionID, request.Mode); err != nil {
			return err
		}
		t.effective.Mode = request.Mode
	}
	settings := request.Settings
	if model := request.Connection.Account.Model; model != "" {
		i := slices.IndexFunc(t.effective.Options, func(o AgentSessionOption) bool { return o.Category == "model" || o.ID == "model" })
		if i < 0 {
			return status_error.RuntimePolicyUnsupported
		}
		modelID := t.effective.Options[i].ID
		if slices.ContainsFunc(settings, func(s SessionSetting) bool {
			return s.ID == modelID && (s.Value.ID != model || s.Value.Bool != nil)
		}) {
			return status_error.RuntimeConfigurationInvalid
		}
		settings = append([]SessionSetting{{ID: modelID, Value: AgentOptionValue{ID: model}}}, settings...)
	}
	for _, setting := range settings {
		i := slices.IndexFunc(t.effective.Options, func(o AgentSessionOption) bool { return o.ID == setting.ID })
		if i < 0 {
			return status_error.RuntimePolicyUnsupported
		}
		option := t.effective.Options[i]
		if setting.Value.Bool != nil {
			if setting.Value.ID != "" || option.Value.Bool == nil {
				return status_error.RuntimeConfigurationInvalid
			}
			if *setting.Value.Bool == *option.Value.Bool {
				continue
			}
		} else {
			if setting.Value.ID != "" && setting.Value.ID == option.Value.ID {
				continue
			}
			if !slices.ContainsFunc(option.Choices, func(c AgentChoice) bool { return c.ID == setting.Value.ID }) {
				return status_error.RuntimeConfigurationInvalid
			}
		}
		options, err := client.SetSessionOption(t.ctx, t.binding.Session.SessionID, setting.ID, setting.Value)
		if err != nil {
			return err
		}
		t.effective.Options = options
	}
	return nil
}

func cloneSession(session AgentSession) AgentSession {
	session.Options = cloneOptions(session.Options)
	session.Modes = cloneChoices(session.Modes)
	return session
}

func cloneOptions(options []AgentSessionOption) []AgentSessionOption {
	options = slices.Clone(options)
	for i := range options {
		if options[i].Value.Bool != nil {
			value := *options[i].Value.Bool
			options[i].Value.Bool = &value
		}
		options[i].Choices = cloneChoices(options[i].Choices)
	}
	return options
}

func cloneChoices(choices []AgentChoice) []AgentChoice {
	choices = slices.Clone(choices)
	for i := range choices {
		if choices[i].Config != nil {
			cfg := *choices[i].Config
			cfg.Options = cloneOptions(cfg.Options)
			cfg.UnknownOptionIDs = slices.Clone(cfg.UnknownOptionIDs)
			choices[i].Config = &cfg
		}
	}
	return choices
}
