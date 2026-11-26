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

func (w *Worker) generate(ctx context.Context, selection Selection, waker, chat, use, system, input string) (string, error) {
	var text string
	err := w.withSource(ctx, waker, chat, func(ctx context.Context) error {
		var err error
		text, err = w.ports.Generate(ctx, selection, waker, use, system, input, func(ctx context.Context) error {
			return w.checkSource(ctx, waker, chat)
		})
		return err
	})
	return text, err
}

// withSource 让模型及嵌入调用响应实际的来源删除。
func (w *Worker) withSource(ctx context.Context, waker, chat string, call func(context.Context) error) error {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	flight := &modelFlight{waker: waker, chat: chat, cancel: cancel}
	w.flightMu.Lock()
	w.flight = flight
	w.flightMu.Unlock()
	defer func() {
		w.flightMu.Lock()
		if w.flight == flight {
			w.flight = nil
		}
		w.flightMu.Unlock()
	}()
	if err := w.checkSource(run, waker, chat); err != nil {
		return err
	}
	w.changed(eventReview, waker, chat)
	return call(run)
}

func (w *Worker) checkSource(ctx context.Context, waker, chat string) error {
	release, err := w.guardSource(ctx, waker, chat)
	if err == nil {
		release()
	}
	return err
}

func (w *Worker) guardSource(ctx context.Context, waker, chat string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release, err := w.ports.Guard(waker, chat)
	if err != nil {
		return nil, err
	}
	access, err := w.ports.Chat(ctx, chat)
	if err == nil && access.WakerID != waker {
		err = status_error.ChatNotFound
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}
