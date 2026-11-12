package controllers

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"gorm.io/gorm"
)

// boardAttentionPageKey 只保留深页排序需要的身份，不保留标题、请求正文和关联对象。
type boardAttentionPageKey struct {
	// ID 是有效请求的原始身份，用于补齐选中页。
	ID string
	// CreatedAt 是请求时间。
	CreatedAt int64
	// OwnerKind 是归属容器种类。
	OwnerKind constants.WorkOwnerKind
	// OwnerID 是归属容器身份，用于限制第二次读取。
	OwnerID string
}

// attentionPageFacts 仅为选中页的原容器分批补齐请求，沿用首次扫描的有效性时刻。
func (c *workBoardController) attentionPageFacts(ctx context.Context, at int64, keys []boardAttentionPageKey) ([]models.WorkBoardAttention, error) {
	facts := make([]models.WorkBoardAttention, len(keys))
	if len(keys) == 0 {
		return facts, nil
	}
	positions := map[string]int{}
	owners := []models.WorkBoardEntry{}
	seen := map[string]bool{}
	for i, key := range keys {
		positions[key.ID] = i
		identity := string(key.OwnerKind) + ":" + key.OwnerID
		if !seen[identity] {
			seen[identity] = true
			owners = append(owners, models.WorkBoardEntry{OwnerKind: key.OwnerKind, OwnerID: key.OwnerID})
		}
	}
	err := c.walkAttention(ctx, at, boardAttentionScope{owners: owners}, true, func(f models.WorkBoardAttention) error {
		if i, ok := positions[f.ID]; ok {
			facts[i] = f
		}
		return nil
	})
	return facts, err
}

const pendingChatPredicate = "active_execution_id<>'' AND (waiting_approval=1 OR waiting_elicitation=1)"

// boardPendingQueries 是全局扫描与精确定位共用的等待候选查询。
func boardPendingQueries(db *gorm.DB, at int64) (chats, tasks, humans, operations *gorm.DB) {
	// OR 等待条件会让 SQLite 误选两个布尔索引并在每批重复排序；该部分索引按主键有序续批。
	chats = db.Model(&models.Chat{}).Table("chats INDEXED BY idx_chat_pending_id").Select("id,waker_id,title,active_execution_id,waiting_approval,waiting_elicitation,pending_approvals,pending_elicitations,hidden_from_waker_history").Where(pendingChatPredicate)
	tasks = db.Model(&models.ChannelTask{}).Select("id,thread_id,assignee_waker_id,subject,current_fence,finished_at,updated_at").Where("status = ?", constants.ChannelTaskStatusInReview)
	humans = db.Model(&models.WorkflowHumanRequest{}).Select("id,run_id,instance_id,prompt,created_at,deadline_at").Where("status='waiting' AND (deadline_at=0 OR deadline_at>?)", at)
	operations = db.Model(&models.WorkflowOperationRequest{}).Select("id,run_id,instance_id,description,created_at").Where("status='waiting'")
	return
}

type boardAttentionScope struct {
	// owners 限定看板当前页需要补齐的原容器。
	owners []models.WorkBoardEntry
	// executors 限定摘要请求中的伙伴、团队和工作流定义标识。
	executors []string
}

// walkAttention 按原表主键分批遍历当前等待集合，不积累全部待办或关联正文。
func (c *workBoardController) walkAttention(ctx context.Context, at int64, scope boardAttentionScope, details bool, visit func(models.WorkBoardAttention) error) error {
	db := controllerDB(ctx, c.db)
	chats, tasks, humans, operations := boardPendingQueries(db, at)
	if scope.owners != nil || scope.executors != nil {
		direct := db.Model(&models.Chat{}).Select("id")
		threads := db.Model(&models.ChannelThread{}).Select("id")
		visibleRuns := db.Model(&models.WorkflowRun{})
		if scope.executors != nil {
			direct = direct.Table("chats chat INDEXED BY idx_chat_pending_waker").Where(pendingChatPredicate).
				Where("waker_id IN ?", scope.executors).Where(workBoardIndependentChat).
				Where("NOT EXISTS (SELECT 1 FROM workflow_effects WHERE execution_id=chat.active_execution_id AND " + unboundWorkerEffect + ")")
			threads = threads.Where("channel_id IN ? AND ready_at IS NOT NULL", scope.executors)
			visibleRuns = visibleRuns.Where("definition_id IN ? AND (parent_run_id='' OR NOT joined) AND history_deleted_at IS NULL", scope.executors)
		} else {
			chatIDs, threadIDs, runIDs := []string{}, []string{}, []string{}
			for _, owner := range scope.owners {
				switch owner.OwnerKind {
				case constants.WorkOwnerKindChat:
					chatIDs = append(chatIDs, owner.OwnerID)
				case constants.WorkOwnerKindTeamThread:
					threadIDs = append(threadIDs, owner.OwnerID)
				case constants.WorkOwnerKindWorkflowRun:
					runIDs = append(runIDs, owner.OwnerID)
				}
			}
			direct = direct.Where("id IN ?", chatIDs)
			threads = threads.Where("id IN ?", threadIDs)
			visibleRuns = visibleRuns.Where("id IN ?", runIDs)
		}
		// detached 运行及其 joined 后代仍保存顶层调用树 ID。
		// 候选受 WorkflowMaxChildren 约束，返回前再校验准确的可见归属。
		trees := visibleRuns.Session(&gorm.Session{}).Select("COALESCE(NULLIF(root_run_id,''),id)")
		runs := db.Model(&models.WorkflowRun{}).Select("id").Where("id IN (?) OR root_run_id IN (?)", visibleRuns.Select("id"), trees)
		members := db.Model(&models.ChannelThreadMember{}).Select("chat_id").Where("thread_id IN (?)", threads)
		workers := db.Model(&models.WorkflowEffect{}).Select("chat_id").Where("run_id IN (?) AND chat_id<>''", runs)
		unboundWorkers := db.Model(&models.WorkflowEffect{}).Select("execution_id").Where("run_id IN (?) AND "+unboundWorkerEffect, runs)
		unboundChats := db.Model(&models.Chat{}).Select("id").Where("active_execution_id IN (?)", unboundWorkers)
		// 合并窄身份后按待办索引点查，避免 OR 过滤扫描其他对象的待办。
		candidates := db.Raw("? UNION ALL ? UNION ALL ? UNION ALL ?", direct, members, workers, unboundChats)
		chats = chats.Where("id IN (?)", candidates)
		tasks = tasks.Where("thread_id IN (?)", threads)
		humans = humans.Where("run_id IN (?)", runs)
		operations = operations.Where("run_id IN (?)", runs)
	}
	emit := func(chats []models.Chat, tasks []models.ChannelTask, humans []models.WorkflowHumanRequest, operations []models.WorkflowOperationRequest) error {
		facts, err := c.attentionBatch(ctx, at, chats, tasks, humans, operations, details)
		if err != nil {
			return err
		}
		for _, fact := range facts {
			if err := visit(fact); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walkRepositoryRows(chats, func(rows []models.Chat) error { return emit(rows, nil, nil, nil) }); err != nil {
		return err
	}
	if err := walkRepositoryRows(tasks, func(rows []models.ChannelTask) error { return emit(nil, rows, nil, nil) }); err != nil {
		return err
	}
	if err := walkRepositoryRows(humans, func(rows []models.WorkflowHumanRequest) error { return emit(nil, nil, rows, nil) }); err != nil {
		return err
	}
	return walkRepositoryRows(operations, func(rows []models.WorkflowOperationRequest) error { return emit(nil, nil, nil, rows) })
}

const unboundWorkerEffect = "chat_id='' AND json_extract(boundary,'$.primitive')='worker'"

// attentionBatch 只为当前候选批补齐归属，在 Go 中统一解析生命周期与真实请求身份。
func (c *workBoardController) attentionBatch(ctx context.Context, at int64, chats []models.Chat, tasks []models.ChannelTask, humans []models.WorkflowHumanRequest, operations []models.WorkflowOperationRequest, details bool) ([]models.WorkBoardAttention, error) {
	db := controllerDB(ctx, c.db)
	chatIDs, wakerIDs, threadIDs, runIDs, definitionIDs := []string{}, []string{}, []string{}, []string{}, []string{}
	for _, v := range chats {
		chatIDs = append(chatIDs, v.ID)
		if details {
			wakerIDs = append(wakerIDs, v.WakerID)
		}
	}
	members, err := readRelatedRows[models.ChannelThreadMember](db, "chat_id", "thread_id,chat_id,waker_id,display_name", chatIDs)
	if err != nil {
		return nil, err
	}
	memberByChat := map[string]models.ChannelThreadMember{}
	for _, v := range members {
		memberByChat[v.ChatID] = v
		threadIDs = append(threadIDs, v.ThreadID)
	}
	for _, v := range tasks {
		threadIDs = append(threadIDs, v.ThreadID)
	}
	reviewKeys := [][]any{}
	if details {
		for _, task := range tasks {
			reviewKeys = append(reviewKeys, []any{task.ThreadID, task.AssigneeWakerID})
		}
	}
	var reviewMembers []models.ChannelThreadMember
	if len(reviewKeys) > 0 {
		if err := db.Select("thread_id,chat_id,waker_id,display_name").Where("(thread_id,waker_id) IN ?", reviewKeys).Find(&reviewMembers).Error; err != nil {
			return nil, databaseError("read_review_members", "", err)
		}
	}
	memberByActor := map[string]models.ChannelThreadMember{}
	for _, v := range reviewMembers {
		memberByActor[v.ThreadID+":"+v.WakerID] = v
	}
	threads, err := readRelatedRows[models.ChannelThread](db, "id", "id,channel_id,title,ready_at", threadIDs)
	if err != nil {
		return nil, err
	}
	threadByID := map[string]models.ChannelThread{}
	for _, v := range threads {
		threadByID[v.ID] = v
	}
	// Worker 的 ExecutionID 在派发前已写入，首个请求可能先于 ChatID 回执。
	effectsByChat := map[string]string{}
	for start := 0; start < len(chats); start += repositoryReadBatch {
		executionIDs := []string{}
		chatByExecution := map[string]string{}
		for _, chat := range chats[start:min(start+repositoryReadBatch, len(chats))] {
			executionIDs = append(executionIDs, chat.ActiveExecutionID)
			chatByExecution[chat.ActiveExecutionID] = chat.ID
		}
		var effects []models.WorkflowEffect
		if err := db.Select("chat_id,execution_id,run_id").Where("execution_id IN ? AND (chat_id<>'' OR ("+unboundWorkerEffect+"))", executionIDs).Find(&effects).Error; err != nil {
			return nil, databaseError("read_pending_workflow_effects", "", err)
		}
		for _, effect := range effects {
			chatID := chatByExecution[effect.ExecutionID]
			if effect.ChatID == "" || effect.ChatID == chatID {
				effectsByChat[chatID] = effect.RunID
			}
		}
	}
	var oldEffects []models.Chat
	if len(chatIDs) > 0 {
		if err := db.Select("id").Where("id IN ?", chatIDs).Where("EXISTS (SELECT 1 FROM workflow_effects WHERE chat_id=chats.id)").Find(&oldEffects).Error; err != nil {
			return nil, databaseError("read_worker_membership", "", err)
		}
	}
	hasEffectHistory := map[string]bool{}
	for _, chat := range oldEffects {
		hasEffectHistory[chat.ID] = true
	}
	for _, v := range chats {
		runIDs = append(runIDs, effectsByChat[v.ID])
	}
	for _, v := range humans {
		runIDs = append(runIDs, v.RunID)
	}
	for _, v := range operations {
		runIDs = append(runIDs, v.RunID)
	}
	runs, err := boardWorkflowRuns(db, runIDs)
	if err != nil {
		return nil, err
	}
	for _, v := range runs {
		definitionIDs = append(definitionIDs, v.DefinitionID)
		runIDs = append(runIDs, v.ID)
	}
	creations, err := readRelatedRows[models.WorkflowCreationSession](db, "chat_id", "chat_id,definition_id", chatIDs)
	if err != nil {
		return nil, err
	}
	creationByChat := map[string]string{}
	for _, v := range creations {
		creationByChat[v.ChatID] = v.DefinitionID
		definitionIDs = append(definitionIDs, v.DefinitionID)
	}
	definitions, err := readRelatedRows[models.WorkflowDefinition](db, "id", "id,title", definitionIDs)
	if err != nil {
		return nil, err
	}
	definitionByID := map[string]models.WorkflowDefinition{}
	for _, v := range definitions {
		definitionByID[v.ID] = v
	}
	wakers, err := readRelatedRows[models.Waker](db, "id", "id,name", wakerIDs)
	if err != nil {
		return nil, err
	}
	wakerNames := map[string]string{}
	for _, v := range wakers {
		wakerNames[v.ID] = v.Name
	}
	chatSources, err := boardSources(db, "chat_id", chatIDs)
	if err != nil {
		return nil, err
	}
	threadSources, err := boardSources(db, "thread_id", threadIDs)
	if err != nil {
		return nil, err
	}
	runSources, err := boardSources(db, "workflow_run_id", runIDs)
	if err != nil {
		return nil, err
	}
	source := func(fallback string, values ...string) string {
		for _, v := range values {
			if v == "automatic" {
				return v
			}
		}
		for _, v := range values {
			if v == "im" {
				return v
			}
		}
		return fallback
	}
	rootOf := func(run models.WorkflowRun) models.WorkflowRun {
		return visibleWorkflowRun(run, runs)
	}
	facts := []models.WorkBoardAttention{}
	for _, chat := range chats {
		member, hasMember := memberByChat[chat.ID]
		thread := threadByID[member.ThreadID]
		if hasMember && thread.ReadyAt == nil {
			continue
		}
		runID, hasEffect := effectsByChat[chat.ID]
		run := runs[runID]
		root := rootOf(run)
		definition := definitionByID[root.DefinitionID]
		if hasEffect && (run.ID == "" || root.ID == "" || definition.ID == "" || !boardWorkflowAlive(run, runs, at)) {
			continue
		}
		if !hasEffect && hasEffectHistory[chat.ID] {
			continue
		}
		creationID, hasCreation := creationByChat[chat.ID]
		creation := definitionByID[creationID]
		if hasCreation && creation.ID == "" {
			continue
		}
		if !hasMember && !hasEffect && !hasCreation && chat.HiddenFromWakerHistory && chatSources[chat.ID] == "" {
			continue
		}
		a := models.WorkBoardAttention{OwnerKind: constants.WorkOwnerKindChat, OwnerID: chat.ID, WorkTitle: chat.Title, ExecutorID: chat.WakerID, ActorName: wakerNames[chat.WakerID], Source: "chat", ChatID: chat.ID, WakerID: chat.WakerID}
		if hasCreation {
			a.WorkTitle = creation.Title
			a.WorkflowID = creation.ID
			a.Source = "workflow"
		}
		if hasEffect {
			a.OwnerKind = constants.WorkOwnerKindWorkflowRun
			a.OwnerID = root.ID
			a.WorkTitle = definition.Title
			a.ExecutorID = definition.ID
			a.WorkflowID = run.DefinitionID
			a.RunID = run.ID
			a.Source = "workflow"
		}
		if hasMember {
			a.OwnerKind = constants.WorkOwnerKindTeamThread
			a.OwnerID = thread.ID
			a.WorkTitle = thread.Title
			a.ExecutorID = thread.ChannelID
			a.ChannelID = thread.ChannelID
			a.Source = "team"
		}
		a.Source = source(a.Source, chatSources[chat.ID], threadSources[thread.ID], runSources[root.ID])
		if chat.WaitingApproval {
			for _, r := range chat.PendingApprovals {
				v := a
				v.ID = "approval:" + chat.ID + ":" + chat.ActiveExecutionID + ":" + r.ApprovalID
				v.RequestID, v.ExecutionID, v.RequestSequence = r.ApprovalID, chat.ActiveExecutionID, r.RequestedSequence
				v.Kind = "approval"
				v.Title = r.Title
				v.CreatedAt = r.RequestedAt
				facts = append(facts, v)
			}
		}
		if chat.WaitingElicitation {
			for _, r := range chat.PendingElicitations {
				v := a
				v.ID = "input:" + chat.ID + ":" + chat.ActiveExecutionID + ":" + r.ElicitationID
				v.RequestID, v.ExecutionID, v.RequestSequence = r.ElicitationID, chat.ActiveExecutionID, r.RequestedSequence
				v.Kind = "input"
				v.Title = r.Message
				v.CreatedAt = r.RequestedAt
				facts = append(facts, v)
			}
		}
	}
	for _, task := range tasks {
		thread := threadByID[task.ThreadID]
		if thread.ReadyAt == nil {
			continue
		}
		member := memberByActor[task.ThreadID+":"+task.AssigneeWakerID]
		created := task.UpdatedAt
		if task.FinishedAt != nil {
			created = *task.FinishedAt
		}
		facts = append(facts, models.WorkBoardAttention{ID: "review:" + task.ID, RequestID: task.ID, RequestFence: int64(task.CurrentFence), Kind: "review", OwnerKind: constants.WorkOwnerKindTeamThread, OwnerID: thread.ID, Title: task.Subject, WorkTitle: thread.Title, ExecutorID: thread.ChannelID, ActorName: member.DisplayName, Source: source("team", threadSources[thread.ID]), ChatID: member.ChatID, WakerID: task.AssigneeWakerID, ChannelID: thread.ChannelID, TaskID: task.ID, CreatedAt: created})
	}
	workflowRequest := func(id, kind, title, runID, instanceID string, created int64) {
		run := runs[runID]
		root := rootOf(run)
		definition := definitionByID[root.DefinitionID]
		target := definitionByID[run.DefinitionID]
		if run.ID == "" || root.ID == "" || definition.ID == "" || target.ID == "" || !boardWorkflowAlive(run, runs, at) {
			return
		}
		if kind == "approval" && created == 0 {
			created = run.UpdatedAt
		}
		facts = append(facts, models.WorkBoardAttention{ID: kind + ":" + id, RequestID: id, ExecutionID: runID, InstanceID: instanceID, Kind: kind, OwnerKind: constants.WorkOwnerKindWorkflowRun, OwnerID: root.ID, Title: title, WorkTitle: definition.Title, ExecutorID: definition.ID, ActorName: target.Title, Source: source("workflow", runSources[root.ID]), WorkflowID: run.DefinitionID, RunID: run.ID, CreatedAt: created})
	}
	for _, r := range humans {
		workflowRequest(r.ID, "input", r.Prompt, r.RunID, r.InstanceID, r.CreatedAt)
	}
	for _, r := range operations {
		workflowRequest(r.ID, "approval", r.Description, r.RunID, r.InstanceID, r.CreatedAt)
	}
	return facts, nil
}

// boardContains 保持 SQLite 默认 LIKE 的 ASCII 大小写行为，并将通配符视为用户文字。
func boardContains(text, query string) bool {
	fold := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' {
				return r + ('a' - 'A')
			}
			return r
		}, s)
	}
	return strings.Contains(fold(text), fold(query))
}

func boardAttentionMatches(a models.WorkBoardAttention, q WorkBoardQuery) bool {
	return (a.Kind == "review" || a.ChatID == "" || !slices.Contains(q.IsolatedChats, a.ChatID)) && (len(q.States) == 0 || slices.Contains(q.States, a.Kind)) && (q.ExecutorID == "" || q.ExecutorID == a.ExecutorID) && (q.Source == "" || q.Source == a.Source) && (q.Query == "" || boardContains(a.WorkTitle, q.Query))
}

// Attention 在一次候选遍历中统计请求和原容器，仅为选中页补齐请求详情。
func (c *workBoardController) Attention(ctx context.Context, q WorkBoardQuery) ([]models.WorkBoardAttention, int64, int64, error) {
	owners := map[models.WorkRef]struct{}{}
	window := newRepositoryPage[boardAttentionPageKey](q.Offset, q.Limit+1, func(a, b boardAttentionPageKey) bool {
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt < b.CreatedAt
		}
		if a.OwnerKind != b.OwnerKind {
			return a.OwnerKind < b.OwnerKind
		}
		return a.ID < b.ID
	})
	var total int64
	at := time.Now().UnixMilli()
	err := c.walkAttention(ctx, at, boardAttentionScope{}, false, func(a models.WorkBoardAttention) error {
		if !boardAttentionMatches(a, q) {
			return nil
		}
		total++
		owners[models.WorkRef{Kind: a.OwnerKind, ID: a.OwnerID}] = struct{}{}
		if q.Limit == 0 || q.Cursor != nil && (a.CreatedAt < q.Cursor.At || (a.CreatedAt == q.Cursor.At && (a.OwnerKind < q.Cursor.Kind || (a.OwnerKind == q.Cursor.Kind && a.ID <= q.Cursor.ID)))) {
			return nil
		}
		window.add(boardAttentionPageKey{ID: a.ID, OwnerKind: a.OwnerKind, OwnerID: a.OwnerID, CreatedAt: a.CreatedAt})
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	facts, err := c.attentionPageFacts(ctx, at, window.page())
	items := make([]models.WorkBoardAttention, 0, len(facts))
	for _, fact := range facts {
		if fact.ID != "" {
			items = append(items, fact)
		}
	}
	return items, total, int64(len(owners)), err
}

// attachAttentionCounts 仅查询本页容器的待办；无关工作不会进入关联读取。
func (c *workBoardController) attachAttentionCounts(ctx context.Context, values []models.WorkBoardEntry, isolated []string) error {
	if len(values) == 0 {
		return nil
	}
	positions := map[string]int{}
	for i, v := range values {
		positions[string(v.OwnerKind)+":"+v.OwnerID] = i
	}
	return c.walkAttention(ctx, time.Now().UnixMilli(), boardAttentionScope{owners: values}, false, func(a models.WorkBoardAttention) error {
		if a.Kind != "review" && a.ChatID != "" && slices.Contains(isolated, a.ChatID) {
			return nil
		}
		i, ok := positions[string(a.OwnerKind)+":"+a.OwnerID]
		if !ok {
			return nil
		}
		switch a.Kind {
		case "approval":
			values[i].ApprovalCount++
		case "input":
			values[i].InputCount++
		case "review":
			values[i].ReviewCount++
		}
		return nil
	})
}
