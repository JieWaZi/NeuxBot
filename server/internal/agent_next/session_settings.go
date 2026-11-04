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
		if err := t.setSessionMode(client, request.Mode); err != nil {
			return err
		}
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
		revision := t.sessionOptionsRevision()
		options, err := client.SetSessionOption(t.ctx, t.binding.Session.SessionID, setting.ID, setting.Value)
		if err != nil {
			return err
		}
		t.applyOptionReceipt(revision, options)
	}
	return nil
}

func (t *Turn) setSessionMode(client AgentClient, mode string) error {
	l, state := t.lifecycle, t.thread
	l.mu.Lock()
	revision := state.modeRevision
	l.mu.Unlock()
	sessionID := t.binding.Session.SessionID
	if err := client.SetSessionMode(t.ctx, sessionID, mode); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if state.connection == t.conn && state.effective.ID == sessionID {
		// A successful setter confirms the mode. A newer native notification takes precedence.
		if state.modeRevision == revision {
			state.effective.Mode = mode
			state.modeRevision++
		}
		t.effective = state.effective
	}
	return nil
}

func (t *Turn) sessionOptionsRevision() uint64 {
	l, state := t.lifecycle, t.thread
	l.mu.Lock()
	defer l.mu.Unlock()
	return state.optionsRevision
}

func (t *Turn) applyOptionReceipt(revision uint64, options []AgentSessionOption) {
	l, state := t.lifecycle, t.thread
	l.mu.Lock()
	defer l.mu.Unlock()
	t.effective.Options = options
	if state.connection != t.conn || state.effective.ID != t.binding.Session.SessionID {
		return
	}
	if state.optionsRevision == revision {
		state.effective.Options = options
		state.optionsRevision++
	}
	t.effective = state.effective
}

func (l *ThreadLifecycle) applySessionNotification(state *threadState, generation uint64, scope AgentScope, event AgentEvent) {
	if event.Kind != "settings" && event.Kind != "mode" || scope.SessionID == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if state.generation != generation || state.effective.ID != scope.SessionID {
		return
	}
	switch event.Kind {
	case "settings":
		state.effective.Options = cloneOptions(event.Options)
		state.optionsRevision++
	case "mode":
		if event.Mode != "" {
			state.effective.Mode = event.Mode
			state.modeRevision++
		}
	}
}

func (t *Turn) observedSession() AgentSession {
	l, state := t.lifecycle, t.thread
	l.mu.Lock()
	defer l.mu.Unlock()
	if state.connection == t.conn && state.effective.ID == t.binding.Session.SessionID {
		return cloneSession(state.effective)
	}
	return cloneSession(t.effective)
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
