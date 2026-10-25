package contract

// DisplayLabel 返回不含模型数据的固定产品动作；完成语义不由文字推断。
func (o Operation) DisplayLabel() string {
	labels := map[string]string{
		"knowledge.libraries.get": "读取知识库设置", "knowledge.sources.list": "查看知识来源", "knowledge.sources.read": "核查原文证据", "knowledge.pages.list": "查看知识页面", "knowledge.themes.list": "查看知识目录", "knowledge.index.get": "读取知识索引", "knowledge.revisions.list": "查看知识历史", "knowledge.revisions.get": "读取历史版本", "knowledge.jobs.list": "查看整理任务", "knowledge.jobs.get": "查看整理结果", "knowledge.audit.get": "核查知识质量", "knowledge.activity.list": "查看知识活动", "knowledge.export.get": "导出知识正文",
		"knowledge.pages.write": "修订知识页面", "knowledge.pages.replace": "精确修订知识", "knowledge.pages.rename": "重命名知识页面", "knowledge.pages.delete": "归档知识页面", "knowledge.revisions.restore": "恢复知识版本", "knowledge.themes.write": "调整知识目录", "knowledge.sources.import": "导入知识来源", "knowledge.jobs.retry": "重试知识整理",
		"knowledge.jobs.start":     "接纳授权知识整理",
		"knowledge.libraries.list": "查看可用知识库", "knowledge.pages.search": "搜索知识", "knowledge.pages.get": "读取知识页面", "knowledge.sources.get": "查看原件版本", "workflow.authoring.prepare": "准备工作流创作",
		"im.targets.list": "查看可发送聊天", "im.deliveries.create": "提交消息交付", "im.deliveries.get": "查看交付状态", "im.deliveries.wait": "等待消息交付",
		"workflow.source.get": "读取工作流", "workflow.source.validate": "检查工作流", "workflow.source.save": "保存工作流", "workflow.sdk.get": "了解工作流能力", "workflow.resources.list": "查看可用伙伴与资源", "workflow.annotations.list": "查看修改要求", "workflow.evidence.list": "查看执行记录",
		"workflow.definitions.list": "查看可用工作流", "workflow.definitions.describe": "查看工作流参数", "workflow.runs.start": "提交工作流运行", "workflow.runs.get": "查看执行进度", "workflow.runs.cancel": "请求停止工作流", "workflow.runs.wait": "等待工作流运行", "workflow.results.schema": "查看步骤结果格式", "workflow.results.submit": "提交步骤候选结果",
		"team.members.list": "查看团队成员", "team.tasks.list": "查看团队任务", "team.tasks.get": "查看任务详情", "team.tasks.create": "分配团队任务", "team.tasks.update": "更新团队任务", "team.tasks.follow-up": "补充团队任务", "team.tasks.retry": "请求重试任务", "team.tasks.cancel": "请求停止任务", "team.reports.submit": "提交工作报告",
		"memory.entries.search": "搜索记忆", "memory.entries.get": "查看记忆", "memory.entries.create": "保存记忆", "memory.entries.update": "更新记忆", "memory.entries.forget": "遗忘记忆",
		"interaction.questions.create": "请求用户补充", "interaction.questions.get": "查看用户回复", "interaction.questions.wait": "等待用户回复",
	}
	return labels[o.ID]
}
