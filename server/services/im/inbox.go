package im

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/channel"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func (s *Service) route(ctx context.Context, input models.IMInbox) error {
	command, args := parseCommand(input.Content)
	selectedLinkID := ""
	var inspectedRevision int64
	if command == "" {
		binding, err := s.config.Store.Binding(ctx, input.BindingID)
		if err != nil {
			return err
		}
		inspectedRevision = binding.Revision
		if binding.State == constants.IMBindingStateApproved {
			selectedLinkID, err = s.incomingConversation(ctx, input, binding)
			if err != nil {
				return err
			}
		}
	}
	return s.WithBinding(ctx, input.BindingID, func(ctx context.Context, binding models.IMBinding) error {
		if binding.State == constants.IMBindingStateRejected {
			input.State = constants.IMInboxStateSkipped
			input.ErrorCode = "IMChatNotAuthorized"
			return s.config.Store.UpdateInbox(ctx, input)
		}
		if binding.State != constants.IMBindingStateApproved {
			return nil
		}
		// 权威 Chat 查询在事务外完成；配置变动时让耐久输入重新路由，避免覆盖新选择。
		if command == "" && binding.Revision != inspectedRevision {
			return status_error.IMConfigurationConflict
		}
		if command != "" && !allowedCommand(binding, input.SenderID, command) {
			return s.finishInput(ctx, input, binding, "此命令需要该聊天的管理员操作。")
		}
		switch command {
		case "/help":
			return s.finishInput(ctx, input, binding, "发送消息即可开始工作。\n/new 新建对话\n/list 查看当前聊天的对话\n/use <对话编号> 切换对话\n/status 查看当前状态\n/stop 停止当前工作\n/approve <审批编号> <选项编号> 处理工具审批\n/answer <问答编号> <JSON答案> 回答简单表单\n群内新建、切换、停止和审批由聊天管理员操作。")
		case "/new":
			if err := s.selectConversation(ctx, binding, ""); err != nil {
				return err
			}
			return s.finishInput(ctx, input, binding, "已准备新对话。下一条工作消息会开始独立对话。")
		case "/list":
			links, err := s.config.Store.Links(ctx, binding.ID)
			if err != nil {
				return err
			}
			text := "当前聊天的对话："
			for index, link := range links {
				if index >= 20 {
					break
				}
				if link.ChatID == "" {
					continue
				}
				text += "\n" + link.ID[:8] + " · " + link.Title
			}
			return s.finishInput(ctx, input, binding, text)
		case "/use":
			selected, err := s.findLink(ctx, binding.ID, strings.TrimSpace(args))
			if err != nil {
				return s.finishInput(ctx, input, binding, "未找到该聊天内唯一匹配的对话编号，请用 /list 查看。")
			}
			if err := s.selectConversation(ctx, binding, selected.ID); err != nil {
				return err
			}
			return s.finishInput(ctx, input, binding, "已切换到："+selected.Title)
		}
		linkID := binding.ActiveLinkID
		if command == "" {
			linkID = selectedLinkID
		}
		if linkID == "" && command != "" {
			return s.finishInput(ctx, input, binding, "当前还没有实际对话。发送工作消息即可创建。")
		}
		if linkID == "" {
			linkID = protocol.StableID("link", input.ID)
			frozen := binding
			frozen.ContextToken = ""
			frozen.ContextCiphertext = ""
			link := models.IMConversationLink{ID: linkID, BindingID: binding.ID, Title: shortTitle(input.Content), Configuration: frozen}
			if err := s.config.Store.SaveLink(ctx, link); err != nil {
				return err
			}
			binding.ActiveLinkID = linkID
			if err := s.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
				return err
			}
		}
		input.LinkID = linkID
		input.ExecutionID = protocol.StableID("execution", input.ID)
		input.State = constants.IMInboxStateRouted
		return s.config.Store.UpdateInbox(ctx, input)
	})
}

// incomingConversation 只延续仍存在的实际会话，失效引用由本次新输入开始独立对话。
func (s *Service) incomingConversation(ctx context.Context, input models.IMInbox, binding models.IMBinding) (string, error) {
	linkID := binding.ActiveLinkID
	if input.QuoteMessageID != "" {
		quoted, err := s.config.Store.QuoteLink(ctx, binding.ID, input.QuoteMessageID)
		if err != nil {
			return "", err
		}
		if quoted != "" {
			linkID = quoted
		}
	}
	if linkID == "" {
		return "", nil
	}
	link, err := s.config.Store.Link(ctx, linkID)
	if hasCode(err, status_error.IMResourceNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if link.BindingID != binding.ID {
		return "", status_error.IMResourceNotFound
	}
	exists, err := s.conversationExists(ctx, link)
	if err != nil || !exists {
		return "", err
	}
	return linkID, nil
}

// conversationExists 区分尚未首发、明确删除与真实查询失败，不把后者当新建依据。
func (s *Service) conversationExists(ctx context.Context, link models.IMConversationLink) (bool, error) {
	if link.ThreadID != "" {
		if _, err := s.config.Threads.Get(ctx, link.ThreadID); err != nil {
			if hasCode(err, status_error.ChannelThreadNotFound) {
				return false, nil
			}
			return false, err
		}
	}
	if link.ChatID == "" {
		return true, nil
	}
	// 真实日志才拥有会话存在性；不能把可重建的 SQLite 查询投影缺失当作删除。
	if _, err := s.config.Chats.CountUserMessages(ctx, link.ChatID); err != nil {
		if hasCode(err, status_error.ChatNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func parseCommand(text string) (string, string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", ""
	}
	parts := strings.SplitN(text, " ", 2)
	command := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return command, args
}
func allowedCommand(binding models.IMBinding, sender, command string) bool {
	if binding.Kind == constants.IMChatKindDirect || command == "/help" || command == "/list" || command == "/status" {
		return true
	}
	for _, controller := range binding.ControllerIDs {
		if controller != "" && controller == sender {
			return true
		}
	}
	return false
}
func shortTitle(content string) string {
	value := []rune(strings.TrimSpace(content))
	if len(value) > 40 {
		value = value[:40]
	}
	return string(value)
}
func (s *Service) findLink(ctx context.Context, bindingID, prefix string) (models.IMConversationLink, error) {
	links, err := s.config.Store.Links(ctx, bindingID)
	if err != nil {
		return models.IMConversationLink{}, err
	}
	var found *models.IMConversationLink
	if len(prefix) < 8 {
		return models.IMConversationLink{}, status_error.IMResourceNotFound
	}
	for index := range links {
		if links[index].ChatID != "" && strings.HasPrefix(links[index].ID, prefix) {
			if found != nil {
				return models.IMConversationLink{}, status_error.IMConfigurationConflict
			}
			found = &links[index]
		}
	}
	if found == nil {
		return models.IMConversationLink{}, status_error.IMResourceNotFound
	}
	return *found, nil
}

func (s *Service) finishInput(ctx context.Context, input models.IMInbox, binding models.IMBinding, content string) error {
	if _, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "input:" + input.ID, TargetID: binding.ID, Content: content, LinkID: input.LinkID}); err != nil {
		return err
	}
	input.State = constants.IMInboxStateProcessed
	return s.config.Store.UpdateInbox(ctx, input)
}

func (s *Service) dispatch(input models.IMInbox) {
	key := input.LinkID
	if command, _ := parseCommand(input.Content); command != "" {
		key = input.ID
	}
	s.life.Lock()
	if s.ctx.Err() != nil || s.dispatching[key] {
		s.life.Unlock()
		return
	}
	s.dispatching[key] = true
	s.dispatchConnections[input.ConnectionID]++
	s.workers.Add(1)
	s.life.Unlock()
	go func() {
		defer s.workers.Done()
		defer func() {
			s.life.Lock()
			delete(s.dispatching, key)
			s.dispatchConnections[input.ConnectionID]--
			if s.dispatchConnections[input.ConnectionID] == 0 {
				delete(s.dispatchConnections, input.ConnectionID)
			}
			s.life.Unlock()
		}()
		if err := s.accept(s.ctx, input); err != nil {
			if hasCode(err, status_error.ChatHasActiveExecution) || hasCode(err, status_error.CreateRequestInProgress) {
				return
			}
			input.State = constants.IMInboxStateFailed
			input.ErrorCode = errorCode(err)
			if updateErr := s.config.Store.UpdateInbox(s.ctx, input); updateErr != nil {
				logx.Errorf("im input failure commit failed: input_id=%s error=%v", input.ID, updateErr)
			}
			if _, deliveryErr := s.CreateDelivery(s.ctx, DeliveryRequest{RequestID: "input-failed:" + input.ID, TargetID: input.BindingID, LinkID: input.LinkID, Content: "本次工作未能接纳，请在 NeuxBot 查看配置或失败详情。原因：" + input.ErrorCode}); deliveryErr != nil {
				logx.Errorf("im failure delivery failed: input_id=%s error=%v", input.ID, deliveryErr)
			}
		}
	}()
}

func (s *Service) accept(ctx context.Context, input models.IMInbox) error {
	binding, err := s.config.Store.Binding(ctx, input.BindingID)
	if err != nil {
		return err
	}
	if binding.State != constants.IMBindingStateApproved {
		return status_error.IMChatNotAuthorized
	}
	link, err := s.config.Store.Link(ctx, input.LinkID)
	if err != nil {
		return err
	}
	command, args := parseCommand(input.Content)
	exists, err := s.conversationExists(ctx, link)
	if err != nil {
		return err
	}
	if !exists {
		if command != "" {
			return s.finishInput(ctx, input, binding, "当前对话已不存在。发送工作消息即可开始新对话。")
		}
		link, err = s.replaceMissingConversation(ctx, &input, link)
		if err != nil {
			return err
		}
	}
	if command != "" {
		return s.executeCommand(ctx, input, binding, link, command, args)
	}
	if err := s.downloadInput(ctx, &input); err != nil {
		return err
	}
	options := link.Configuration.Options
	workInput := execution.WorkInput{Actor: string(constants.ChannelMessageActorUser), SubmissionID: input.ID, Content: input.Content}
	if strings.TrimSpace(workInput.Content) == "" {
		workInput.Content = "请处理附带的文件。"
	}
	workInput.ContextInstructions = []workinput.Instruction{{Name: "IM_DELIVERY.md", Content: "完成后文字结果会自动回复到本聊天。如果生成了需要交付的文件，请使用 im_delivery：先 targets.list 查验授权目标，再把文件登记到当前聊天目标 " + binding.ID + "，等待 deliveries.get 返回 sent。文件只能来自当前工作目录。不要通过该工具重复发送完整文字结论。"}}
	for _, file := range input.Files {
		workInput.Attachments = append(workInput.Attachments, execution.AttachmentInput{SourcePath: file.Path})
	}
	if link.Configuration.ChannelID != "" {
		submission := channel.SubmitMessageInput{}
		if link.ThreadID == "" {
			submission.Create = &channel.CreateThreadInput{ChannelID: link.Configuration.ChannelID, ClientRequestID: link.ID, ExecutionID: input.ExecutionID, ProjectID: link.Configuration.ProjectID, WorkingDirectory: link.Configuration.WorkingDirectory, Input: workInput}
			if submission.Create.ProjectID == "" && submission.Create.WorkingDirectory == "" {
				// 团队首发需要确定的共享目录，受管模式由入口按对话身份稳定分配。
				directory := filepath.Join(s.config.DataDirectory, "im", "workspaces", link.ID)
				if err := os.MkdirAll(directory, 0700); err != nil {
					return status_error.IMFileInvalid
				}
				submission.Create.WorkingDirectory = directory
			}
		} else {
			submission.Continue = &channel.SendMessageInput{ThreadID: link.ThreadID, Input: workInput}
		}
		result, err := s.config.Teams.SubmitMessage(ctx, submission)
		if err != nil {
			return err
		}
		if result.Message != nil && result.Message.Execution != nil {
			input.ExecutionID = result.Message.Execution.ID
		} else if result.Message != nil {
			input.ExecutionID = ""
		}
		if result.Thread != nil {
			link.ThreadID = result.Thread.Thread.ID
			link.ChatID = result.Thread.Thread.LeadChatID
		}
	} else {
		intent := execution.WorkIntent{ExecutionID: input.ExecutionID, Input: workInput, Admission: constants.ExecutionAdmissionCommitBeforeDispatch, Options: execution.WorkOptions{ConfigurationResolved: true, RuntimeID: options.RuntimeID, AccountID: options.AccountID, Model: options.Model, Reasoning: options.Reasoning, PermissionLevel: options.PermissionLevel}}
		if link.ChatID == "" {
			intent.ClientRequestID = link.ID
			intent.Chat.Create = &execution.CreateChatSpec{WakerID: link.Configuration.WakerID, ProjectID: link.Configuration.ProjectID, WorkingDirectory: link.Configuration.WorkingDirectory}
		} else {
			intent.Chat.ChatID = link.ChatID
		}
		accepted, err := s.config.Executions.Start(ctx, intent)
		if err != nil {
			return err
		}
		link.ChatID = accepted.Chat.ID
		input.ExecutionID = accepted.Execution.ID
	}
	input.State = constants.IMInboxStateAccepted
	return s.config.Transactions.Transaction(ctx, func(ctx context.Context) error {
		if err := s.config.Store.UpdateLink(ctx, link); err != nil {
			return err
		}
		return s.config.Store.UpdateInbox(ctx, input)
	})
}

// replaceMissingConversation 为接纳前失效的排队输入冻结独立关联，保留原配置及历史收据。
func (s *Service) replaceMissingConversation(ctx context.Context, input *models.IMInbox, previous models.IMConversationLink) (models.IMConversationLink, error) {
	linkID := protocol.StableID("link", previous.ID, input.ID)
	var replacement models.IMConversationLink
	err := s.WithBinding(ctx, input.BindingID, func(ctx context.Context, binding models.IMBinding) error {
		if binding.State != constants.IMBindingStateApproved {
			return status_error.IMChatNotAuthorized
		}
		var err error
		replacement, err = s.config.Store.Link(ctx, linkID)
		if hasCode(err, status_error.IMResourceNotFound) {
			replacement = models.IMConversationLink{ID: linkID, BindingID: binding.ID, Title: shortTitle(input.Content), Configuration: previous.Configuration}
			if err := s.config.Store.SaveLink(ctx, replacement); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		// /new 或显式切换后仍处理原排队输入，但不能把当前选择切回旧工作的替代对话。
		if binding.ActiveLinkID == previous.ID {
			binding.ActiveLinkID = linkID
			if err := s.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
				return err
			}
		}
		input.LinkID = replacement.ID
		return s.config.Store.UpdateInbox(ctx, *input)
	})
	return replacement, err
}

func (s *Service) downloadInput(ctx context.Context, input *models.IMInbox) error {
	if len(input.Files) == 0 {
		return nil
	}
	connection, err := s.config.Store.Connection(ctx, input.ConnectionID)
	if err != nil {
		return err
	}
	platform := s.config.Platforms[connection.Platform]
	if platform == nil {
		return status_error.IMConfigurationInvalid
	}
	directory := filepath.Join(s.config.DataDirectory, "im", "inbox", input.ID)
	if err := os.MkdirAll(directory, 0700); err != nil {
		return status_error.IMFileInvalid
	}
	for index := range input.Files {
		file := &input.Files[index]
		if file.Path != "" {
			continue
		}
		value, err := platform.Download(ctx, connection, input.ExternalMessageID, *file)
		if err != nil {
			return err
		}
		file.Name = protocol.SafeFilename(file.Name)
		file.Path = filepath.Join(directory, fmt.Sprintf("%02d-%s", index, file.Name))
		if err := os.WriteFile(file.Path, value, 0600); err != nil {
			return status_error.IMFileInvalid
		}
		file.AESKey = ""
	}
	return s.config.Store.UpdateInbox(ctx, *input)
}

func (s *Service) executeCommand(ctx context.Context, input models.IMInbox, binding models.IMBinding, link models.IMConversationLink, command, args string) error {
	if !allowedCommand(binding, input.SenderID, command) {
		return s.finishInput(ctx, input, binding, "此命令需要聊天管理员操作。")
	}
	switch command {
	case "/status":
		if link.ChatID == "" {
			return s.finishInput(ctx, input, binding, "对话正在准备，工作尚未接纳。")
		}
		summary, err := s.config.Chats.GetChatSummary(ctx, link.ChatID)
		if err != nil {
			return err
		}
		state := "空闲"
		if summary.Chat.ActiveExecutionID != "" {
			state = "执行中"
		}
		if len(summary.Chat.PendingApprovals) > 0 {
			state = "等待工具审批"
		}
		if len(summary.Chat.PendingElicitations) > 0 {
			state = "等待补充信息"
		}
		if link.ThreadID != "" {
			pending, members, err := s.config.Teams.ListPendingInteractions(ctx, link.ThreadID)
			if err != nil {
				return err
			}
			tasks, err := s.config.TeamTasks.ListByThread(ctx, link.ThreadID)
			if err != nil {
				return err
			}
			running, waiting := 0, 0
			for _, task := range tasks {
				switch task.Status {
				case constants.ChannelTaskStatusInProgress:
					running++
				case constants.ChannelTaskStatusPending:
					waiting++
				}
			}
			state += fmt.Sprintf("\n团队：%d 项任务运行中，%d 项待派发，%d 项待交互，%d 位成员。", running, waiting, len(pending), len(members))
		}
		return s.finishInput(ctx, input, binding, link.Title+"\n状态："+state)
	case "/stop":
		if link.ThreadID != "" {
			err := s.config.Teams.StopWork(ctx, link.Configuration.ChannelID, link.ThreadID)
			if err != nil {
				return err
			}
		} else if link.ChatID != "" {
			if err := s.config.Executions.Cancel(link.ChatID); err != nil {
				return err
			}
		}
		return s.finishInput(ctx, input, binding, "已停止当前工作，对话仍可继续。")
	case "/approve", "/answer":
		return s.resolveInteraction(ctx, input, binding, link, command, args)
	default:
		return s.finishInput(ctx, input, binding, "无法识别该命令，请发送 /help 查看可用命令。")
	}
}
