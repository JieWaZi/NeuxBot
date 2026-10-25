package authorization

import (
	"context"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
)

// noticeDriver 以受控挑战驱动真实授权生命周期。
type noticeDriver struct {
	// run 发布挑战并由测试控制完成或取消。
	run func(context.Context, func(Device) error) error
}

func (*noticeDriver) Binary() string                                  { return "test-bridge" }
func (*noticeDriver) Environment() map[string]string                  { return map[string]string{} }
func (*noticeDriver) Configure(context.Context, string, string) error { return nil }
func (*noticeDriver) Logout(context.Context) error                    { return nil }
func (*noticeDriver) Status(context.Context, bool) (Account, error) {
	return Account{Configured: true, Available: true, Scopes: []string{}}, nil
}
func (d *noticeDriver) Login(ctx context.Context, _ []string, report func(Device) error) error {
	return d.run(ctx, report)
}

func awaitNoticeSession(t *testing.T, events <-chan Session) Session {
	t.Helper()
	select {
	case session := <-events:
		return session
	case <-time.After(2 * time.Second):
		t.Fatal("authorization did not publish expected challenge/state")
		return Session{}
	}
}

func TestNoticeCopiesAndClearsAcrossChallengesAndTerminalStates(t *testing.T) {
	for _, outcome := range []string{"ready", "failed", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			advance, finish := make(chan struct{}), make(chan struct{})
			firstReported := make(chan struct{})
			reminder := &Notice{Messages: map[string]string{"en": "Scan once more", "zh-CN": "还需再次扫码授权权限"}}
			driver := &noticeDriver{run: func(ctx context.Context, report func(Device) error) error {
				device := Device{ChallengeID: "first", Actions: []Action{{Type: "copy-code", Value: "ONE"}}, Notice: reminder, DeadlineAt: time.Now().Add(time.Minute).UnixMilli()}
				if err := report(device); err != nil {
					return err
				}
				reminder.Messages["en"] = "driver mutation"
				close(firstReported)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-advance:
				}
				device.ChallengeID, device.Notice = "second", nil
				if err := report(device); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-finish:
				}
				// 终态前再次提供提醒，以验证完成清理并非仅依赖上一挑战已经为空。
				device.ChallengeID, device.Notice = "third", &Notice{Messages: map[string]string{"en": "Final confirmation"}}
				if err := report(device); err != nil {
					return err
				}
				if outcome == "failed" {
					return status_error.PluginAuthorizationFailed
				}
				return nil
			}}
			events := make(chan Session, 8)
			authorizer := New(Config{Driver: driver, Changed: func(session Session) { events <- session }})
			authorizer.Open(context.Background())
			t.Cleanup(authorizer.Close)
			session, err := authorizer.Login(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			<-firstReported
			first := awaitNoticeSession(t, events)
			if session.Notice == nil || first.Notice == nil || session.Notice.Messages["en"] != "Scan once more" || first.Notice.Messages["en"] != "Scan once more" {
				t.Fatalf("notice was lost or shared with driver: %+v %+v", session.Notice, first.Notice)
			}
			session.Notice.Messages["en"] = "HTTP client mutation"
			first.Notice.Messages["en"] = "event observer mutation"
			snapshot := authorizer.Snapshot()
			if snapshot.Notice == nil || snapshot.Notice.Messages["en"] != "Scan once more" {
				t.Fatal("session notice aliases HTTP/event recipient map")
			}
			snapshot.Notice.Messages["en"] = "snapshot mutation"
			if authorizer.Snapshot().Notice.Messages["en"] != "Scan once more" {
				t.Fatal("snapshot notice aliases session map")
			}
			if outcome == "cancelled" {
				if err := authorizer.CancelAndWait(context.Background(), session.ID); err != nil {
					t.Fatal(err)
				}
				snapshot = authorizer.Snapshot()
				if snapshot.Notice != nil || len(snapshot.Actions) != 0 || snapshot.State != constants.PluginAuthorizationStateCancelled {
					t.Fatalf("cancel retained ephemeral challenge: %+v", snapshot)
				}
				return
			}
			close(advance)
			second := awaitNoticeSession(t, events)
			if second.ChallengeID != "second" || second.Notice != nil || authorizer.Snapshot().Notice != nil {
				t.Fatalf("second challenge inherited the previous notice: %+v", second)
			}
			close(finish)
			third := awaitNoticeSession(t, events)
			if third.Notice == nil || third.ChallengeID != "third" {
				t.Fatal("terminal cleanup fixture did not restore notice")
			}
			terminal := awaitNoticeSession(t, events)
			want := constants.PluginAuthorizationStateReady
			if outcome == "failed" {
				want = constants.PluginAuthorizationStateFailed
			}
			if terminal.State != want || terminal.Notice != nil || len(terminal.Actions) != 0 || authorizer.Snapshot().Notice != nil {
				t.Fatalf("terminal state retained challenge notice: %+v", terminal)
			}
		})
	}
}
