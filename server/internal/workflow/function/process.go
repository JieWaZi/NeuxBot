package function

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
)

// ProcessResult 保存有界日志和真实进程退出码。
type ProcessResult struct {
	// ExitCode 是已等待退出的子进程状态。
	ExitCode int `json:"exitCode"`
	// Stdout 是与完成收据分离的标准输出。
	Stdout string `json:"stdout"`
	// Stderr 是与完成收据分离的错误输出。
	Stderr string `json:"stderr"`
	// Truncated 表示输出超出展示预算。
	Truncated bool `json:"logsTruncated"`
}

// limitedLog 不阻塞子进程，超限内容不进入持久结果。
type limitedLog struct {
	// mutex 保护 exec 的异步日志复制。
	mutex sync.Mutex
	// buffer 最多保留一条日志预算。
	buffer bytes.Buffer
	// truncated 记录丢弃了输出。
	truncated bool
}

func (b *limitedLog) Write(value []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	n := len(value)
	space := constants.WorkflowLogLimitBytes - b.buffer.Len()
	if space < len(value) {
		b.truncated = true
		value = value[:max(0, space)]
	}
	_, _ = b.buffer.Write(value)
	return n, nil
}
func (b *limitedLog) value() (string, bool) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String(), b.truncated
}

// Command 等待真实退出，并在取消、超时或内存超限时终止所持进程组。
func Command(ctx context.Context, executable string, args []string, directory string, environment []string, input io.Reader) (ProcessResult, error) {
	command := exec.Command(executable, args...)
	command.Dir = directory
	command.Env = environment
	command.Stdin = input
	command.WaitDelay = time.Second
	stdout, stderr := &limitedLog{}, &limitedLog{}
	command.Stdout, command.Stderr = stdout, stderr
	configureGroup(command)
	if err := command.Start(); err != nil {
		return ProcessResult{}, hostError("start", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var failure error
	var exit error
	waiting := true
	for waiting {
		select {
		case exit = <-done:
			waiting = false
		case <-ctx.Done():
			killGroup(command)
			exit = <-done
			waiting = false
			if ctx.Err() == context.DeadlineExceeded {
				failure = status_error.WorkflowTimeout
			} else {
				failure = status_error.WorkflowCancelled
			}
		case <-ticker.C:
			rss, err := RSSBytes(ctx, command.Process.Pid)
			if err != nil {
				killGroup(command)
				exit = <-done
				waiting = false
				failure = err
			} else if rss > 128<<20 {
				killGroup(command)
				exit = <-done
				waiting = false
				failure = status_error.WorkflowBudgetExceeded.WithField("budget", "function_memory")
			}
		}
	}
	// 主进程已退出也不允许同一组内的后台子进程脱离这个效果的生命周期。
	killGroup(command)
	out, truncatedOut := stdout.value()
	errOut, truncatedErr := stderr.value()
	result := ProcessResult{ExitCode: command.ProcessState.ExitCode(), Stdout: out, Stderr: errOut, Truncated: truncatedOut || truncatedErr}
	if failure != nil {
		return result, failure
	}
	if exit != nil {
		if _, ok := exit.(*exec.ExitError); ok {
			return result, status_error.WorkflowEffectFailed.WithField("code", "PROCESS_FAILED")
		}
		return result, hostError("wait", exit)
	}
	return result, nil
}

// RSSBytes 在有界采样中查询受管进程内存，并保留效果自身的停止原因。
func RSSBytes(ctx context.Context, pid int) (int64, error) {
	sample, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return processRSS(sample, ctx, strconv.Itoa(pid))
}

// memorySampleError 在系统采样边界首次转换错误，采样超时不冒充效果截止。
func memorySampleError(effectContext context.Context, err error) error {
	switch effectContext.Err() {
	case context.DeadlineExceeded:
		return status_error.WorkflowTimeout
	case context.Canceled:
		return status_error.WorkflowCancelled
	default:
		return hostError("memory_sample", err)
	}
}

// Execute 从专用认证结果通道获取一次 Function 回执。
func Execute(ctx context.Context, request Request, language, python, directory string) (Result, ProcessResult, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{}, ProcessResult{}, hostError("result_listener", err)
	}
	defer listener.Close()
	request.Address = listener.Addr().String()
	request.Token = util.New()
	encoded, err := json.Marshal(request)
	if err != nil {
		return Result{}, ProcessResult{}, hostError("encode_request", err)
	}
	received := make(chan Result, 1)
	receiveDone := make(chan struct{})
	go func() {
		defer close(receiveDone)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_ = connection.SetReadDeadline(time.UnixMilli(request.DeadlineAt))
		var result Result
		if decodeErr := json.NewDecoder(io.LimitReader(connection, constants.WorkflowOutputLimitBytes+8192)).Decode(&result); decodeErr == nil && result.Token == request.Token && result.RequestID == request.RequestID {
			received <- result
		}
	}()
	executable, args := "", []string{}
	if language == "javascript" {
		executable, err = os.Executable()
		args = []string{WorkerFlag}
	} else if language == "python" {
		executable = python
		args = []string{"-I", "-c", pythonBootstrap}
	} else {
		err = status_error.WorkflowCapabilityUnsupported
	}
	if err != nil || executable == "" {
		listener.Close()
		<-receiveDone
		return Result{}, ProcessResult{}, status_error.WorkflowCapabilityUnsupported.WithField("capability", "function_interpreter")
	}
	process, err := Command(ctx, executable, args, directory, SafeEnvironment(), bytes.NewReader(encoded))
	listener.Close()
	<-receiveDone
	if err != nil {
		return Result{}, process, err
	}
	select {
	case result := <-received:
		if !result.OK {
			return result, process, status_error.WorkflowEffectFailed.WithField("code", "FUNCTION_FAILED").WithField("reason", result.Error)
		}
		return result, process, nil
	default:
		return Result{}, process, status_error.WorkflowEffectFailed.WithField("code", "FUNCTION_RESULT_MISSING")
	}
}

// SafeEnvironment 只继承启动命令所需的非凭据平台变量。
func SafeEnvironment() []string {
	keys := []string{"PATH", "LANG", "LC_ALL", "TMPDIR", "TEMP", "TMP", "SYSTEMROOT", "WINDIR"}
	values := []string{}
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			values = append(values, key+"="+value)
		}
	}
	return values
}

// PythonVersion 校验已配置解释器，不安装或改变用户环境。
func PythonVersion(ctx context.Context, executable string) error {
	command := exec.CommandContext(ctx, executable, "-I", "-c", "import sys; assert sys.version_info >= (3,11), 'Python 3.11+ required'")
	command.Env = SafeEnvironment()
	if output, err := command.CombinedOutput(); err != nil {
		return hostError("python_version_"+strings.TrimSpace(string(output)), err)
	}
	return nil
}

const pythonBootstrap = `import sys,json,socket,time,traceback
r=json.load(sys.stdin)
t=time.monotonic()
out={"requestId":r["requestId"],"token":r["token"],"ok":False,"data":None}
try:
 if sys.version_info < (3,11): raise RuntimeError("Python 3.11+ required")
 scope={"__name__":"workflow_function"}
 exec(compile(r["code"],"workflow-function.py","exec"),scope)
 value=scope["main"](r["input"])
 wire=json.dumps(value,allow_nan=False,ensure_ascii=False)
 if len(wire.encode())>1048576: raise RuntimeError("FUNCTION_OUTPUT_LIMIT")
 out.update(ok=True,data=value)
except BaseException as e:
 out["error"]=str(e)
 traceback.print_exc()
out["durationMs"]=int((time.monotonic()-t)*1000)
host,port=r["address"].rsplit(":",1)
with socket.create_connection((host,int(port)),timeout=1) as s:
 s.sendall(json.dumps(out,allow_nan=False,ensure_ascii=False).encode()+b"\n")
`
