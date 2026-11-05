package agent

import (
	"context"
	"slices"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// SessionSetting requests one exact native option without guessing a provider's choices.
type SessionSetting struct {
	// ID is the provider-issued setting identifier.
	ID string
	// Value preserves select or tri-state boolean intent.
	Value AgentOptionValue
}

// TurnSelection carries requested per-turn overrides and an unresolved host default.
type TurnSelection struct {
	// Reasoning is the native effort ID, or empty for provider defaults.
	Reasoning string
	// PreferNoThinking 在没有明确强度时优先选择关闭推理，其次选择最低可用档位。
	PreferNoThinking bool
	// Fast is explicit on/off, or nil to inherit.
	Fast *bool
	// DefaultFast applies only if this model advertises native Fast support.
	DefaultFast *bool
}

type codexTurnConfigurer interface {
	ConfigureCodexTurn(context.Context, string, string, string, *bool) ([]AgentSessionOption, error)
}

func (t *Turn) applyCodexSettings(client AgentClient, request ThreadRequest) error {
	codex, ok := client.(codexTurnConfigurer)
	if !ok {
		return status_error.RuntimePolicyUnsupported
	}
	settings := request.Selection
	model := request.Connection.Account.Model
	fast := settings.Fast
	if fast == nil && settings.DefaultFast != nil {
		for _, option := range t.effective.Options {
			if option.ID != "model" {
				continue
			}
			target := model
			if target == "" {
				target = option.Value.ID
			}
			if slices.ContainsFunc(option.Choices, func(choice AgentChoice) bool {
				return choice.ID == target && choice.FastServiceTier != ""
			}) {
				fast = settings.DefaultFast
			}
		}
	}
	revision := t.sessionOptionsRevision()
	options, err := codex.ConfigureCodexTurn(t.ctx, t.binding.Session.SessionID, model, settings.Reasoning, fast)
	if err != nil {
		return err
	}
	t.applyOptionReceipt(revision, options)
	t.requestedFast = fast
	return nil
}

// RequestedFast reports the frozen intent from the native setter, independently of native observation.
func (t *Turn) RequestedFast() *bool {
	if t.requestedFast == nil {
		return nil
	}
	value := *t.requestedFast
	return &value
}

func (t *Turn) applySettings(client AgentClient, request ThreadRequest) error {
	if request.Selection != nil && request.Connection.Account.Provider == "codex" {
		if err := t.applyCodexSettings(client, request); err != nil {
			return err
		}
	} else {
		if model := request.Connection.Account.Model; model != "" {
			i := slices.IndexFunc(t.effective.Options, func(o AgentSessionOption) bool { return o.Category == "model" || o.ID == "model" })
			if i < 0 {
				return status_error.RuntimePolicyUnsupported
			}
			id := t.effective.Options[i].ID
			if slices.ContainsFunc(request.Settings, func(s SessionSetting) bool { return s.ID == id && (s.Value.ID != model || s.Value.Bool != nil) }) {
				return status_error.RuntimeConfigurationInvalid
			}
			if err := t.applySetting(client, SessionSetting{ID: id, Value: AgentOptionValue{ID: model}}); err != nil {
				return err
			}
		}
		if request.Selection != nil {
			if err := t.applyProviderSelection(client, request.Connection.Account.Provider, *request.Selection); err != nil {
				return err
			}
		}
		for _, setting := range request.Settings {
			if err := t.applySetting(client, setting); err != nil {
				return err
			}
		}
	}
	// Claude model changes can downgrade auto mode. The adapter validates the
	// requested mode against the newly selected model before any prompt is admitted.
	mode := request.Mode
	if provider := request.Connection.Account.Provider; (provider == "qoder" || provider == "qoder-cn") && mode == "yolo" {
		if !slices.ContainsFunc(t.effective.Modes, func(choice AgentChoice) bool { return choice.ID == mode }) &&
			slices.ContainsFunc(t.effective.Modes, func(choice AgentChoice) bool { return choice.ID == "bypassPermissions" }) {
			mode = "bypassPermissions"
		}
	}
	if mode != "" && mode != t.effective.Mode {
		return t.setSessionMode(client, mode)
	}
	return nil
}

// applyProviderSelection consumes the dependent selectors confirmed by the model setter.
func (t *Turn) applyProviderSelection(client AgentClient, provider string, selection TurnSelection) error {
	effort := slices.IndexFunc(t.effective.Options, func(o AgentSessionOption) bool { return o.Category == "thought_level" })
	if selection.Reasoning == "" && selection.PreferNoThinking && effort >= 0 {
		selection.Reasoning = minimumReasoning(t.effective.Options[effort].Choices)
	}
	if selection.Reasoning != "" {
		if effort < 0 {
			return status_error.RuntimePolicyUnsupported
		}
		option := t.effective.Options[effort]
		if err := t.applySetting(client, SessionSetting{ID: option.ID, Value: AgentOptionValue{ID: selection.Reasoning}}); err != nil {
			return err
		}
	} else if effort >= 0 {
		if err := t.applyNativeDefault(client, provider, t.effective.Options[effort]); err != nil {
			return err
		}
	}

	fast := selection.Fast
	i := slices.IndexFunc(t.effective.Options, func(o AgentSessionOption) bool { return o.ID == "fast" })
	if fast == nil && i >= 0 {
		fast = selection.DefaultFast
	}
	if i >= 0 {
		option := t.effective.Options[i]
		if fast == nil {
			if err := t.applyNativeDefault(client, provider, option); err != nil {
				return err
			}
		} else {
			if err := t.applySetting(client, SessionSetting{ID: option.ID, Value: fastSettingValue(option, fast)}); err != nil {
				return err
			}
		}
	} else if fast != nil {
		return status_error.RuntimePolicyUnsupported
	}
	t.requestedFast = fast
	return nil
}

// minimumReasoning 按原生可选值选择最低推理档位，未知档位沿用原生顺序。
func minimumReasoning(choices []AgentChoice) string {
	for _, wanted := range []string{"none", "off", "disabled", "minimal", "min", "low", "medium", "high", "max", "xhigh"} {
		for _, choice := range choices {
			if strings.EqualFold(choice.ID, wanted) {
				return choice.ID
			}
		}
	}
	if len(choices) > 0 {
		return choices[0].ID
	}
	return ""
}

func fastSettingValue(option AgentSessionOption, fast *bool) AgentOptionValue {
	if option.Value.Bool != nil {
		return AgentOptionValue{Bool: fast}
	}
	if slices.ContainsFunc(option.Choices, func(c AgentChoice) bool { return c.ID == "on" }) {
		if *fast {
			return AgentOptionValue{ID: "on"}
		}
		return AgentOptionValue{ID: "off"}
	}
	if *fast {
		return AgentOptionValue{ID: "true"}
	}
	return AgentOptionValue{ID: "false"}
}

func (t *Turn) applyNativeDefault(client AgentClient, provider string, option AgentSessionOption) error {
	switch provider {
	case "claude":
		// Claude 的有效值可能来自用户配置。只清除原生确认过的显式覆盖。
		if option.acknowledgedID != "" && option.acknowledgedID != "default" {
			return t.applySetting(client, SessionSetting{ID: option.ID, Value: AgentOptionValue{ID: "default"}})
		}
	case "cursor":
		// Cursor 会跨进程保留模型参数。当前会话值不能作为默认值的证据。
		model := slices.IndexFunc(t.effective.Options, func(o AgentSessionOption) bool { return o.Category == "model" || o.ID == "model" })
		if model < 0 {
			return status_error.RuntimePolicyUnsupported
		}
		id := t.effective.Options[model].Value.ID
		catalog, err := client.ReadModels(t.ctx)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(catalog.Models, func(choice AgentChoice) bool { return choice.ID == id })
		if i < 0 || catalog.Models[i].Config == nil {
			return status_error.RuntimePolicyUnsupported
		}
		options := catalog.Models[i].Config.Options
		j := slices.IndexFunc(options, func(candidate AgentSessionOption) bool { return candidate.ID == option.ID })
		if j < 0 {
			return status_error.RuntimePolicyUnsupported
		}
		return t.applySetting(client, SessionSetting{ID: option.ID, Value: options[j].Value})
	}
	return nil
}

func (t *Turn) applySetting(client AgentClient, setting SessionSetting) error {
	i := slices.IndexFunc(t.effective.Options, func(o AgentSessionOption) bool { return o.ID == setting.ID })
	if i < 0 {
		return status_error.RuntimePolicyUnsupported
	}
	option := t.effective.Options[i]
	resetClaude := t.binding.Session.Provider == "claude" && setting.Value.ID == "default" &&
		option.acknowledgedID != "" && option.acknowledgedID != "default"
	if setting.Value.Bool != nil {
		if setting.Value.ID != "" || option.Value.Bool == nil {
			return status_error.RuntimeConfigurationInvalid
		}
		if *setting.Value.Bool == *option.Value.Bool {
			return nil
		}
	} else {
		if setting.Value.ID != "" && setting.Value.ID == option.Value.ID && !resetClaude {
			return nil
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
	observed := slices.IndexFunc(t.effective.Options, func(o AgentSessionOption) bool { return o.ID == setting.ID })
	if observed < 0 {
		return status_error.RuntimeConfigurationInvalid
	}
	receipt := t.effective.Options[observed]
	// 清除覆盖后的观测值可能来自用户配置，不要求其恰好等于 default。
	if resetClaude && receipt.acknowledgedID == "default" {
		return nil
	}
	value := receipt.Value
	if setting.Value.Bool != nil {
		if value.Bool == nil || *value.Bool != *setting.Value.Bool {
			return status_error.RuntimeConfigurationInvalid
		}
	} else if value.ID != setting.Value.ID || value.Bool != nil {
		return status_error.RuntimeConfigurationInvalid
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
	if t.effective.Mode != mode {
		return status_error.RuntimeConfigurationInvalid
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
	applySessionOptions(&t.effective, options)
	if state.connection != t.conn || state.effective.ID != t.binding.Session.SessionID {
		return
	}
	if state.optionsRevision == revision {
		if applySessionOptions(&state.effective, options) {
			state.modeRevision++
		}
		state.optionsRevision++
	}
	t.effective = state.effective
	// Dependent setters consume this request's authoritative readback. Notifications
	// queued by an earlier setter may arrive during this call; they remain global
	// observations but cannot replace the receipt used to configure this turn.
	applySessionOptions(&t.effective, options)
}

// applySessionOptions keeps the native mode selector and its session view coherent.
// A model change may downgrade permissions even when no separate mode event exists.
func applySessionOptions(session *AgentSession, options []AgentSessionOption) bool {
	session.Options = options
	for _, option := range options {
		if option.Category == "mode" && option.Value.ID != "" {
			// 旧设置的同值快照不应覆盖正在确认的新权限。
			changed := session.Mode != option.Value.ID
			session.Mode = option.Value.ID
			return changed
		}
	}
	return false
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
		if applySessionOptions(&state.effective, cloneOptions(event.Options)) {
			state.modeRevision++
		}
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
	session := t.effective
	if state.connection == t.conn && state.effective.ID == t.binding.Session.SessionID {
		session = state.effective
	}
	l.mu.Unlock()
	session = cloneSession(session)
	if t.conn != nil {
		if client, err := t.conn.Client(); err == nil {
			if direct, ok := client.(*directAgentClient); ok {
				session.ObservedCodexSettings = direct.observeCodexSettings(t.ctx, session.ID)
			}
		}
	}
	return session
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
		choices[i].InputModalities = slices.Clone(choices[i].InputModalities)
		choices[i].PermissionModes = slices.Clone(choices[i].PermissionModes)
		if tier := choices[i].DefaultServiceTier; tier != nil {
			copied := *tier
			choices[i].DefaultServiceTier = &copied
		}
		if choices[i].Config != nil {
			cfg := *choices[i].Config
			cfg.Options = cloneOptions(cfg.Options)
			cfg.UnknownOptionIDs = slices.Clone(cfg.UnknownOptionIDs)
			choices[i].Config = &cfg
		}
	}
	return choices
}
