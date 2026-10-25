package aiaccount

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/accountconfig"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/runtimes"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
)

const kimiCredentialMarker = "kimi-cli-managed"

var kimiDeviceURL = regexp.MustCompile(`https://[^\s]+authorize_device\?user_code=[^\s]+`)

// kimiLoginOutput 只截取 CLI 给用户展示的设备登录地址，不保留进程日志。
type kimiLoginOutput struct {
	// mutex 保护并发写入的短暂输出缓冲。
	mutex sync.Mutex
	// buffer 保存尚未出现完整设备登录地址的少量输出。
	buffer string
	// ready 首次发现官方设备登录地址时通知启动请求。
	ready chan string
}

func (w *kimiLoginOutput) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.buffer += string(data)
	if len(w.buffer) > 8192 {
		w.buffer = w.buffer[len(w.buffer)-8192:]
	}
	for {
		line, remainder, found := strings.Cut(w.buffer, "\n")
		if !found {
			break
		}
		w.buffer = remainder
		if match := kimiDeviceURL.FindString(line); match != "" {
			select {
			case w.ready <- match:
			default:
			}
		}
	}
	return len(data), nil
}

func (s *Service) kimiHome(id string) (string, error) {
	if s.kimiDataDirectory == "" || len(id) != 32 {
		return "", status_error.AIAccountConfigurationInvalid
	}
	for _, char := range id {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return "", status_error.AIAccountConfigurationInvalid
		}
	}
	for _, directory := range []string{filepath.Dir(s.kimiDataDirectory), s.kimiDataDirectory, filepath.Join(s.kimiDataDirectory, id)} {
		if err := os.Mkdir(directory, 0700); err != nil && !os.IsExist(err) {
			return "", status_error.AIAccountWorkspaceError
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", status_error.AIAccountWorkspaceError
		}
		if err := os.Chmod(directory, 0700); err != nil {
			return "", status_error.AIAccountWorkspaceError
		}
	}
	return filepath.Join(s.kimiDataDirectory, id), nil
}

func (s *Service) prepareKimiSubscription(account models.AIAccount) (runtimes.RuntimeTarget, error) {
	home, err := s.kimiHome(account.ID)
	if err != nil {
		return runtimes.RuntimeTarget{}, err
	}
	if info, err := os.Stat(filepath.Join(home, "config.toml")); err != nil || !info.Mode().IsRegular() {
		return runtimes.RuntimeTarget{}, status_error.RuntimeAuthRequired
	}
	environment := accountconfig.HomeEnvironment(constants.RuntimeIDKimi, home)
	environment[constants.AIAccountIdentityEnvironment] = account.ID + ":" + strconv.FormatInt(account.Revision, 10)
	return runtimes.RuntimeTarget{AccountID: account.ID, Environment: environment, Isolated: true, CloseAfterRun: true}, nil
}

// KimiSubscriptionModels 读取该订阅账号独有的原生模型别名目录。
func (s *Service) KimiSubscriptionModels(ctx context.Context, id string) ([]string, string, error) {
	account, err := s.accounts.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if account.AuthMode != constants.AccountAuthModeSubscription || len(account.Endpoints) != 1 || account.Endpoints[0].BaseURL != constants.AIAccountKimiURL {
		return nil, "", status_error.AIAccountConfigurationInvalid
	}
	home, err := s.kimiHome(id)
	if err != nil {
		return nil, "", err
	}
	var config struct {
		// DefaultModel 是 Kimi 登录目录声明的默认模型别名。
		DefaultModel string `toml:"default_model"`
		// Models 是 Kimi 登录目录可选的模型别名集合。
		Models map[string]struct{} `toml:"models"`
	}
	if _, err := toml.DecodeFile(filepath.Join(home, "config.toml"), &config); err != nil {
		return nil, "", status_error.RuntimeAuthRequired
	}
	ids := make([]string, 0, len(config.Models))
	for modelID := range config.Models {
		ids = append(ids, modelID)
	}
	slices.Sort(ids)
	return ids, config.DefaultModel, nil
}

func kimiExecutable() (string, error) {
	if path, err := exec.LookPath("kimi"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", status_error.AIAccountLoginFailed
	}
	path := filepath.Join(home, ".kimi-code", "bin", "kimi")
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", status_error.AIAccountLoginFailed
	}
	return path, nil
}

func (s *Service) startKimiLogin(id string, revision int64) (Login, error) {
	release, err := s.Exclusive(id)
	if err != nil {
		return Login{}, err
	}
	home, err := s.kimiHome(id)
	if err != nil {
		release()
		return Login{}, err
	}
	binary, err := kimiExecutable()
	if err != nil {
		release()
		return Login{}, err
	}
	s.loginCommit.Lock()
	s.mutex.Lock()
	if s.ctx == nil || s.ctx.Err() != nil {
		s.mutex.Unlock()
		s.loginCommit.Unlock()
		release()
		return Login{}, status_error.OperationCanceled
	}
	expiresAt := time.Now().Add(30 * time.Minute)
	job := &loginJob{revision: revision, release: release, kimiExit: make(chan struct{}), kimiReady: make(chan struct{}), view: Login{ID: util.New(), ExpiresAt: expiresAt.UnixMilli(), IntervalMS: 2000}}
	job.ctx, job.cancel = context.WithDeadline(s.ctx, expiresAt)
	previous := s.jobs[id]
	if previous != nil {
		previous.cancel()
	}
	s.jobs[id] = job
	s.workers.Add(1)
	s.mutex.Unlock()
	s.loginCommit.Unlock()
	if previous != nil {
		previous.mutex.Lock()
		previous.canceled = true
		previous.mutex.Unlock()
	}
	output := &kimiLoginOutput{ready: make(chan string, 1)}
	command := exec.CommandContext(job.ctx, binary, "login")
	command.Env = append(accountconfig.Environment(os.Environ()), "KIMI_CODE_HOME="+home)
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		close(job.kimiExit)
		job.cancel()
		release()
		s.workers.Done()
		return Login{}, status_error.AIAccountLoginFailed
	}
	go func() {
		defer s.workers.Done()
		defer release()
		defer job.cancel()
		err := command.Wait()
		close(job.kimiExit)
		select {
		case <-job.kimiReady:
		case <-job.ctx.Done():
		}
		job.mutex.Lock()
		if err == nil && job.ctx.Err() == nil {
			_, err = s.commitKimiLogin(job.ctx, id, job)
		}
		job.snapshotMutex.Lock()
		if err != nil || job.ctx.Err() != nil {
			if job.view.ErrorCode == "" {
				job.view.ErrorCode = loginErrorCode(err)
			}
		}
		view := job.view
		job.snapshotMutex.Unlock()
		job.mutex.Unlock()
		release()
		s.publishLogin(id, view)
	}()
	var address string
	select {
	case address = <-output.ready:
	case <-job.kimiExit:
		select {
		case address = <-output.ready:
		default:
			job.cancel()
			return Login{}, status_error.AIAccountLoginFailed
		}
	case <-time.After(20 * time.Second):
		job.cancel()
		return Login{}, status_error.AIAccountLoginFailed
	}
	parsed, parseErr := url.Parse(address)
	if parseErr != nil || parsed.Query().Get("user_code") == "" {
		job.cancel()
		return Login{}, status_error.AIAccountLoginFailed
	}
	job.snapshotMutex.Lock()
	job.view.VerificationURL = address
	job.view.UserCode = parsed.Query().Get("user_code")
	initial := job.view
	job.snapshotMutex.Unlock()
	close(job.kimiReady)
	return initial, nil
}

func (s *Service) commitKimiLogin(ctx context.Context, id string, job *loginJob) (Login, error) {
	home, err := s.kimiHome(id)
	if err != nil {
		return Login{}, err
	}
	if info, err := os.Stat(filepath.Join(home, "config.toml")); err != nil || !info.Mode().IsRegular() {
		return Login{}, status_error.AIAccountLoginFailed
	}
	s.loginCommit.Lock()
	defer s.loginCommit.Unlock()
	s.mutex.Lock()
	currentJob := s.jobs[id]
	s.mutex.Unlock()
	if currentJob != job || job.ctx.Err() != nil {
		return Login{}, status_error.AIAccountLoginFailed
	}
	current, err := s.accounts.Get(ctx, id)
	if err != nil {
		return Login{}, err
	}
	if current.Revision != job.revision {
		return Login{}, status_error.AIAccountRevisionConflict
	}
	if err := s.accounts.StoreCredential(ctx, id, current.Revision, kimiCredentialMarker, ""); err != nil {
		return Login{}, err
	}
	job.snapshotMutex.Lock()
	job.view.Complete = true
	view := job.view
	job.snapshotMutex.Unlock()
	return view, nil
}

// RemoveKimiHome 在账号删除后清除该账号独有的原生授权文件。
func (s *Service) RemoveKimiHome(id string) {
	s.mutex.Lock()
	job := s.jobs[id]
	delete(s.jobs, id)
	s.mutex.Unlock()
	if job != nil {
		job.cancel()
		if job.kimiExit != nil {
			select {
			case <-job.kimiExit:
			case <-time.After(5 * time.Second):
				logx.Errorf("cancel Kimi account login timed out: account_id=%s", id)
				go func() {
					<-job.kimiExit
					job.release()
					s.removeKimiHome(id)
				}()
				return
			}
		}
		if job.release != nil {
			job.release()
		}
	}
	s.removeKimiHome(id)
}

func (s *Service) removeKimiHome(id string) {
	if s.kimiDataDirectory == "" || len(id) != 32 {
		return
	}
	for _, char := range id {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return
		}
	}
	if err := os.RemoveAll(filepath.Join(s.kimiDataDirectory, id)); err != nil {
		logx.Errorf("remove Kimi account home failed: account_id=%s error=%v", id, err)
	}
}
