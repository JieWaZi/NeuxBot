# 服务商能力对齐实施状态

状态：实施中。此记录不是完成或发布声明。

## 范围与分支

NeuxBot 与 acp-go 均使用独立工作区的 `codex/provider-runtime-parity` 分支。NeuxBot 原基线为 `e18a0d6e`，acp-go 为 `a58342da`；原开发工作区的无关修改未带入。

[统一规格](../../superpowers/specs/2026-11-03-provider-runtime-parity-design.md)覆盖服务商发现/安装/配置、多账号、模型/强度/Fast、错误、Token/费用与账号额度。已通过独立设计审查，子任务使用 GPT‑6.1 high。

## 阶段

| 阶段                                 | 状态                                | 实施依据                                                                                           |
| ------------------------------------ | ----------------------------------- | -------------------------------------------------------------------------------------------------- |
| 源码审计和官方协议核验               | 已完成研究，非运行验收              | 本目录专项审计与来源报告                                                                           |
| 原生Token、缺失语义、失败归档        | acp-go修复已审查，NeuxBot贯通复审中 | [用量基础计划](../../superpowers/plans/2026-11-03-provider-usage-foundation.md)                    |
| 模型、Fast、账号目录和结构错误       | 计划已审查，未实施                  | [模型能力计划](../../superpowers/plans/2026-11-03-provider-model-capabilities.md)                  |
| 账号额度与重置时间                   | 计划已审查，未实施                  | [额度计划](../../superpowers/plans/2026-11-03-provider-account-quota.md)                           |
| 受管安装、更新与重启                 | 计划已审查，未实施                  | [安装计划](../../superpowers/plans/2026-11-03-managed-provider-runtimes.md)                        |
| 耐久统计和费用估算                   | 计划待审查，未实施           | [统计计划](../../superpowers/plans/2026-11-03-provider-usage-analytics.md)                                                                    |
| 服务商详情、模型选择、统计与配额界面 | 计划已审查，未实施                  | [界面计划](../../superpowers/plans/2026-11-03-provider-ui.md)与[界面审计](accounts-ui-audit-cn.md) |
| 集成及真实验收                       | 未完成                              | 后续逐provider/平台记录                                                                            |

## 已有验证

- 基线 acp-go：`go test ./pkg/codex ./pkg/claude ./pkg/acpmeta` 通过。
- 基线 NeuxBot：`go test ./internal/agent/... ./services/runtimeenvironment ./services/aiaccount ./apis/runtime_options ./apis/ai_accounts` 通过。
- 审计临时回归复现 Codex 多请求漏计/缓存分类、Claude 未知归零及取消结果丢已有用量；具体限定条件见[用量审计](usage-audit-cn.md)。这些 FAIL 是缺陷证据，不能记为修复通过。
- acp-go `82785d48`、`485d6a7f`：新增可空用量快照及来源/覆盖范围，修复Codex轮累计、Claude后续输入结算、Cursor字段缺失性、Kimi截断日志保留消费。acpmeta/Codex/Claude/Pi/Kimi/Cursor涉及包通过；修复后Claude/Cursor/Kimi包及新增用例race通过。独立审查四项Important均已解决；float64极大整数精度边界留整体验收处理。
- 新代码在每个实施任务完成、独立审查后更新本记录。真实账号、模型、页面与各操作系统尚未验收。

两个仓库通过工作区外的临时 Go workspace 联调，不提交机器绝对路径 replace。依赖发布、固定远端版本、合并和产品发布均尚未执行。
