# 伙伴委派、团队与左栏：上游源码对照评审

本评审核对原分支 `codex/superagent-main-conversation` 的本次工作区。区分源码移植、等效适配及未对齐项，不把静态核对当成桌面或真实模型验收。

## 直接移植

- **Nuphos 左栏页面动效。** [轨道组件](https://github.com/nuphos/nuphos/blob/2c2f2e27fab0a9098de430734f05079680ff71c8/apps/desktop/src/components/sidebar/SidebarSwipePages.tsx#L21-L40) 保持页面壳常驻并将离屏页置为 `inert`；[CSS 布局](https://github.com/nuphos/nuphos/blob/2c2f2e27fab0a9098de430734f05079680ff71c8/apps/desktop/src/components/sidebar/sidebar-pages.css#L1-L17) 为横向 flex 轨道；[控制器](https://github.com/nuphos/nuphos/blob/2c2f2e27fab0a9098de430734f05079680ff71c8/apps/desktop/src/components/sidebar/sidebar-swipe-controller.ts#L40-L70) 使用位移、220 毫秒和指定缓动。NeuxBot [移植样式](../../packages/frontend/src/app/layout/partner-navigation-pages.css) 用两个分页，保留位移曲线，以共享标准时长和 CSS transition 接入，不引入三页拖动、原生触控板协议或额外动画库。离屏页不接受焦点，减少动态效果时直接切换。第三方许可随包保留。轨道壳常驻不代表会话数据面板一直挂载，离开会话页时仍按现有查询逻辑卸载列表。

## 等效适配

- **OpenBot 的分派与移交。** 固定版本 [OpenBot channel service](https://github.com/nightly-labs/openbot/blob/100c60e893eda3ce4761b4d10001a8e490c43536/src/backend/channel-service.ts#L3405-L3529) 验证成员和来源消息；assign 创建子任务，transfer 保留任务身份并改负责人。NeuxBot [工单服务](../../server/services/workticket/delegation.go) 在已有事务里用父单、代次、请求 ID 去重，任一层子单都服从团队成员边界，回件走直接父单。沿用原有的同工单移交流程，不把 Teams 的状态写进个人主会话。未新增复制 OpenBot 代码；该固定版 [许可](https://github.com/nightly-labs/openbot/blob/100c60e893eda3ce4761b4d10001a8e490c43536/LICENSE) 限制商用。右侧设置参考其信息层级，并非完整复刻。
- **OpenSuperAgent 的 Leader、派单和回件。** Rust 固定版本 [群入口与接力](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/server/crates/osa/src/groups.rs#L129-L214) 默认交给 Leader，明示点名可走其他成员；[派工](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/server/crates/osa/src/router.rs#L860-L899) 先记录任务事件，再起 Worker；[结算](https://github.com/kadaliao/open-super-agent/blob/e9722c7462cbf220ece4e03f74257cfc7d19962a/server/crates/osa/src/router.rs#L1018-L1049) 在原聊天追加关联任务身份的结果。NeuxBot 复用现有 Go Router / WorkTicket / Chat，保存原主会话和交办消息，Team 共享主会话走 Leader；没有复制 Rust 队列或按名称匹配的群点名规则，不把技术身份硬编码进文案。
- **Nuphos 的公开反馈和私有执行隔离。** [`channel_post` 工具](https://github.com/nuphos/nuphos/blob/2c2f2e27fab0a9098de430734f05079680ff71c8/apps/backend/src/lib/claude-code-preview/preview-tools/channels.ts#L231-L245) 要求先简短确认，再随时发布已取得的结果，原始执行聊天不对频道公开。NeuxBot 用现有受授权的工单公开回件实现这条边界。A 显示委派，B 左栏显示受托工作，工作对话只按授权开放，不复制到 B 的私人主会话。团队与伙伴复用 [主会话委派卡](../../packages/frontend/src/modules/chat/components/main-conversation-delegations.tsx)；[任务树测试](../../packages/frontend/src/app/layout/sidebar-work-tree.test.tsx) 断言完成前后仍使用原列表行。

## 评审发现及验收缺口

1. **长历史可能挤掉仍在执行的任务。** [活动查询](../../server/repositories/controllers/work_ticket_delegation.go) 按最近创建取每层最多 100 条，没有优先保活跃任务或历史游标。长时间运行的旧任务可能被新记录挤出左栏，且现有搜索不会补回这些工单。建议活跃任务独立入选、历史用游标，再验证大量历史与深层子任务。
2. **团队兼容链路仍在。** Team 主会话已共用 Router/WorkTicket，但原团队线程、IM 和定时任务没有迁移。不能把它们视作已退役。移除前要验证旧历史链接、当前在途任务的归属、幂等与外部绑定。
3. **上游交互没有全部复刻。** 当前左栏没有 Nuphos 的触控板滑动、拖动排序或任务行重排补间；右侧复杂设置仍以管理入口为主。全局会话搜索目前匹配标题，不搜索正文。
4. **仍需真实入口验收。** 测试覆盖切换、无交互离屏页、任务状态更新及 Team 成员约束；没有据此确认窗口实际帧表现、A/B 两个主会话的真实模型交互、Team 并行执行或新闻逐条发布的实际时序。
