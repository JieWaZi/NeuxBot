package mainconversation

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/chatstorage"
	"github.com/JieWaZi/NeuxBot/server/internal/workinput"
	"github.com/JieWaZi/NeuxBot/server/internal/workspace"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	gsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMainInputQueueOrdersEachChatAndRestoresOriginalSubmissions(t *testing.T) {
	ctx := t.Context()
	store, first, other := mainInputTestStore(t)
	gate := make(chan struct{})
	t.Cleanup(sync.OnceFunc(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}))
	fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 10)}
	fake.before = func(ctx context.Context, input models.MainConversationInput) error {
		if input.Input.SubmissionID == "a" {
			select {
			case <-gate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	q := NewInputQueue(store, InputQueueOptions{ReadSubmission: fake.read, Dispatch: fake.dispatch})
	if err := q.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	a := enqueueMainInput(t, q, mainInputTestRow(first, "a"))
	assertMainInputCall(t, fake.calls, "a")
	b := enqueueMainInput(t, q, mainInputTestRow(first, "b"))
	if a.Sequence >= b.Sequence {
		t.Fatalf("queue order = %d, %d", a.Sequence, b.Sequence)
	}
	otherItem := enqueueMainInput(t, q, mainInputTestRow(other, "other"))
	assertMainInputCall(t, fake.calls, "other")
	waitMainInputStatus(t, q, otherItem, constants.MainConversationInputAccepted)
	replay := enqueueMainInput(t, q, mainInputTestRow(first, "b"))
	if replay.Sequence != b.Sequence {
		t.Fatalf("replay allocated a second position: %+v, original %+v", replay, b)
	}
	close(gate)
	assertMainInputCall(t, fake.calls, "b")
	waitMainInputStatus(t, q, b, constants.MainConversationInputAccepted)
}

func TestMainInputQueueRecoveryReadsAuthorityBeforeDispatch(t *testing.T) {
	ctx := t.Context()
	store, work, another := mainInputTestStore(t)
	fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 10)}
	before := NewInputQueue(store, InputQueueOptions{ReadSubmission: fake.read, Dispatch: fake.dispatch})
	first := enqueueMainInput(t, before, mainInputTestRow(work, "already"))
	second := enqueueMainInput(t, before, mainInputTestRow(work, "after-confirmed"))
	third := enqueueMainInput(t, before, mainInputTestRow(another, "uncertain"))
	enqueueMainInput(t, before, mainInputTestRow(another, "held"))
	for _, receipt := range []models.MainConversationInputReceipt{first, third} {
		claimed, err := store.Claim(ctx, receipt.Sequence)
		if err != nil || !claimed {
			t.Fatalf("claim %s = %v, %v", receipt.SubmissionID, claimed, err)
		}
		marked, err := store.MarkCommitting(ctx, receipt.Sequence)
		if err != nil || !marked {
			t.Fatalf("mark %s = %v, %v", receipt.SubmissionID, marked, err)
		}
	}
	fake.record(mainInputTestRow(work, "already"))
	restarted := NewInputQueue(store, InputQueueOptions{ReadSubmission: fake.read, Dispatch: fake.dispatch})
	if err := restarted.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	restarted.Wake(work.WorkChatID)
	restarted.Wake(another.WorkChatID)
	waitMainInputStatus(t, restarted, first, constants.MainConversationInputAccepted)
	assertMainInputCall(t, fake.calls, second.SubmissionID)
	waitMainInputStatus(t, restarted, second, constants.MainConversationInputAccepted)
	waitMainInputStatus(t, restarted, third, constants.MainConversationInputUncertain)
	// 迟到的权威回执解开原工作来源，不造成第二次提交。
	fake.record(mainInputTestRow(another, "uncertain"))
	restarted.Wake(another.WorkChatID)
	waitMainInputStatus(t, restarted, third, constants.MainConversationInputAccepted)
	assertMainInputCall(t, fake.calls, "held")
}

func TestMainInputQueueRestartsRoutingFromPublishedAuthority(t *testing.T) {
	for _, sample := range []struct {
		// name 标识中断时的路由进度。
		name string
		// marked 表示旧进程已经到达原生输入提交门。
		marked bool
		// published 表示直接答复已写入权威公开时间线。
		published bool
	}{
		{name: "claimed-before-native-gate"},
		{name: "marked-without-publication", marked: true},
		{name: "published-before-stop", marked: true, published: true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			ctx := t.Context()
			store, work, _ := mainInputTestStore(t)
			fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 1)}
			type routeCall struct {
				// submission 标识派发给 Router 的原输入。
				submission string
				// recovering 表示因旧进程可能已提交原生输入而需要新开会话。
				recovering bool
			}
			calls := make(chan routeCall, 2)
			options := InputQueueOptions{
				ReadSubmission: fake.read,
				Dispatch:       fake.dispatch,
				ReadRoutedPublication: func(_ context.Context, input models.MainConversationInput) (bool, error) {
					return sample.published && input.Input.SubmissionID == "first", nil
				},
				Route: func(ctx context.Context, input models.MainConversationInput, recovering bool, mark func(context.Context) error, promote func(context.Context, models.MainConversationInput) error) error {
					calls <- routeCall{input.Input.SubmissionID, recovering}
					if err := mark(ctx); err != nil {
						return err
					}
					if input.Input.SubmissionID == "first" {
						input.WorkChatID = work.WorkChatID
						input.RoutedSource = &models.MainConversationRoutedSource{ScopeKey: work.ScopeKey}
						return promote(ctx, input)
					}
					return nil
				},
			}
			before := NewInputQueue(store, options)
			var accepted []models.MainConversationInputReceipt
			for _, submission := range []string{"first", "next"} {
				row := mainInputTestRow(work, submission)
				row.WorkChatID, row.RouterWakerID = "", work.WakerID
				accepted = append(accepted, enqueueMainInput(t, before, row))
			}
			claimed, err := store.Claim(ctx, accepted[0].Sequence)
			if err != nil || !claimed {
				t.Fatalf("original claim = %v, %v", claimed, err)
			}
			if sample.marked {
				marked, err := store.MarkCommitting(ctx, accepted[0].Sequence)
				if err != nil || !marked {
					t.Fatalf("original native gate = %v, %v", marked, err)
				}
			}

			restarted := NewInputQueue(store, options)
			if err := restarted.Open(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(restarted.Close)
			want := []routeCall{{"next", false}}
			if !sample.published {
				want = append([]routeCall{{"first", sample.marked}}, want...)
			}
			for _, call := range want {
				select {
				case got := <-calls:
					if got != call {
						t.Fatalf("next routing turn = %+v, want %+v", got, call)
					}
				case <-time.After(3 * time.Second):
					t.Fatalf("missing routing turn %+v", call)
				}
			}
			if !sample.published {
				assertMainInputCall(t, fake.calls, "first")
			}
			for _, receipt := range accepted {
				waitMainInputStatus(t, restarted, receipt, constants.MainConversationInputAccepted)
			}
		})
	}
}

func TestMainInputQueueContinuesAfterRouterDeadline(t *testing.T) {
	store, work, _ := mainInputTestStore(t)
	fake := &queueTestDispatch{receipts: make(map[string]models.Execution)}
	q := NewInputQueue(store, InputQueueOptions{
		ReadSubmission: fake.read,
		Dispatch:       fake.dispatch,
		ReadRoutedPublication: func(context.Context, models.MainConversationInput) (bool, error) {
			return false, nil
		},
		Route: func(ctx context.Context, row models.MainConversationInput, _ bool, mark func(context.Context) error, _ func(context.Context, models.MainConversationInput) error) error {
			if err := mark(ctx); err != nil {
				return err
			}
			if row.Input.SubmissionID == "deadline" {
				return context.DeadlineExceeded
			}
			return nil
		},
	})
	first := mainInputTestRow(work, "deadline")
	first.WorkChatID, first.RouterWakerID = "", work.WakerID
	second := mainInputTestRow(work, "after-deadline")
	second.WorkChatID, second.RouterWakerID = "", work.WakerID
	failing := enqueueMainInput(t, q, first)
	next := enqueueMainInput(t, q, second)
	if err := q.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	waitMainInputStatus(t, q, failing, constants.MainConversationInputFailed)
	waitMainInputStatus(t, q, next, constants.MainConversationInputAccepted)
}

func TestMainInputQueueWakesDefaultSourceAfterDirectRouterReply(t *testing.T) {
	ctx := t.Context()
	db := mainInputTestDB(t)
	store := controllers.NewMainConversationInputController(db)
	work := models.MainConversationWork{ConversationID: "main", WorkChatID: "work", WakerID: "waker", ScopeKey: models.MainConversationDefaultScope}
	routing, release := make(chan struct{}), make(chan struct{})
	fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 1)}
	q := NewInputQueue(store, InputQueueOptions{
		ReadSubmission: fake.read,
		Dispatch:       fake.dispatch,
		ReadRoutedPublication: func(context.Context, models.MainConversationInput) (bool, error) {
			return false, nil
		},
		Route: func(ctx context.Context, _ models.MainConversationInput, _ bool, mark func(context.Context) error, _ func(context.Context, models.MainConversationInput) error) error {
			close(routing)
			select {
			case <-release:
				return mark(ctx)
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	if err := q.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	first := mainInputTestRow(work, "main")
	first.WorkChatID, first.RouterWakerID = "", work.WakerID
	enqueueMainInput(t, q, first)
	select {
	case <-routing:
	case <-time.After(3 * time.Second):
		t.Fatal("router did not start")
	}
	if err := db.Create(&work).Error; err != nil {
		t.Fatal(err)
	}
	late := enqueueMainInput(t, q, mainInputTestRow(work, "source"))
	lane := mainInputLane{chatID: work.WorkChatID}
	deadline := time.Now().Add(3 * time.Second)
	for {
		q.mu.Lock()
		_, running := q.lanes[lane]
		q.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("source lane did not return while routing")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case input := <-fake.calls:
		t.Fatalf("source dispatched ahead of routing: %+v", input)
	default:
	}
	close(release)
	assertMainInputCall(t, fake.calls, "source")
	waitMainInputStatus(t, q, late, constants.MainConversationInputAccepted)
}

func TestMainInputQueueCommitResponseFailureDoesNotReplay(t *testing.T) {
	ctx := t.Context()
	store, work, _ := mainInputTestStore(t)
	fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 10)}
	q := NewInputQueue(store, InputQueueOptions{
		ReadSubmission: fake.read,
		Dispatch: func(ctx context.Context, input models.MainConversationInput, mark func(context.Context) error) (models.Execution, error) {
			result, err := fake.dispatch(ctx, input, mark)
			if err != nil {
				return result, err
			}
			return models.Execution{}, status_error.DatabaseError
		},
	})
	if err := q.Open(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	accepted := enqueueMainInput(t, q, mainInputTestRow(work, "lost-response"))
	assertMainInputCall(t, fake.calls, "lost-response")
	waitMainInputStatus(t, q, accepted, constants.MainConversationInputAccepted)
	q.Wake(work.WorkChatID)
	replay := enqueueMainInput(t, q, mainInputTestRow(work, "lost-response"))
	if replay.Sequence != accepted.Sequence {
		t.Fatalf("replayed identity = %+v", replay)
	}
	select {
	case row := <-fake.calls:
		t.Fatalf("already admitted submission dispatched again: %s", row.Input.SubmissionID)
	default:
	}
}

func TestMainInputQueueRecoversAProvenEmptyBootClaim(t *testing.T) {
	for _, sample := range []struct {
		// name 标识相对于原生提交门发生的中断阶段。
		name string
		// marked 表示上次 Core 已登记原生提交门。
		marked bool
		// temporarilyUnreadable 表示重启后的首个提交收据读取失败。
		temporarilyUnreadable bool
	}{
		{name: "claimed-before-gate"},
		{name: "marked-without-commit", marked: true},
		{name: "marked-receipt-temporarily-unreadable", marked: true, temporarilyUnreadable: true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			store, work, _ := mainInputTestStore(t)
			fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 10)}
			ctx := t.Context()
			before := NewInputQueue(store, InputQueueOptions{ReadSubmission: fake.read, Dispatch: fake.dispatch})
			pending := enqueueMainInput(t, before, mainInputTestRow(work, sample.name))
			if ok, err := store.Claim(ctx, pending.Sequence); err != nil || !ok {
				t.Fatalf("claim = %v, %v", ok, err)
			}
			expectedProofs := 0
			if sample.marked {
				if ok, err := store.MarkCommitting(ctx, pending.Sequence); err != nil || !ok {
					t.Fatalf("mark = %v, %v", ok, err)
				}
				expectedProofs = 1
			}
			confirmed := 0
			unreadable := sample.temporarilyUnreadable
			q := NewInputQueue(store, InputQueueOptions{
				ReadSubmission: func(ctx context.Context, chat, submission string) (*models.Execution, error) {
					if unreadable {
						unreadable = false
						return nil, status_error.ChatLogError
					}
					return fake.read(ctx, chat, submission)
				},
				Dispatch: fake.dispatch,
				ConfirmBootAbsence: func(_ context.Context, item models.MainConversationInput) (bool, error) {
					confirmed++
					return item.ExecutionID == pending.ExecutionID, nil
				},
			})
			if err := q.Open(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(q.Close)
			q.Wake(work.WorkChatID)
			assertMainInputCall(t, fake.calls, pending.SubmissionID)
			waitMainInputStatus(t, q, pending, constants.MainConversationInputAccepted)
			if confirmed != expectedProofs {
				t.Fatalf("checked boot proof %d times, want %d", confirmed, expectedProofs)
			}
		})
	}
}

func TestMainInputQueueRetriesTemporaryRestorePastInitialBackoff(t *testing.T) {
	store, work, _ := mainInputTestStore(t)
	fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 10)}
	const failedAttempts = 4
	attempts := 0
	fake.before = func(_ context.Context, input models.MainConversationInput) error {
		if input.Input.SubmissionID == "restore" {
			attempts++
			if attempts <= failedAttempts {
				return status_error.RuntimeSessionRestoreFailed
			}
		}
		return nil
	}
	submitted := make(chan models.MainConversationInput, 10)
	q := NewInputQueue(store, InputQueueOptions{
		ReadSubmission: fake.read,
		Dispatch: func(ctx context.Context, input models.MainConversationInput, mark func(context.Context) error) (models.Execution, error) {
			return fake.dispatch(ctx, input, func(ctx context.Context) error {
				if err := mark(ctx); err != nil {
					return err
				}
				submitted <- input
				return nil
			})
		},
	})
	// Both sends already have their durable positions. Neither can issue an extra wake after startup.
	first := enqueueMainInput(t, q, mainInputTestRow(work, "restore"))
	second := enqueueMainInput(t, q, mainInputTestRow(work, "next"))
	if err := q.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	for i := 0; i <= failedAttempts; i++ {
		assertMainInputCall(t, fake.calls, first.SubmissionID)
	}
	assertMainInputCall(t, fake.calls, second.SubmissionID)
	waitMainInputStatus(t, q, first, constants.MainConversationInputAccepted)
	waitMainInputStatus(t, q, second, constants.MainConversationInputAccepted)
	q.Close()
	close(submitted)
	for _, original := range []models.MainConversationInputReceipt{first, second} {
		input, ok := <-submitted
		if !ok || input.ExecutionID != original.ExecutionID || input.Input.SubmissionID != original.SubmissionID {
			t.Fatalf("submitted %+v, want original receipt %+v", input, original)
		}
	}
	if extra, ok := <-submitted; ok {
		t.Fatalf("submitted an extra native input: %s", extra.Input.SubmissionID)
	}
}

func TestMainInputQueueRetriesTemporaryReceiptReadsWithoutAnotherWake(t *testing.T) {
	for _, name := range []string{"before-native-admission", "after-native-admission"} {
		t.Run(name, func(t *testing.T) {
			store, work, _ := mainInputTestStore(t)
			ctx := t.Context()
			fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 10)}
			const interruptedReads = 4
			failures := interruptedReads
			afterAdmission := name == "after-native-admission"
			q := NewInputQueue(store, InputQueueOptions{
				ReadSubmission: func(ctx context.Context, chatID, submissionID string) (*models.Execution, error) {
					receipt, err := fake.read(ctx, chatID, submissionID)
					if submissionID == "first" && (receipt != nil) == afterAdmission && failures > 0 {
						failures--
						return nil, status_error.DatabaseBusy
					}
					return receipt, err
				},
				Dispatch: fake.dispatch,
			})
			first := enqueueMainInput(t, q, mainInputTestRow(work, "first"))
			second := enqueueMainInput(t, q, mainInputTestRow(work, "second"))
			if err := q.Open(ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(q.Close)
			assertMainInputCall(t, fake.calls, first.SubmissionID)
			assertMainInputCall(t, fake.calls, second.SubmissionID)
			waitMainInputStatus(t, q, first, constants.MainConversationInputAccepted)
			waitMainInputStatus(t, q, second, constants.MainConversationInputAccepted)
			q.Close()
			if failures != 0 {
				t.Fatalf("still have %d injected failures after dispatch", failures)
			}
			select {
			case row := <-fake.calls:
				t.Fatalf("reconciliation dispatched %q more than once", row.Input.SubmissionID)
			default:
			}
		})
	}
}

func TestMainInputQueueStagesAttachmentForRestartAndTransfersOwnership(t *testing.T) {
	store, work, _ := mainInputTestStore(t)
	layout, err := chatstorage.NewLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chatRoot, err := layout.ChatRoot(work.WorkChatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(chatRoot, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(t.TempDir(), "original.txt")
	if err := os.WriteFile(original, []byte("persisted attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	pendingInput := mainInputTestRow(work, "attachment")
	pendingInput.Input.Attachments = []workinput.Attachment{{SourcePath: original}}
	pendingInput.Options.AccountID = "private-account"
	pendingInput.Input.ContextInstructions = []workinput.Instruction{{Name: "frozen", Content: "private instruction"}}
	before := NewInputQueue(store, InputQueueOptions{Layout: layout})
	pending := enqueueMainInput(t, before, pendingInput)
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	fake := &queueTestDispatch{receipts: make(map[string]models.Execution), calls: make(chan models.MainConversationInput, 10)}
	acceptedCopy := make(chan string, 1)
	fake.before = func(_ context.Context, input models.MainConversationInput) error {
		prepared, err := workspace.PrepareChatAttachments(chatRoot, input.WorkChatID, input.ExecutionID,
			[]workspace.AttachmentInput{{SourcePath: input.Input.Attachments[0].SourcePath}})
		if err == nil {
			acceptedCopy <- prepared.RuntimePaths[0]
		}
		return err
	}
	q := NewInputQueue(store, InputQueueOptions{Layout: layout, ReadSubmission: fake.read, Dispatch: fake.dispatch})
	if err := q.Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	var staged string
	select {
	case row := <-fake.calls:
		staged = row.Input.Attachments[0].SourcePath
	case <-time.After(3 * time.Second):
		t.Fatal("attachment was not recovered")
	}
	waitMainInputStatus(t, q, pending, constants.MainConversationInputAccepted)
	q.Close()
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staging still exists: %v", err)
	}
	data, err := os.ReadFile(<-acceptedCopy)
	if err != nil || string(data) != "persisted attachment" {
		t.Fatalf("accepted original = %q, %v", data, err)
	}
	saved, found, err := store.Get(t.Context(), pending.ConversationID, pending.PublicMessageID)
	if err != nil || !found || saved.Input.SubmissionID != pending.SubmissionID || saved.Options.AccountID != "" || saved.Input.Content != "" ||
		len(saved.Input.Attachments) != 0 || len(saved.Input.ContextInstructions) != 0 || saved.PublicContent != "" {
		t.Fatalf("handoff persisted private payload after settlement: %+v, found %v, error %v", saved, found, err)
	}
}

func mainInputTestStore(t *testing.T) (*controllers.MainConversationInputController, models.MainConversationWork, models.MainConversationWork) {
	t.Helper()
	db := mainInputTestDB(t)
	work := models.MainConversationWork{ConversationID: "main", WorkChatID: "work", WakerID: "waker", ScopeKey: models.MainConversationDefaultScope}
	other := models.MainConversationWork{ConversationID: "main", WorkChatID: "other-work", WakerID: "waker", ScopeKey: "other-selected"}
	if err := db.Create([]models.MainConversationWork{work, other}).Error; err != nil {
		t.Fatal(err)
	}
	return controllers.NewMainConversationInputController(db), work, other
}

func mainInputTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(gsqlite.Open(filepath.Join(t.TempDir(), "queue.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&models.MainConversation{}, &models.MainConversationInput{}, &models.MainConversationWork{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.MainConversation{ID: "main", SubjectType: "waker", SubjectID: "waker"}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func mainInputTestRow(work models.MainConversationWork, submission string) models.MainConversationInput {
	id := UserMessageID(work.ConversationID, submission)
	return models.MainConversationInput{
		ConversationID: work.ConversationID, WorkChatID: work.WorkChatID, PublicMessageID: id, ExecutionID: id,
		PublicContent: submission, RequestHash: "digest-" + submission,
		Input: workinput.Input{SubmissionID: submission, Actor: "user", Content: submission},
	}
}

func enqueueMainInput(t *testing.T, q *InputQueue, row models.MainConversationInput) models.MainConversationInputReceipt {
	t.Helper()
	receipt, err := q.Enqueue(t.Context(), row, nil)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func assertMainInputCall(t *testing.T, calls <-chan models.MainConversationInput, want string) {
	t.Helper()
	select {
	case row := <-calls:
		if row.Input.SubmissionID != want {
			t.Fatalf("dispatched %q, want %q", row.Input.SubmissionID, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
}

func waitMainInputStatus(t *testing.T, q *InputQueue, original models.MainConversationInputReceipt, status constants.MainConversationInputStatus) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := q.Receipt(t.Context(), original.ConversationID, original.SubmissionID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == status {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("submission %q did not reach %s", original.SubmissionID, status)
}

type queueTestDispatch struct {
	// mu protects the authority receipts from concurrent reads and writes.
	mu sync.Mutex
	// receipts holds facts already committed to the fake Chat journal.
	receipts map[string]models.Execution
	// calls observes each invocation of the normal delivery entry point.
	calls chan models.MainConversationInput
	// before can hold delivery before its authority commit starts.
	before func(context.Context, models.MainConversationInput) error
}

func (f *queueTestDispatch) read(_ context.Context, chat, submission string) (*models.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	receipt, exists := f.receipts[chat+"/"+submission]
	if !exists {
		return nil, nil
	}
	return &receipt, nil
}

func (f *queueTestDispatch) record(input models.MainConversationInput) models.Execution {
	f.mu.Lock()
	defer f.mu.Unlock()
	receipt := models.Execution{ID: input.ExecutionID, ChatID: input.WorkChatID, SubmissionID: input.Input.SubmissionID}
	f.receipts[input.WorkChatID+"/"+input.Input.SubmissionID] = receipt
	return receipt
}

func (f *queueTestDispatch) dispatch(ctx context.Context, input models.MainConversationInput, mark func(context.Context) error) (models.Execution, error) {
	f.calls <- input
	if f.before != nil {
		if err := f.before(ctx, input); err != nil {
			return models.Execution{}, err
		}
	}
	if err := mark(ctx); err != nil {
		return models.Execution{}, err
	}
	return f.record(input), nil
}
