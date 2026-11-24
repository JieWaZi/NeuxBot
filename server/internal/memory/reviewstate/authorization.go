package reviewstate

import (
	"context"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

type modelFlight struct {
	// waker 是设置归属。
	waker string
	// chat 是证据来源。
	chat string
	// cancel 取消租约等待及尚未结束的调用。
	cancel context.CancelFunc
}

// ObserveDeletion 撤销使用来源聊天的进行中调用，并登记后台清理。
func (w *Worker) ObserveDeletion(chat string) {
	w.cancelFlight("", chat)
	w.ObserveTerminal(chat)
}

// ObservePartnerDeletion 在伙伴的权威删除提交后取消模型调用并安排清理。
func (w *Worker) ObservePartnerDeletion(waker string) {
	if waker != "" {
		w.cancelFlight(waker, "")
		w.departed.Store(waker, struct{}{})
		w.Wake()
	}
}

func (w *Worker) forgetMissingChat(ctx context.Context, waker, chat string) error {
	release, err := w.ports.Guard(waker, chat)
	if err != nil {
		return err
	}
	defer release()
	if _, err = w.ports.Chat(ctx, chat); util.HasStatusCode(err, status_error.ChatNotFound) {
		return w.Store.ForgetChat(ctx, chat)
	}
	return err
}

func (w *Worker) cancelFlight(waker, chat string) {
	w.flightMu.Lock()
	defer w.flightMu.Unlock()
	if f := w.flight; f != nil && (waker == "" || waker == f.waker) && (chat == "" || chat == f.chat) {
		f.cancel()
	}
}

func (w *Worker) generate(ctx context.Context, selection Selection, waker, chat string, origin Admission, use, system, input string) (string, error) {
	var text string
	err := w.authorizedCall(ctx, waker, chat, origin, func(ctx context.Context, gate func(context.Context) error) error {
		var err error
		text, err = w.ports.Generate(ctx, selection, waker, use, system, input, gate)
		return err
	})
	return text, err
}

func (w *Worker) authorizedCall(ctx context.Context, waker, chat string, origin Admission, call func(context.Context, func(context.Context) error) error) error {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	w.mu.Lock()
	if err := w.checkAuthorization(run, waker, chat, origin); err != nil {
		w.mu.Unlock()
		return err
	}
	flight := &modelFlight{waker: waker, chat: chat, cancel: cancel}
	w.flightMu.Lock()
	w.flight = flight
	w.flightMu.Unlock()
	w.mu.Unlock()
	w.changed(eventReview, waker, chat)
	defer func() {
		w.flightMu.Lock()
		if w.flight == flight {
			w.flight = nil
		}
		w.flightMu.Unlock()
	}()
	return call(run, func(ctx context.Context) error {
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.checkAuthorization(ctx, waker, chat, origin)
	})
}

func (w *Worker) checkAuthorization(ctx context.Context, waker, chat string, origin Admission) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, _, current, err := w.Effective(ctx, waker)
	if err != nil {
		return err
	}
	if current.Configuration != origin.Configuration || (origin.Memory != 0 && origin.Memory != current.Memory) ||
		(origin.Skill != 0 && origin.Skill != current.Skill) {
		return status_error.Conflict
	}
	release, err := w.ports.Guard(waker, chat)
	if err != nil {
		return err
	}
	defer release()
	access, err := w.ports.Chat(ctx, chat)
	if err != nil {
		return err
	}
	if access.WakerID != waker {
		return status_error.ChatNotFound
	}
	return ctx.Err()
}
