package runtimes

func builtinCodexDefinition() runtimeDefinition {
	return runtimeDefinition{
		id:                            RuntimeIDCodex,
		baseRuntimeID:                 RuntimeIDCodex,
		name:                          "Codex CLI",
		binaryCandidates:              []string{"codex"},
		environment:                   map[string]string{},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            ".agents",
		configHomeEnvironmentVariable: "CODEX_HOME",
	}
}

func builtinClaudeDefinition() runtimeDefinition {
	return runtimeDefinition{
		id:               RuntimeIDClaude,
		baseRuntimeID:    RuntimeIDClaude,
		name:             "Claude Code",
		binaryCandidates: []string{"claude"},
		environment:      map[string]string{},
		models: []ModelOption{
			{ID: "sonnet", Name: "Sonnet"},
			{ID: "opus", Name: "Opus"},
			{ID: "haiku", Name: "Haiku"},
		},
		defaultModel:                  "sonnet",
		instructionFileName:           "CLAUDE.md",
		skillDirectoryName:            ".claude",
		configHomeEnvironmentVariable: "CLAUDE_CONFIG_DIR",
	}
}

// builtinNativeDefinition 为原生 ACP 和现成适配器建立相同的启动与目录定义。
func builtinNativeDefinition(id, name string, commands []string, skills, configHome string) runtimeDefinition {
	return runtimeDefinition{
		id:                            id,
		baseRuntimeID:                 id,
		name:                          name,
		binaryCandidates:              commands,
		environment:                   map[string]string{},
		instructionFileName:           "AGENTS.md",
		skillDirectoryName:            skills,
		configHomeEnvironmentVariable: configHome,
	}
}

// builtinGeminiDefinition 使用官方 GEMINI.md 和 .gemini/skills。
func builtinGeminiDefinition() runtimeDefinition {
	definition := builtinNativeDefinition(RuntimeIDGemini, "Gemini CLI", []string{"gemini"}, ".gemini", "GEMINI_CLI_HOME")
	definition.instructionFileName = "GEMINI.md"
	return definition
}

// builtinWorkBuddyDefinition 通过公开 CLI 入口交付其原生指令和技能，不依赖桌面私有进程。
func builtinWorkBuddyDefinition() runtimeDefinition {
	definition := builtinNativeDefinition(RuntimeIDWorkBuddy, "WorkBuddy", []string{"codebuddy", "cbc"}, ".codebuddy", "CODEBUDDY_CONFIG_DIR")
	definition.instructionFileName = "CODEBUDDY.md"
	return definition
}

// builtinQoderDefinition 隔离国际版与中国版的原生命令和用户配置根，项目技能均使用 .qoder。
func builtinQoderDefinition(china bool) runtimeDefinition {
	definition := builtinNativeDefinition(RuntimeIDQoder, "Qoder", []string{"qoder", "qodercli"}, ".qoder", "QODER_CONFIG_DIR")
	definition.userBinaryCandidates = []string{".qoder/bin/qodercli/qodercli"}
	if china {
		definition = builtinNativeDefinition(RuntimeIDQoderCN, "Qoder CN", []string{"qodercn", "qoder-cn", "qoderclicn"}, ".qoder", "QODERCN_CONFIG_DIR")
		definition.userBinaryCandidates = []string{".qoder-cn/bin/qoderclicn/qoderclicn"}
	}
	return definition
}
