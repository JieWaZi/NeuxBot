package global

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/agent"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/memory"
	"github.com/JieWaZi/NeuxBot/server/internal/memory/reviewstate"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	skilldocs "github.com/JieWaZi/NeuxBot/server/internal/skills"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/aiaccount"
	"github.com/JieWaZi/NeuxBot/server/services/skilllearning"
)

// ReviewWorker 是伙伴记忆与技能学习共用的后台所有者。
var ReviewWorker *reviewstate.Worker

func loadPartnerReview(ctx context.Context, configuration *Configuration, skills *skilllearning.Service) error {
	if MemoryService == nil {
		return status_error.MemoryServiceUnavailable
	}
	scratch := filepath.Join(configuration.DataDirectory, "partner-review-scratch")
	if err := agent.RecoverEphemeralScratch(scratch); err != nil {
		logx.Warnf("recover partner review workspace: error=%v", err)
	}
	store, err := reviewstate.Open(MemoryService.ReviewDB())
	if err != nil {
		return err
	}
	ReviewWorker = reviewstate.New(store, reviewstate.Ports{
		Chat: func(ctx context.Context, id string) (reviewstate.ChatAccess, error) {
			item, err := ChatService.LoadExecutionChat(ctx, id)
			if err != nil {
				return reviewstate.ChatAccess{}, err
			}
			if item.HiddenFromWakerHistory {
				return reviewstate.ChatAccess{}, status_error.ChatNotFound
			}
			return reviewstate.ChatAccess{WakerID: item.WakerID, ProjectID: item.ProjectID,
				TemporaryDirectory: temporaryReviewChat(item), Active: item.ActiveExecutionID != "", LatestSequence: item.LatestSequence}, nil
		},
		TurnStarts: ChatService.ReadReviewTurnStarts,
		Turns:      ChatService.ReadReviewTurns,
		WakerExists: func(ctx context.Context, id string) (bool, error) {
			_, err := WakerControllerManager.Get(ctx, id)
			if util.HasStatusCode(err, status_error.WakerNotFound) {
				return false, nil
			}
			return err == nil, err
		},
		Recover: ChatService.ListReviewChatIDs,
		Guard: func(wakerID, chatID string) (func(), error) {
			if wakerID != "" {
				wakerID = "waker:" + wakerID
			}
			return chatstorage.AcquireReferences(configuration.ManagedFilesDirectory, wakerID, chatID)
		},
		ListLearningReceiptIDs: SkillControllerManager.ListLearningReceiptIDs,
		DeleteLearningReceipt:  SkillControllerManager.DeleteLearningReceipt,
		RetryEphemeralCleanup:  AgentProviders.RetryEphemeralCleanup,
		Resolve: func(ctx context.Context, wakerID string, settings reviewstate.Settings) (reviewstate.Selection, error) {
			return reviewSelection(ctx, wakerID, settings, scratch)
		},
		Generate: func(ctx context.Context, selection reviewstate.Selection, waker, source, system, input string, authorize func(context.Context) error) (string, error) {
			return generateReviewText(ctx, selection, scratch, waker, source, system, input, authorize)
		},
		Memories: MemoryService.RelevantReviewMemories,
		Prepare: func(ctx context.Context, b reviewstate.MemoryBatch) (reviewstate.MemoryCommit, error) {
			prepared, err := MemoryService.PrepareReviewChanges(ctx, memory.ReviewMemoryBatch{
				PartnerID: b.Progress.WakerID, ChatID: b.Progress.ChatID, After: b.Progress.Cursor, Through: b.Progress.Through,
				EvidenceIDs: b.EvidenceIDs, Snapshot: b.Snapshot, Changes: b.Changes})
			if err != nil {
				return nil, err
			}
			return func(ctx context.Context, finish reviewstate.CommitReviewProgress) error {
				return MemoryService.CommitReviewChanges(ctx, prepared, finish)
			}, nil
		},
		Skills: func(ctx context.Context, id string) ([]reviewstate.SkillCandidate, error) {
			values, err := skills.ListCandidates(ctx, id)
			result := make([]reviewstate.SkillCandidate, 0, len(values))
			for _, v := range values {
				result = append(result, reviewstate.SkillCandidate(v))
			}
			return result, err
		},
		Target: func(ctx context.Context, id, skill, version string) (reviewstate.SkillTarget, error) {
			target, err := skills.ReadTarget(ctx, id, skill, version)
			ids := make([]string, 0, len(target.AffectedPartners))
			for _, p := range target.AffectedPartners {
				ids = append(ids, p.ID)
			}
			return reviewstate.SkillTarget{VersionID: target.VersionID, Name: target.Name, Markdown: target.Markdown, PartnerIDs: ids}, err
		},
		Published: func(ctx context.Context, v reviewstate.Suggestion) (reviewstate.PublishedSkill, bool, error) {
			receipt, found, err := skills.GetReceipt(ctx, v.WakerID, v.ID)
			return reviewstate.PublishedSkill{SkillID: receipt.SkillID, VersionID: receipt.VersionID, PublishedAt: receipt.CreatedAt}, found, err
		},
		Publish: func(ctx context.Context, v reviewstate.Suggestion, draft skilldocs.LearnedDraft, approved []string, guard func() (func(), error)) (reviewstate.PublishedSkill, error) {
			receipt, err := skills.Publish(ctx, skilllearning.Publication{SuggestionID: v.ID, WakerID: v.WakerID, Name: v.Name,
				Draft: draft, SkillID: v.SkillID, ExpectedVersionID: v.VersionID, ConfirmedPartnerIDs: approved, AcquirePublication: guard})
			return reviewstate.PublishedSkill{SkillID: receipt.SkillID, VersionID: receipt.VersionID, PublishedAt: receipt.CreatedAt}, err
		},
		Changed: func(event, waker, chat string) {
			resource := events.Resource{Kind: "waker", ID: waker}
			if waker == "" {
				resource.Kind = "global"
			}
			_ = events.Emit(EventHub, "resources", event, resource, struct {
				// ChatID 是需重新读取建议的来源。
				ChatID string `json:"chatId,omitempty"`
			}{ChatID: chat})
		},
	})
	ReviewWorker.Start(ctx)
	return nil
}

func temporaryReviewChat(item models.Chat) bool {
	return item.ProjectID == "" && item.ProjectName != ""
}

func reviewProvider(e models.RuntimeEnvironment) string {
	if e.BaseRuntimeID != "" {
		return e.BaseRuntimeID
	}
	if e.Detection.Runtime.BaseRuntimeID != "" {
		return e.Detection.Runtime.BaseRuntimeID
	}
	return e.RuntimeID
}

// ValidateReviewSelection 仅检查配置引用，模型调用负责鉴权结果。
func ValidateReviewSelection(ctx context.Context, settings reviewstate.Settings) error {
	_, err := reviewSelection(ctx, "", settings, "")
	return err
}

func reviewSelection(ctx context.Context, wakerID string, settings reviewstate.Settings, identityWorkspace string) (reviewstate.Selection, error) {
	selected := reviewstate.Selection{RuntimeID: settings.RuntimeID, AccountID: settings.AccountID, Model: settings.Model}
	if settings.Source != reviewstate.SourceGlobal {
		waker, err := WakerControllerManager.Get(ctx, wakerID)
		if err != nil {
			return selected, err
		}
		if config := waker.DefaultModel; config != nil {
			selected.RuntimeID, selected.AccountID, selected.Model = config.RuntimeID, config.AccountID, config.Model
		}
	}
	if selected.RuntimeID == "" {
		list, err := RuntimeEnvironmentControllerManager.List(ctx)
		if err != nil {
			return selected, err
		}
		selected.RuntimeID = defaultReviewRuntime(list)
	}
	if selected.RuntimeID == "" {
		return selected, status_error.RuntimeUnavailable
	}
	e, err := RuntimeEnvironmentControllerManager.Get(ctx, selected.RuntimeID)
	if err != nil {
		return selected, err
	}
	e.BaseRuntimeID = reviewProvider(e)
	if selected.AccountID == "" {
		selected.AccountID = e.DefaultAccountID
	}
	if selected.AccountID == "" {
		selected.AccountID = constants.AiAccountSystemID
	}
	if selected.AccountID != constants.AiAccountSystemID {
		account, err := AIAccountService.Get(ctx, selected.AccountID)
		if err != nil {
			return selected, err
		}
		if err = aiaccount.ValidateRuntimeAccount(account, e); err != nil {
			return selected, err
		}
		selected.Model, _ = aiaccount.RuntimeSelectionDefaults(account, e, selected.Model, "")
	}
	if selected.AccountID == constants.AiAccountSystemID {
		if selected.Model == "" {
			selected.Model = e.DefaultModel
		}
	}
	if !e.Enabled || strings.TrimSpace(e.Detection.ResolvedBinary) == "" {
		return selected, status_error.RuntimeUnavailable
	}
	if !agent.SupportsProvider(e.BaseRuntimeID) {
		return selected, status_error.RuntimePolicyUnsupported
	}
	selected.Connection = detachedConnection(models.LibraryExecutionDefaults{RuntimeID: selected.RuntimeID, AccountID: selected.AccountID, Model: selected.Model}, e)
	if identityWorkspace != "" && selected.AccountID == constants.AiAccountSystemID {
		// 内置账号的预检查使用复盘自己的目录，实际调用再从同一根目录下的临时工作区核对身份。
		request := selected.Connection.Account
		request.CWD = identityWorkspace
		identity, err := AIAccountService.ResolveAccount(ctx, request, selected.Connection.Launch.ConfigHome)
		if err != nil {
			return selected, err
		}
		selected.SystemIdentity = systemReviewIdentity(identity)
	}
	return selected, nil
}

func defaultReviewRuntime(list []models.RuntimeEnvironment) string {
	var first string
	for _, runtime := range list {
		if !runtime.Enabled || !agent.SupportsProvider(reviewProvider(runtime)) {
			continue
		}
		if runtime.IsDefault {
			return runtime.RuntimeID
		}
		if first == "" {
			first = runtime.RuntimeID
		}
	}
	return first
}

func systemReviewIdentity(account agent.AccountRequest) string {
	body, _ := json.Marshal([]string{account.Subject, account.ConfigFingerprint})
	return string(body)
}

func generateReviewText(ctx context.Context, selection reviewstate.Selection, scratch, waker, source, system, input string, authorize func(context.Context) error) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resolve := func(ctx context.Context, request agent.AccountRequest, home string) (agent.AccountRequest, error) {
		identity, err := AIAccountService.ResolveAccount(ctx, request, home)
		if err == nil && request.AccountID == constants.AiAccountSystemID && systemReviewIdentity(identity) != selection.SystemIdentity {
			return agent.AccountRequest{}, status_error.Conflict
		}
		return identity, err
	}
	result, err := AgentThreads.GenerateText(ctx, agent.TextRequest{Connection: selection.Connection, ResolveAccount: resolve,
		Messages:    []agent.TextMessage{{Role: "system", Content: system}, {Role: "user", Content: input}},
		UsageSource: source, UsageWakerID: waker, Ephemeral: true, BeforeSubmit: authorize, ScratchRoot: scratch})
	return result.Text, err
}
