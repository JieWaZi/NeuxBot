# WorkBuddy 专家和专家团的实现与 Codex 迁移

调研日期为 2026 年 11 月 26 日。本机对照 WorkBuddy 桌面版 `5.11.3` 的安装包与四个专家资源目录。结论来自公开文档和静态读取，实际召唤、模型调用、企业接口及 Codex 安装待验证。

## WorkBuddy 怎样运行专家

专家以 CodeBuddy 插件目录发布。`.codebuddy-plugin/plugin.json` 声明 `expertType`、入口 `agentName`、角色定义路径、版本和展示信息。`agents/*.md` 的 YAML frontmatter 保存角色名、描述及可选技能配置，Markdown 正文作为角色指令，描述工作流程、边界和交付。包可附带技能、脚本、参考资料、MCP 和连接器依赖。单专家召唤后由该角色执行主会话。 [专家包规范](https://open.workbuddy.cn/docs/expert) · [专家中心](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Expert-Center)

专家团通过 `expertType: "team"` 与 `teamInfo.leadAgent`、`teamInfo.memberAgents` 声明团长和成员。开放平台提交规范还要求用根目录 `settings.json` 指向团长。本机两个已下载的专家团样本没有该文件，入口仍可从清单确定。团长的角色指令定义任务路由、并行阶段、先后依赖和交付规则。WorkBuddy 提供建队、启动成员和消息通信等工具，团长据此分派并汇总。 [专家团包规范](https://open.workbuddy.cn/docs/expert-team) · [Agent Teams 工具和协作模型](https://www.codebuddy.cn/docs/cli/agent-teams)

安装包 `app.asar` 中的 `main/module.app-server.js` 先定位本地插件，再按 `expertType` 解析 `teamInfo.leadAgent` 或普通专家的 `agentName`，定位对应 Markdown。`main/conversations.js` 可从会话详情还原专家选择，并在组装请求时保留 `codebuddy.ai/expertSelection` 标记。`main/expert-summon-guard.js` 有受功能开关控制的召唤访问检查。源码片段说明了可见的入口和数据传递边界。

| 本机样本 | 静态证据 |
| --- | --- |
| `fullstack-dev` `1.0.0` | 包含一份角色定义，样本没有附带技能目录或 `.mcp.json`。 |
| `senior-developer` `1.1.1` | 包含一份角色定义，随包提供 `browser-use`、`frontend-dev`、`fullstack-dev` 三项技能。 |
| `software-company` `1.1.0` | 团长 `software-team-lead` 带四个成员，分别负责产品、架构、开发、QA。团长指令区分快速、缺陷修复和标准流程。快速流程由工程师执行，QA 随后验证。样本没有附带技能目录或 `.mcp.json`。 |
| `cloud-ops-team` `1.0.1` | 三位成员各自绑定技能，团长按咨询、工单、迁移等意图派工。迁移时还需核对技能的脚本、服务接口和授权。 |

本机相关目录是 `~/.workbuddy/plugins/marketplaces/experts/plugins/` 和 `~/.workbuddy/plugins/cache/experts/<包名>/<版本>/`。这四个样本确认了该机器上可见的资源范围。其他资源可从自有源包、作者授权包或开放平台官方样例取得。个人版公开的分享链接在 WorkBuddy 里召唤副本。企业目录接口可筛选 `AGENT` 和 `TEAM`，资源结构包含 `downloadUrl`，能否下载指定包取决于企业目录和权限。 [个人版专家管理](https://www.codebuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Expert-Center) · [企业专家目录](https://cloud.tencent.com/document/product/1831/140382) · [企业资源结构](https://cloud.tencent.com/document/product/1831/140399#ExpertItem)

## 在 Codex 中怎么用

| 要保留的能力 | Codex 实现 |
| --- | --- |
| 在主对话中按专家方法执行 | 将获授权的角色正文整理为 `.agents/skills/<专家名>/SKILL.md`，通过技能入口显式调用。 |
| 将某个专家作为独立执行者 | 在项目 `.codex/agents/` 中保存一个 TOML，映射 `name`、`description` 和 `developer_instructions`。个人跨项目使用可放在 `~/.codex/agents/`。 |
| 召唤专家团 | 把团长的流程改成技能，由 Codex 主会话执行。团员各自转为自定义子代理。将 `TeamCreate` 和派工描述改成 Codex 的子代理委派，将回报改成等待并收集子代理结果。按中间产物决定何时进入下一阶段。 |
| 复用脚本、参考资料和连接器 | 保留兼容的技能目录与相对路径。检查原工具名和脚本运行条件。MCP 在父会话配置并按服务要求认证，子代理继承父会话可用的 MCP 与权限。 |

Codex 的迁移入口是技能和角色配置。WorkBuddy 的市场卡片、产品权限、专属连接器、`maxTurns` 及团队任务状态需要按 Codex 可用能力逐项处理。对于有依赖的成员，团长等待前序结果后再派工。让写代码的成员独占相关文件，或按目录明确分工。 [Codex 自定义子代理](https://learn.chatgpt.com/docs/agent-configuration/subagents) · [技能目录](https://learn.chatgpt.com/docs/build-skills) · [MCP 配置](https://learn.chatgpt.com/docs/extend/mcp)

一个团队迁移样例是主会话调用 `$software-team`，小改动先派工程师，完成后派 QA。涉及接口和设计的需求先由产品角色整理验收标准，再由架构角色给出接口与边界，之后派工程师和 QA。子代理文件只需要三个必填字段，例如 `name = "software-qa-engineer"`、`description = "验证软件改动和回归范围"` 和 `developer_instructions = "按照验收标准执行定向验证，返回命令、结果与未验证范围。"`。可用一次明确的只读任务检查子代理调用、等候、产物交接和汇总。 [Codex 子代理工作流](https://learn.chatgpt.com/docs/agent-configuration/subagents)

## 由 Codex 调度，由腾讯侧执行

企业版 Managed Agents 可以绑定目标企业有权使用的普通专家，然后由适配器通过腾讯公开的会话与 ACP 接口执行，再把结果作为 Codex 工具返回。企业资源文档明确 `TEAM` 暂不支持绑定到 Managed Agent。个人版 Open API 公开了本地助理消息和云端任务能力，所查接口中未找到按市场专家 ID 或专家团 ID 发起调用的正式参数。 [企业 Manifest](https://cloud.tencent.com/document/product/1831/139534) · [企业 Agent 调用](https://cloud.tencent.com/document/product/1831/140759) · [专家类型限制](https://cloud.tencent.com/document/product/1831/140399#ExpertItem) · [个人版 Open API](https://open.workbuddy.cn/docs/openapi)

取得适用的专家包时，可以另外评估本机 CodeBuddy Code 桥接。CodeBuddy Code 公开 `--plugin-dir` 加载本地包，并提供 ACP 入口。具体 WorkBuddy 包能否在该入口加载团长、启动成员以及连接依赖仍需实测，尤其是包含 WorkBuddy 专用工具和授权的包。 [CodeBuddy CLI](https://www.codebuddy.cn/docs/cli/cli-reference) · [CodeBuddy 插件加载](https://www.codebuddy.cn/docs/cli/plugins)
