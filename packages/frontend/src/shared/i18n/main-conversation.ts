export const mainConversationZh = {
  newTeamThread: "新建团队对话",
  mainConversation: "主会话",
  openSource: "打开工作会话",
  openDefaultConversation: "打开默认会话",
  defaultConversationTitle: "默认会话",
  workRecordTitle: "工作会话",
  sourceEmptyHistory: "工作会话暂无消息",
  sourceNoReply: "本轮没有回复",
  sourceInputOrigin: "来自主会话",
  viewMainConversationMessage: "在主会话查看原消息",
  sourceModelSelected: "{{actor}} 将下轮模型从 {{before}} 改为 {{after}}",
  sourceModelSelectedInitial: "{{actor}} 选择了下轮模型 {{after}}",
  deletedWorkOrigin: "来自已删除的会话",
  deletedWorkOriginWithTitle: "来自已删除的会话「{{title}}」",
  returnToMain: "返回主会话",
  retryPublicSend: "发送回执尚未确认。可用原请求重试。",
  openWorkRecord: "打开工作会话",
  openWorkflowRun: "查看运行",
  forkPublicMessage: "从这里分支",
  deletePublicMessage: "删除消息？",
  deletePublicMessageDescription: "这条消息将从主会话移除，工作记录会保留。",
  deletePublicMessageFailed: "无法删除消息，请重试。",
  submitPublicFailed: "消息未能发送。",
  workSummary: {
    tasks: "任务",
    loadingTasks: "正在加载任务",
    noTasks: "暂无正在执行的任务",
    untitled: "未命名会话",
  },
  workTicket: {
    loadFailed: "无法读取工单，请重试。",
    transferReplyPlaceholder: "输入回复，或 /移交 伙伴名",
    transferRequiresReply: "请先引用原交办或一条工单回报。",
    transferUsage:
      "输入 /移交 和接手伙伴或团队的全名。另起一行可填写交接说明。",
    transferTargetMissing: "没有找到同名的伙伴或团队。",
    transferTargetAmbiguous:
      "名称对应多个伙伴或团队，请使用唯一 ID，或指定 waker:名称、team:名称。",
    transferNeedsNote: "请另起一行填写交接内容。",
    transferTextOnly: "交接说明仅支持文字，请移除附件后再提交。",
    transferFailed: "移交未获确认，请重试。",
  },
};

export const mainConversationEn = {
  newTeamThread: "New team conversation",
  mainConversation: "Main conversation",
  openSource: "Open work conversation",
  openDefaultConversation: "Open default conversation",
  defaultConversationTitle: "Default conversation",
  workRecordTitle: "Work conversation",
  sourceEmptyHistory: "No work history yet",
  sourceNoReply: "No reply in this turn",
  sourceInputOrigin: "From the main conversation",
  viewMainConversationMessage:
    "View the original message in the main conversation",
  sourceModelSelected:
    "{{actor}} changed the next turn’s model from {{before}} to {{after}}",
  sourceModelSelectedInitial: "{{actor}} selected {{after}} for the next turn",
  deletedWorkOrigin: "From a deleted conversation",
  deletedWorkOriginWithTitle: "From the deleted conversation “{{title}}”",
  returnToMain: "Back to main conversation",
  retryPublicSend: "Delivery is unconfirmed. Retry the original request.",
  openWorkRecord: "Open work conversation",
  openWorkflowRun: "View run",
  forkPublicMessage: "Branch from here",
  deletePublicMessage: "Delete message?",
  deletePublicMessageDescription:
    "This removes the message from the main conversation and keeps the work history.",
  deletePublicMessageFailed: "Could not delete the message. Try again.",
  submitPublicFailed: "The message could not be sent.",
  workSummary: {
    tasks: "Tasks",
    loadingTasks: "Loading tasks",
    noTasks: "No running tasks",
    untitled: "Untitled conversation",
  },
  workTicket: {
    loadFailed: "Could not load the work ticket. Try again.",
    transferReplyPlaceholder: "Reply, or /transfer partner name",
    transferRequiresReply:
      "Reply to the original request or a work report first.",
    transferUsage:
      "Enter /transfer and the partner or team’s full name. Add a handoff note on a new line.",
    transferTargetMissing: "No partner or team has that exact name.",
    transferTargetAmbiguous:
      "Multiple partners or teams have that name. Use a unique ID or specify waker:name or team:name.",
    transferNeedsNote: "Add a handoff note on a new line.",
    transferTextOnly:
      "The handoff takes text. Remove attachments and try again.",
    transferFailed: "The transfer is unconfirmed. Try again.",
  },
};
