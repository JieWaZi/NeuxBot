# 智能体与账号

NeuxBot 调用本机智能体。伙伴是长期工作身份，智能体是执行程序，AI 账号是认证与模型服务配置。

| 智能体      | 默认命令              |
| ----------- | --------------------- |
| Codex       | codex                 |
| Claude Code | claude                |
| Cursor      | cursor-agent 或 agent |
| Kimi        | kimi                  |
| Pi          | pi                    |
| OpenCode    | opencode              |
| Gemini CLI  | gemini                |
| Grok Build  | grok                  |
| WorkBuddy   | codebuddy 或 cbc      |
| Qoder       | qoder 或 qodercli     |
| Qoder CN    | qodercn、qoder-cn 或 qoderclicn |

在设置中探测安装并选择实际路径。有多版本时明确选择版本；未完成探测或模型不可用时使用就近重试。系统配置沿用 CLI 登录，受管账号在 AI 账号设置中维护。

Qoder 使用 [官方 ACP 接入](https://docs.qoder.com/cli/acp)，中国版使用 [Qoder CN ACP](https://docs.qoder.cn/cli/acp)，两者在设置中分别选择并使用各自 CLI 登录。桌面版与独立 CLI 分别安装，中国版安装入口见 [官方安装说明](https://docs.qoder.cn/cli/installation)。模型、图片和续聊等能力以当前安装版本的探测结果为准；权限按实际模式目录展示，旧版仅提供默认与完全访问。

API 账号选择服务商、认证、请求协议、服务地址与模型。刷新模型更新候选项，需要显式选择模型。聊天配置随账号重新校验；模型与思考菜单以真实能力为准。

权限、MCP 传输、附加目录、分叉和即时输入由实际版本与能力决定。完全访问仍需用户回答业务问题。协议或模型不匹配时修正配置，不依赖应用自动转换服务协议。
