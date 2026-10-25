package controllers

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

// boardAttentionFact 补充通知需要的原验收围栏和人工输入期限，不持久化派生状态。
type boardAttentionFact struct {
	// Attention 是工作看板和通知共用的有效请求定位。
	Attention models.WorkBoardAttention
	// Fence 是原团队任务当前验收版本。
	Fence int64
	// Deadline 是人工输入的到期时间，零表示不限期。
	Deadline int64
}

// boardAttentionPageKey 只保留深页排序需要的身份，不保留标题、请求正文和关联对象。
type boardAttentionPageKey struct {
	// ID 是有效请求的原始身份，用于补齐选中页。
	ID string
	// OrderID 是当前入口的稳定排序身份。
	OrderID string
	// CreatedAt 是请求时间。
	CreatedAt int64
	// OwnerKind 是归属容器种类。
	OwnerKind constants.WorkResultOwnerKind
	// OwnerID 是归属容器身份，用于限制第二次读取。
	OwnerID string
}

func attentionPageKey(a models.WorkBoardAttention, orderID string) boardAttentionPageKey {
	return boardAttentionPageKey{ID: a.ID, OrderID: orderID, CreatedAt: a.CreatedAt, OwnerKind: a.OwnerKind, OwnerID: a.OwnerID}
}

// attentionPageFacts 仅为选中页的原容器分批补齐请求，沿用首次扫描的有效性时刻。
func (c *workBoardController) attentionPageFacts(ctx context.Context, at int64, keys []boardAttentionPageKey) ([]boardAttentionFact, error) {
	facts := make([]boardAttentionFact, len(keys))
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
	err := c.walkAttention(ctx, at, owners, func(f boardAttentionFact) error {
		if i, ok := positions[f.Attention.ID]; ok {
			facts[i] = f
		}
		return nil
	})
	return facts, err
}

// walkAttention 按原表主键分批遍历当前等待集合，不积累全部待办或关联正文。
func (c *workBoardController) walkAttention(ctx context.Context, at int64, owners []models.WorkBoardEntry, visit func(boardAttentionFact) error) error {
	db := controllerDB(ctx, c.db)
	// OR 等待条件会让 SQLite 误选两个布尔索引并在每批重复排序；该部分索引按主键有序续批。
	chats := db.Model(&models.Chat{}).Table("chats INDEXED BY idx_chat_pending_id").Select("id,waker_id,title,active_execution_id,waiting_approval,waiting_elicitation,pending_approvals,pending_elicitations,hidden_from_waker_history").Where("active_execution_id<>'' AND (waiting_approval=1 OR waiting_elicitation=1)")
	tasks := db.Model(&models.ChannelTask{}).Select("id,thread_id,assignee_waker_id,subject,current_fence,finished_at,updated_at").Where("status = ?", constants.ChannelTaskStatusInReview)
	humans := db.Model(&models.WorkflowHumanRequest{}).Select("id,run_id,prompt,created_at,deadline_at").Where("status='waiting' AND (deadline_at=0 OR deadline_at>?)", at)
	operations := db.Model(&models.WorkflowOperationRequest{}).Select("id,run_id,description,created_at").Where("status='waiting'")
	if owners != nil {
		chatIDs, threadIDs, rootIDs := []string{}, []string{}, []string{}
		for _, owner := range owners {
			switch owner.OwnerKind {
			case constants.WorkResultOwnerKindChat:
				chatIDs = append(chatIDs, owner.OwnerID)
			case constants.WorkResultOwnerKindTeamThread:
				threadIDs = append(threadIDs, owner.OwnerID)
			case constants.WorkResultOwnerKindWorkflowRun:
				rootIDs = append(rootIDs, owner.OwnerID)
			}
		}
		runs := db.Model(&models.WorkflowRun{}).Select("id").Where("id IN ? OR root_run_id IN ?", rootIDs, rootIDs)
		members := db.Model(&models.ChannelThreadMember{}).Select("chat_id").Where("thread_id IN ?", threadIDs)
		workers := db.Model(&models.WorkflowEffect{}).Select("chat_id").Where("run_id IN (?)", runs)
		chats = chats.Where("id IN ? OR id IN (?) OR id IN (?)", chatIDs, members, workers)
		tasks = tasks.Where("thread_id IN ?", threadIDs)
		humans = humans.Where("run_id IN (?)", runs)
		operations = operations.Where("run_id IN (?)", runs)
	}
	emit := func(chats []models.Chat, tasks []models.ChannelTask, humans []models.WorkflowHumanRequest, operations []models.WorkflowOperationRequest) error {
		facts, err := c.attentionBatch(ctx, chats, tasks, humans, operations)
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

// attentionBatch 只为当前候选批补齐归属，在 Go 中统一解析生命周期与真实请求身份。
func (c *workBoardController) attentionBatch(ctx context.Context, chats []models.Chat, tasks []models.ChannelTask, humans []models.WorkflowHumanRequest, operations []models.WorkflowOperationRequest) ([]boardAttentionFact, error) {
	db := controllerDB(ctx, c.db)
	chatIDs, wakerIDs, threadIDs, runIDs, definitionIDs := []string{}, []string{}, []string{}, []string{}, []string{}
	for _, v := range chats {
		chatIDs = append(chatIDs, v.ID)
		wakerIDs = append(wakerIDs, v.WakerID)
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
	for _, task := range tasks {
		reviewKeys = append(reviewKeys, []any{task.ThreadID, task.AssigneeWakerID})
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
	// 只补齐当前执行的效果；历史关系仅用于排除已退役 worker，不读取历史效果内容。
	effectsByChat := map[string]map[string]string{}
	for start := 0; start < len(chats); start += repositoryReadBatch {
		keys := [][]any{}
		for _, chat := range chats[start:min(start+repositoryReadBatch, len(chats))] {
			keys = append(keys, []any{chat.ID, chat.ActiveExecutionID})
		}
		var effects []models.WorkflowEffect
		if err := db.Select("chat_id,execution_id,run_id").Where("(chat_id,execution_id) IN ?", keys).Find(&effects).Error; err != nil {
			return nil, databaseError("read_pending_workflow_effects", "", err)
		}
		for _, effect := range effects {
			if effectsByChat[effect.ChatID] == nil {
				effectsByChat[effect.ChatID] = map[string]string{}
			}
			effectsByChat[effect.ChatID][effect.ExecutionID] = effect.RunID
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
		runIDs = append(runIDs, effectsByChat[v.ID][v.ActiveExecutionID])
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
	receipts, err := readRelatedRows[models.WorkResultReceipt](db.Where("owner_kind = ?", constants.WorkResultOwnerKindChat), "owner_id", "owner_id", chatIDs)
	if err != nil {
		return nil, err
	}
	hasReceipt := map[string]bool{}
	for _, v := range receipts {
		hasReceipt[v.OwnerID] = true
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
		if run.RootRunID != "" {
			return runs[run.RootRunID]
		}
		return run
	}
	facts := []boardAttentionFact{}
	for _, chat := range chats {
		member, hasMember := memberByChat[chat.ID]
		thread := threadByID[member.ThreadID]
		if hasMember && thread.ReadyAt == nil {
			continue
		}
		runID, hasEffect := effectsByChat[chat.ID][chat.ActiveExecutionID]
		run := runs[runID]
		root := rootOf(run)
		definition := definitionByID[root.DefinitionID]
		if hasEffect && (run.ID == "" || root.ID == "" || definition.ID == "" || !boardWorkflowAlive(run, runs)) {
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
		if !hasMember && !hasEffect && !hasCreation && chat.HiddenFromWakerHistory && chatSources[chat.ID] == "" && !hasReceipt[chat.ID] {
			continue
		}
		a := models.WorkBoardAttention{OwnerKind: constants.WorkResultOwnerKindChat, OwnerID: chat.ID, WorkTitle: chat.Title, ExecutorID: chat.WakerID, ActorName: wakerNames[chat.WakerID], Source: "chat", ChatID: chat.ID, WakerID: chat.WakerID}
		if hasCreation {
			a.WorkTitle = creation.Title
			a.ExecutorID = creation.ID
			a.WorkflowID = creation.ID
			a.Source = "workflow"
		}
		if hasEffect {
			a.OwnerKind = constants.WorkResultOwnerKindWorkflowRun
			a.OwnerID = root.ID
			a.WorkTitle = definition.Title
			a.ExecutorID = definition.ID
			a.WorkflowID = run.DefinitionID
			a.RunID = run.ID
			a.Source = "workflow"
		}
		if hasMember {
			a.OwnerKind = constants.WorkResultOwnerKindTeamThread
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
				v.ID = "approval:" + chat.ID + ":" + r.ApprovalID
				v.Kind = "approval"
				v.Title = r.Title
				v.CreatedAt = r.RequestedAt
				facts = append(facts, boardAttentionFact{Attention: v})
			}
		}
		if chat.WaitingElicitation {
			for _, r := range chat.PendingElicitations {
				v := a
				v.ID = "input:" + chat.ID + ":" + r.ElicitationID
				v.Kind = "input"
				v.Title = r.Message
				v.CreatedAt = r.RequestedAt
				facts = append(facts, boardAttentionFact{Attention: v})
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
		facts = append(facts, boardAttentionFact{Fence: int64(task.CurrentFence), Attention: models.WorkBoardAttention{ID: "review:" + task.ID, Kind: "review", OwnerKind: constants.WorkResultOwnerKindTeamThread, OwnerID: thread.ID, Title: task.Subject, WorkTitle: thread.Title, ExecutorID: thread.ChannelID, ActorName: member.DisplayName, Source: source("team", threadSources[thread.ID]), ChatID: member.ChatID, WakerID: task.AssigneeWakerID, ChannelID: thread.ChannelID, TaskID: task.ID, CreatedAt: created}})
	}
	workflowRequest := func(id, kind, title, runID string, created, deadline int64) {
		run := runs[runID]
		root := rootOf(run)
		definition := definitionByID[root.DefinitionID]
		target := definitionByID[run.DefinitionID]
		if run.ID == "" || root.ID == "" || definition.ID == "" || target.ID == "" || !boardWorkflowAlive(run, runs) {
			return
		}
		if kind == "approval" && created == 0 {
			created = run.UpdatedAt
		}
		facts = append(facts, boardAttentionFact{Deadline: deadline, Attention: models.WorkBoardAttention{ID: kind + ":" + id, Kind: kind, OwnerKind: constants.WorkResultOwnerKindWorkflowRun, OwnerID: root.ID, Title: title, WorkTitle: definition.Title, ExecutorID: definition.ID, ActorName: target.Title, Source: source("workflow", runSources[root.ID]), WorkflowID: run.DefinitionID, RunID: run.ID, CreatedAt: created}})
	}
	for _, r := range humans {
		workflowRequest(r.ID, "input", r.Prompt, r.RunID, r.CreatedAt, r.DeadlineAt)
	}
	for _, r := range operations {
		workflowRequest(r.ID, "approval", r.Description, r.RunID, r.CreatedAt, 0)
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
	return (len(q.States) == 0 || slices.Contains(q.States, a.Kind)) && (q.ExecutorID == "" || q.ExecutorID == a.ExecutorID) && (q.Source == "" || q.Source == a.Source) && (q.Query == "" || boardContains(a.WorkTitle, q.Query))
}

// AttentionOverview 合并顶部概览的请求数、工作数和预览，不重复扫描当前等待集合。
func (c *workBoardController) AttentionOverview(ctx context.Context, q WorkBoardQuery) ([]models.WorkBoardAttention, int64, int64, error) {
	q.Query, q.Cursor, q.Offset = "", nil, 0
	return c.attentionPage(ctx, q, true)
}

// Attention 分批计数，仅保留排序所需的分页候选，不积累完整请求集合。
func (c *workBoardController) Attention(ctx context.Context, q WorkBoardQuery) ([]models.WorkBoardAttention, int64, error) {
	items, total, _, err := c.attentionPage(ctx, q, false)
	return items, total, err
}

// attentionPage 在同一候选遍历中按需统计原容器，仅为选中页补齐请求详情。
func (c *workBoardController) attentionPage(ctx context.Context, q WorkBoardQuery, countOwners bool) ([]models.WorkBoardAttention, int64, int64, error) {
	var owners map[[2]string]struct{}
	if countOwners {
		owners = map[[2]string]struct{}{}
	}
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
	err := c.walkAttention(ctx, at, nil, func(f boardAttentionFact) error {
		a := f.Attention
		if !boardAttentionMatches(a, q) {
			return nil
		}
		total++
		if countOwners {
			owners[[2]string{string(a.OwnerKind), a.OwnerID}] = struct{}{}
		}
		if v := q.Cursor; v != nil && (a.CreatedAt < v.At || (a.CreatedAt == v.At && (a.OwnerKind < v.Kind || (a.OwnerKind == v.Kind && a.ID <= v.ID)))) {
			return nil
		}
		window.add(attentionPageKey(a, a.ID))
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}
	facts, err := c.attentionPageFacts(ctx, at, window.page())
	items := make([]models.WorkBoardAttention, 0, len(facts))
	for _, fact := range facts {
		if fact.Attention.ID != "" {
			items = append(items, fact.Attention)
		}
	}
	return items, total, int64(len(owners)), err
}

// attachAttentionCounts 仅查询本页容器的待办；无关工作不会进入关联读取。
func (c *workBoardController) attachAttentionCounts(ctx context.Context, values []models.WorkBoardEntry) error {
	if len(values) == 0 {
		return nil
	}
	positions := map[string]int{}
	for i, v := range values {
		positions[string(v.OwnerKind)+":"+v.OwnerID] = i
	}
	return c.walkAttention(ctx, time.Now().UnixMilli(), values, func(f boardAttentionFact) error {
		a := f.Attention
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
