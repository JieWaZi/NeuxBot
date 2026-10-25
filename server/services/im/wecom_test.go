package im

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	protocol "github.com/JieWaZi/NeuxBot/server/internal/im"
	"github.com/JieWaZi/NeuxBot/server/internal/notification/events"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/JieWaZi/NeuxBot/server/services/execution"
)

func TestWecomQRConfirmationProtectsIdentityAndPrivateCredentials(t *testing.T) {
	for _, existing := range []string{"", "same-bot", "different-bot"} {
		t.Run("existing_"+existing, func(t *testing.T) {
			s, _, _ := newIMFixture(t)
			ctx := context.Background()
			saved := models.IMConnection{ID: "wecom", Platform: constants.IMPlatformWecom, Name: "企业微信", AppID: existing}
			if existing != "" {
				saved.Credential = "old-secret"
			}
			if err := s.config.Store.SaveConnection(ctx, saved, 0); err != nil {
				t.Fatal(err)
			}
			s.config.Platforms[constants.IMPlatformWecom] = &protocol.Wecom{}
			original := http.DefaultTransport
			http.DefaultTransport = registrationTransport(func(request *http.Request) (*http.Response, error) {
				body := `{"data":{"scode":"private-poll-code","auth_url":"https://work.weixin.qq.com/qr"}}`
				if strings.HasSuffix(request.URL.Path, "query_result") {
					body = `{"data":{"status":"success","bot_info":{"botid":"same-bot","secret":"new-secret"}}}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			s.ctx, s.cancel = context.WithCancel(ctx)
			t.Cleanup(func() { s.Close(); http.DefaultTransport = original })
			subscription := s.config.Events.Subscribe("im", events.Resource{Kind: "global"})
			defer subscription.Close()
			beganAt := s.config.Now()
			login, err := s.BeginLogin(ctx, "wecom")
			if err != nil {
				t.Fatal(err)
			}
			if login.ExpiresAt < beganAt.Add(5*time.Minute).UnixMilli() || login.ExpiresAt > beganAt.Add(5*time.Minute+time.Second).UnixMilli() {
				t.Fatalf("QR expiry does not match official lifetime: %d", login.ExpiresAt)
			}
			deadline := time.After(2 * time.Second)
			for {
				select {
				case event := <-subscription.Events:
					if event.Type != "im.login.updated" {
						continue
					}
					if strings.Contains(string(event.Payload), "new-secret") || strings.Contains(string(event.Payload), "old-secret") || strings.Contains(string(event.Payload), "private-poll-code") {
						t.Fatal("login event exposed credentials or polling code")
					}
					var payload struct {
						// Login 是公开进度事件，不包含认证凭据。
						Login LoginProgress `json:"login"`
					}
					if err := json.Unmarshal(event.Payload, &payload); err != nil {
						t.Fatal(err)
					}
					if payload.Login.Status != "confirmed" && payload.Login.Status != "failed" {
						continue
					}
					current, err := s.config.Store.Connection(ctx, "wecom")
					if err != nil {
						t.Fatal(err)
					}
					if existing == "different-bot" {
						if payload.Login.Status != "failed" || payload.Login.ErrorCode != status_error.IMConfigurationConflict.Key() || current.AppID != existing || current.Credential != "old-secret" {
							t.Fatalf("rescanning transferred existing authorization: %+v", payload.Login)
						}
					} else {
						if payload.Login.Status != "confirmed" || current.AppID != "same-bot" || current.Credential != "new-secret" || !current.Enabled {
							t.Fatalf("official QR did not persist usable connection: %+v %v", payload.Login, current.Enabled)
						}
						public, err := json.Marshal(current)
						if err != nil {
							t.Fatal(err)
						}
						if strings.Contains(string(public), "new-secret") || strings.Contains(string(public), current.CredentialCiphertext) {
							t.Fatal("connection JSON exposed encrypted or plaintext credential")
						}
					}
					return
				case <-deadline:
					t.Fatal("QR did not reach terminal progress")
				}
			}
		})
	}
}

func TestWecomQRCancellationStopsOfficialPollAndKeepsCancelledState(t *testing.T) {
	s, _, _ := newIMFixture(t)
	ctx := context.Background()
	if err := s.config.Store.SaveConnection(ctx, models.IMConnection{ID: "wecom", Platform: constants.IMPlatformWecom, Name: "企业微信"}, 0); err != nil {
		t.Fatal(err)
	}
	s.config.Platforms[constants.IMPlatformWecom] = &protocol.Wecom{}
	started := make(chan struct{})
	exited := make(chan struct{})
	original := http.DefaultTransport
	http.DefaultTransport = registrationTransport(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "generate") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"scode":"private","auth_url":"https://work.weixin.qq.com/qr"}}`))}, nil
		}
		close(started)
		<-request.Context().Done()
		close(exited)
		return nil, request.Context().Err()
	})
	s.ctx, s.cancel = context.WithCancel(ctx)
	t.Cleanup(func() { s.Close(); http.DefaultTransport = original })
	login, err := s.BeginLogin(ctx, "wecom")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("poll not started")
	}
	if err := s.CancelLogin(login.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("poll ignored cancellation")
	}
	s.workers.Wait()
	progress, err := s.Login(login.ID)
	if err != nil || progress.Status != "cancelled" {
		t.Fatalf("late platform failure overwrote cancellation: %+v %v", progress, err)
	}
	current, err := s.config.Store.Connection(ctx, "wecom")
	if err != nil || current.Credential != "" || current.Enabled {
		t.Fatal("cancelled QR saved credentials")
	}
}

// wecomStartRecorder 仅观察维护循环是否启动传输，不访问外部平台。
type wecomStartRecorder struct {
	// recordingPlatform 复用本次用例未改变的收发端口。
	recordingPlatform
	// started 通知真实 Run 已经启动。
	started chan struct{}
}

func (p *wecomStartRecorder) Run(ctx context.Context, _ models.IMConnection, _ protocol.BatchCommit, _ protocol.StateChanged) error {
	close(p.started)
	<-ctx.Done()
	return nil
}

func TestWecomConnectionReplacementWaitsForExplicitSaveBeforeReconnecting(t *testing.T) {
	s, _, _ := newIMFixture(t)
	ctx := context.Background()
	configuration, err := s.SaveConfiguration(ctx, "", ConnectionConfiguration{Name: "企业微信", Platform: constants.IMPlatformWecom, AppID: "bot", AppSecret: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.config.Store.ConnectionState(ctx, configuration.ID, constants.IMConnectionStateFailed, status_error.IMConnectionReplaced.Key()); err != nil {
		t.Fatal(err)
	}
	platform := &wecomStartRecorder{started: make(chan struct{})}
	s.config.Platforms[constants.IMPlatformWecom] = platform
	s.ctx, s.cancel = context.WithCancel(ctx)
	t.Cleanup(s.Close)
	s.maintainConnections()
	select {
	case <-platform.started:
		t.Fatal("replacement triggered automatic connection tug-of-war")
	default:
	}
	if _, exists := s.connections[configuration.ID]; exists {
		t.Fatal("replaced bot registered a worker")
	}
	if _, err := s.SaveConfiguration(ctx, configuration.ID, ConnectionConfiguration{Name: configuration.Name, Platform: constants.IMPlatformWecom, Enabled: true, AppID: "different-bot", AppSecret: "different-secret", Revision: configuration.Revision}); !errors.Is(err, status_error.IMConfigurationConflict) {
		t.Fatalf("manual edit transferred chat authorization: %v", err)
	}
	if _, err := s.SaveConfiguration(ctx, configuration.ID, ConnectionConfiguration{Name: configuration.Name, Platform: constants.IMPlatformWecom, Enabled: true, AppID: "bot", Revision: configuration.Revision}); err != nil {
		t.Fatal(err)
	}
	s.maintainConnections()
	select {
	case <-platform.started:
	case <-time.After(time.Second):
		t.Fatal("explicit save did not recover replaced connection")
	}
}

// wecomTextRecorder 记录已有耐久 Outbox 交给平台的实际文本片段。
type wecomTextRecorder struct {
	// recordingPlatform 保留其余未修改的传输行为。
	recordingPlatform
	// parts 记录发送的有序文本片段。
	parts []string
}

func (p *wecomTextRecorder) Send(_ context.Context, _ models.IMConnection, _ models.IMBinding, id, text string, _ *models.IMFile) (string, bool, error) {
	p.parts = append(p.parts, text)
	return id, false, nil
}

func TestWecomDeliveryRespectsUTF8ByteLimitUsingExistingOutbox(t *testing.T) {
	s, _, _ := newIMFixture(t)
	ctx := context.Background()
	connection, err := s.SaveConfiguration(ctx, "", ConnectionConfiguration{Name: "企业微信", Platform: constants.IMPlatformWecom, AppID: "bot", AppSecret: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	binding := models.IMBinding{ID: "wecom-binding", ConnectionID: connection.ID, ExternalChatID: "member", Kind: constants.IMChatKindDirect, Name: "成员", State: constants.IMBindingStateApproved, WakerID: "waker", Revision: 1}
	if err := s.config.Store.DiscoverBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	platform := &wecomTextRecorder{}
	s.config.Platforms[constants.IMPlatformWecom] = platform
	content := strings.Repeat("😀", 2100)
	delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "wecom-request", TargetID: binding.ID, Content: content, ChatID: "chat", ExecutionID: "execution"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sendDelivery(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	for _, part := range platform.parts {
		if len(part) > 4096 {
			t.Fatalf("Markdown exceeded UTF-8 byte limit: %d", len(part))
		}
	}
	if len(platform.parts) != 3 || strings.Join(platform.parts, "") != content {
		t.Fatal("Outbox splitting lost or reordered content")
	}
	persisted, err := s.config.Store.Delivery(ctx, delivery.ID)
	if err != nil || persisted.State != constants.IMDeliveryStateSent || persisted.NextPart != 3 || len(persisted.Receipts) != 3 {
		t.Fatalf("durable delivery boundary was not reused: %+v %v", persisted, err)
	}
}

// preparingWecomPlatform 以认证闸门模拟同一长连接的准备、就绪与失活竞争。
type preparingWecomPlatform struct {
	// recordingPlatform 复用本次用例未改变的下载和运行端口。
	recordingPlatform
	// ready 表示真实认证是否已经完成，不作为业务持久状态。
	ready bool
	// failBeforeWrite 模拟就绪检查后会话退出且消息尚未写出。
	failBeforeWrite bool
	// loseACK 模拟消息已经写出但平台未给出回执。
	loseACK bool
	// fileError 模拟尚未提交聊天消息时的上传或文件错误。
	fileError error
	// sends 记录 Send 调用次数，包含发送前失活。
	sends int
	// writes 记录真正写出的聊天消息身份。
	writes []string
}

func (p *preparingWecomPlatform) ConnectionReady(string) bool { return p.ready }
func (p *preparingWecomPlatform) Send(_ context.Context, _ models.IMConnection, _ models.IMBinding, id, _ string, file *models.IMFile) (string, bool, error) {
	p.sends++
	if file != nil && p.fileError != nil {
		err := p.fileError
		p.fileError = nil
		return "", true, err
	}
	if p.failBeforeWrite {
		p.failBeforeWrite = false
		return "", true, status_error.IMServiceUnavailable
	}
	p.writes = append(p.writes, id)
	if p.loseACK {
		return "", false, status_error.IMPlatformUnavailable
	}
	return id, false, nil
}

func TestWecomPendingDeliveryWaitsForAuthenticationWithoutClaimingOrResending(t *testing.T) {
	s, _, _ := newIMFixture(t)
	ctx := context.Background()
	connection, err := s.SaveConfiguration(ctx, "", ConnectionConfiguration{Name: "企业微信", Platform: constants.IMPlatformWecom, AppID: "bot", AppSecret: "secret", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	binding := models.IMBinding{ID: "waiting-binding", ConnectionID: connection.ID, ExternalChatID: "member", Kind: constants.IMChatKindDirect, Name: "成员", State: constants.IMBindingStateApproved, WakerID: "waker", Revision: 1}
	if err := s.config.Store.DiscoverBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	platform := &preparingWecomPlatform{}
	s.config.Platforms[constants.IMPlatformWecom] = platform
	delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "waiting-request", TargetID: binding.ID, Content: "等待连接后发送", ChatID: "chat", ExecutionID: "execution"})
	if err != nil {
		t.Fatal(err)
	}
	// 模拟已有维护节拍多次发现待发内容；认证期间不能领取、增加尝试或调用发送。
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.sendDelivery(ctx, delivery); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := s.config.Store.Delivery(ctx, delivery.ID)
	if err != nil || pending.State != constants.IMDeliveryStatePending || pending.Attempts != 0 || platform.sends != 0 {
		t.Fatalf("preparing connection consumed pending delivery: %+v sends=%d error=%v", pending, platform.sends, err)
	}
	platform.ready = true
	if err := s.sendDelivery(ctx, pending); err != nil {
		t.Fatal(err)
	}
	// 第二个恢复唤醒仍携带同一旧快照，必须由原领取边界阻止重复发送。
	if err := s.sendDelivery(ctx, pending); err != nil {
		t.Fatal(err)
	}
	sent, err := s.config.Store.Delivery(ctx, delivery.ID)
	if err != nil || sent.State != constants.IMDeliveryStateSent || sent.Attempts != 1 || sent.NextPart != 1 || platform.sends != 1 || len(platform.writes) != 1 {
		t.Fatalf("recovery did not send exactly once: %+v sends=%d writes=%d error=%v", sent, platform.sends, len(platform.writes), err)
	}
}

func TestWecomOnlyDefinitelyUnsentConnectionRaceReturnsToPending(t *testing.T) {
	for _, lostACK := range []bool{false, true} {
		t.Run(map[bool]string{false: "lost_before_write", true: "lost_ack"}[lostACK], func(t *testing.T) {
			s, _, _ := newIMFixture(t)
			ctx := context.Background()
			connection, err := s.SaveConfiguration(ctx, "", ConnectionConfiguration{Name: "企业微信", Platform: constants.IMPlatformWecom, AppID: "bot", AppSecret: "secret", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			binding := models.IMBinding{ID: "race-binding", ConnectionID: connection.ID, ExternalChatID: "member", Kind: constants.IMChatKindDirect, Name: "成员", State: constants.IMBindingStateApproved, WakerID: "waker", Revision: 1}
			if err := s.config.Store.DiscoverBinding(ctx, binding); err != nil {
				t.Fatal(err)
			}
			platform := &preparingWecomPlatform{ready: true, failBeforeWrite: !lostACK, loseACK: lostACK}
			s.config.Platforms[constants.IMPlatformWecom] = platform
			delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "race-request", TargetID: binding.ID, Content: "结果", ChatID: "chat", ExecutionID: "execution"})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.sendDelivery(ctx, delivery); err != nil {
				t.Fatal(err)
			}
			progress, err := s.config.Store.Delivery(ctx, delivery.ID)
			if err != nil {
				t.Fatal(err)
			}
			if lostACK {
				if progress.State != constants.IMDeliveryStateUnknown || len(platform.writes) != 1 {
					t.Fatalf("lost ACK became automatically retryable: %+v", progress)
				}
				if err := s.sendDelivery(ctx, progress); err != nil {
					t.Fatal(err)
				}
				if platform.sends != 1 || len(platform.writes) != 1 {
					t.Fatal("unknown delivery was automatically resent")
				}
				if err := s.RetryDelivery(ctx, delivery.ID, false); !errors.Is(err, status_error.IMDeliveryRetryUnsafe) {
					t.Fatalf("unknown retry no longer requires confirmation: %v", err)
				}
			} else {
				if progress.State != constants.IMDeliveryStatePending || progress.NextPart != 0 || len(platform.writes) != 0 {
					t.Fatalf("definitely unsent result was lost: %+v", progress)
				}
				if err := s.sendDelivery(ctx, progress); err != nil {
					t.Fatal(err)
				}
				final, err := s.config.Store.Delivery(ctx, delivery.ID)
				if err != nil || final.State != constants.IMDeliveryStateSent || final.NextPart != 1 || platform.sends != 2 || len(platform.writes) != 1 {
					t.Fatalf("safe recovery did not write exactly once: %+v sends=%d writes=%d error=%v", final, platform.sends, len(platform.writes), err)
				}
			}
		})
	}
}

func TestWecomUploadRetryClassificationPreservesPendingOrExplicitFailure(t *testing.T) {
	for _, outcome := range []string{"interrupted_upload", "rejected_upload", "invalid_file"} {
		t.Run(outcome, func(t *testing.T) {
			s, _, _ := newIMFixture(t)
			ctx := context.Background()
			connection, err := s.SaveConfiguration(ctx, "", ConnectionConfiguration{Name: "企业微信", Platform: constants.IMPlatformWecom, AppID: "bot", AppSecret: "secret", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			binding := models.IMBinding{ID: "file-binding", ConnectionID: connection.ID, ExternalChatID: "member", Kind: constants.IMChatKindDirect, Name: "成员", State: constants.IMBindingStateApproved, WakerID: "waker", Revision: 1}
			if err := s.config.Store.DiscoverBinding(ctx, binding); err != nil {
				t.Fatal(err)
			}
			platform := &preparingWecomPlatform{ready: true, fileError: status_error.IMServiceUnavailable}
			if outcome == "rejected_upload" {
				platform.fileError = status_error.IMPlatformUnavailable
			}
			if outcome == "invalid_file" {
				platform.fileError = status_error.IMFileInvalid
			}
			s.config.Platforms[constants.IMPlatformWecom] = platform
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "result.bin")
			if err := os.WriteFile(path, []byte("frozen output"), 0600); err != nil {
				t.Fatal(err)
			}
			delivery, err := s.CreateDelivery(ctx, DeliveryRequest{RequestID: "file-request", TargetID: binding.ID, Files: []string{path}, AllowedRoots: []string{root}, ChatID: "chat", ExecutionID: "execution"})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.sendDelivery(ctx, delivery); err != nil {
				t.Fatal(err)
			}
			progress, err := s.config.Store.Delivery(ctx, delivery.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(platform.writes) != 0 || progress.NextPart != 0 {
				t.Fatal("upload failure advanced chat receipt")
			}
			if outcome == "interrupted_upload" {
				if progress.State != constants.IMDeliveryStatePending {
					t.Fatalf("interrupted upload was not retained: %+v", progress)
				}
				if err := s.sendDelivery(ctx, progress); err != nil {
					t.Fatal(err)
				}
				final, err := s.config.Store.Delivery(ctx, delivery.ID)
				if err != nil || final.State != constants.IMDeliveryStateSent || final.NextPart != 1 || len(platform.writes) != 1 {
					t.Fatalf("file did not recover exactly once: %+v error=%v", final, err)
				}
			} else {
				if progress.State != constants.IMDeliveryStateFailed {
					t.Fatalf("explicit failure was made automatically retryable: %+v", progress)
				}
				if err := s.sendDelivery(ctx, progress); err != nil {
					t.Fatal(err)
				}
				if platform.sends != 1 || len(platform.writes) != 0 {
					t.Fatal("failed upload was automatically retried")
				}
			}
		})
	}
}

func TestIMGroupExecutionReceivesOnlyBodyWithoutSenderIdentity(t *testing.T) {
	for _, team := range []bool{false, true} {
		label := "partner"
		if team {
			label = "team"
		}
		t.Run(label, func(t *testing.T) {
			service, db, _ := newIMFixture(t)
			attachConversationReaders(t, service, db)
			ctx := context.Background()
			sender := "wo-private-member-do-not-send-to-cli"
			binding, err := service.config.Store.Binding(ctx, "b")
			if err != nil {
				t.Fatal(err)
			}
			// Kind 是平台发现后的事实，SaveBinding 只写管理员配置字段，因此通过真实临时表设置。
			if err := db.Model(&models.IMBinding{}).Where("id = ?", binding.ID).Update("kind", constants.IMChatKindGroup).Error; err != nil {
				t.Fatal(err)
			}
			binding.Kind = constants.IMChatKindGroup
			binding.ControllerIDs = []string{sender}
			if team {
				binding.WakerID = ""
				binding.ChannelID = "team"
			}
			if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
				t.Fatal(err)
			}
			executions := &missingConversationExecutions{chats: service.config.Chats, directory: service.config.DataDirectory}
			teams := &capturingTeams{}
			service.config.Executions = executions
			service.config.Teams = teams
			input := models.IMInbox{ID: "body-only", BindingID: binding.ID, ConnectionID: "c", SenderID: sender, Content: "你好", State: constants.IMInboxStatePending}
			if err := service.config.Store.SaveInbox(ctx, input); err != nil {
				t.Fatal(err)
			}
			if err := service.route(ctx, input); err != nil {
				t.Fatal(err)
			}
			if err := db.First(&input, "id = ?", input.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err := service.accept(ctx, input); err != nil {
				t.Fatal(err)
			}
			var actual execution.WorkInput
			if team {
				if teams.input.Create == nil {
					t.Fatal("group input did not reach team execution boundary")
				}
				actual = teams.input.Create.Input
			} else {
				if len(executions.inputs) != 1 {
					t.Fatal("group input did not reach execution Start")
				}
				actual = executions.inputs[0].Input
			}
			if actual.Content != "你好" {
				t.Fatalf("CLI work body was polluted: %q", actual.Content)
			}
			encoded, err := json.Marshal(actual)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{sender, "[群成员", "@吴俊杰的机器人"} {
				if strings.Contains(string(encoded), private) {
					t.Fatalf("identity or invocation leaked into execution fields: %s", encoded)
				}
			}
			var persisted models.IMInbox
			if err := db.First(&persisted, "id = ?", input.ID).Error; err != nil {
				t.Fatal(err)
			}
			if persisted.SenderID != sender || persisted.Content != "你好" || persisted.State != constants.IMInboxStateAccepted {
				t.Fatalf("audit identity or accepted body changed: %+v", persisted)
			}
		})
	}
}

func TestIMGroupNewCommandPreservesSenderAuthorizationWithoutRuntimeSubmission(t *testing.T) {
	for _, authorized := range []bool{true, false} {
		label := "controller"
		if !authorized {
			label = "member"
		}
		t.Run(label, func(t *testing.T) {
			service, db, _ := newIMFixture(t)
			ctx := context.Background()
			binding, err := service.config.Store.Binding(ctx, "b")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.IMBinding{}).Where("id = ?", binding.ID).Update("kind", constants.IMChatKindGroup).Error; err != nil {
				t.Fatal(err)
			}
			binding.Kind = constants.IMChatKindGroup
			binding.ControllerIDs = []string{"controller"}
			binding.ActiveLinkID = "existing-link"
			if err := service.config.Store.SaveBinding(ctx, binding, binding.Revision); err != nil {
				t.Fatal(err)
			}
			executions := &missingConversationExecutions{}
			service.config.Executions = executions
			sender := "controller"
			if !authorized {
				sender = "member"
			}
			// 适配器规范化 @机器人 /new 后，命令直接走原权限与切换边界，不构造 CLI 工作。
			input := models.IMInbox{ID: "new-command", BindingID: binding.ID, ConnectionID: "c", SenderID: sender, Content: "/new", State: constants.IMInboxStatePending}
			if err := service.config.Store.SaveInbox(ctx, input); err != nil {
				t.Fatal(err)
			}
			if err := service.route(ctx, input); err != nil {
				t.Fatal(err)
			}
			actual, err := service.config.Store.Binding(ctx, binding.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (authorized && actual.ActiveLinkID != "") || (!authorized && actual.ActiveLinkID != "existing-link") {
				t.Fatalf("group command authorization changed: allowed=%v link=%q", authorized, actual.ActiveLinkID)
			}
			if len(executions.inputs) != 0 {
				t.Fatal("group /new was submitted as CLI work")
			}
			var persisted models.IMInbox
			if err := db.First(&persisted, "id = ?", input.ID).Error; err != nil {
				t.Fatal(err)
			}
			if persisted.SenderID != sender || persisted.State != constants.IMInboxStateProcessed {
				t.Fatal("command audit identity or terminal state lost")
			}
		})
	}
}
