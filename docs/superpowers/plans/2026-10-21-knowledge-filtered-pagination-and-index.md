# 知识库筛选分页与固定索引标题实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 文档搜索、状态筛选、排序、计数与分页统一由后端处理；页面标题固定为“索引”，正文标题固定为“Wiki Index”，概述和目录继续更新。

**Architecture:** 独立核心统一计算来源展示状态并查询，宿主不再对已分页数据筛选。新增查询保持旧 ListSources 调用合同；自有远端透传查询，官方 WeKnora 使用其原查询协议。索引内容在核心生成及宿主展示边界固定标题，保留真实导言和目录。

**Tech Stack:** Go、SQLite/GORM、生成 OpenAPI Client、React、TanStack Query。

**Spec:** 用户本轮明确要求与前一轮已确认诊断。

## 全局约束

- 不提交或推送；保留其他任务未提交改动，不控制运行中的应用。
- 默认每页 10 条，支持 10/30/50；改变筛选与每页条数回到第一页。
- 生成快照和 API 类型只通过对应生成命令更新。
- 排队与整理中按真实作业及当前尝试判定；终态优先，不将旧失败或其他尝试冒充当前状态。

## 审查重点

- 匹配项跨越原始第一页时不得遗漏，总数必须与筛选相同。
- 无匹配、无任务、同时间排序、归档和当前尝试重试。
- 本地、自有远端、官方远端参数和身份保持正确，不回退本机。
- 老索引的动态标题被规范化，已有概述及目录不丢失。
- 筛选变化重置 offset，查询缓存完整包含筛选和排序。

### 任务一：核心来源查询

- [x] 在临时核心副本加入失败测试，覆盖跨页匹配、全局排序、精确计数、活动状态、归档及字面量搜索。
- [x] 实现 `core.SourceQuery` 与 `Engine.ListSourcesQuery(ctx, libraryID, query)`，字段 `Offset/Limit/Query/State/Sort`；状态接受 all/pending/queued/processing/processed/unchanged/failed/cancelled。
- [x] 提供 `Engine.SourceActivity(ctx, libraryID, sources)` 复用状态投影；旧 ListSources 保持兼容。
- [x] 自有 HTTP 和远端客户端支持 query/state/sort；官方客户端映射原查询合同。
- [x] 运行核心测试、vet 和 race，以当前运行快照为基线生成宿主快照，不覆盖较旧独立目录。

### 任务二：固定索引标题

- [x] 加入失败测试，覆盖模型生成动态标题、增量更新与旧导言展示。
- [x] 核心生成和持久索引读取保持固定 Wiki Index 标题，不冻结概述。
- [x] 前端页面名称固定为“索引”，正文固定“Wiki Index”；Wiki 普通页面标题不受影响。

### 任务三：宿主接入与验收

- [x] 宿主本地/远端 Operator 接收 query/state/sort，按过滤结果返回 items/total。
- [x] 前端传递全部参数，纳入 Query Key，删除页内筛选和排序，改变筛选回第一页。
- [x] 回归测试重现 44 条数据的空页问题并验证修复，覆盖状态、搜索、排序和无结果。
- [x] 更新权威文档，运行 API 生成和完整检查；报告并行修改导致的失败及真实视觉验收边界。
