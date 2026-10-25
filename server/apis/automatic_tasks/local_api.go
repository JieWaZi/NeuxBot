package automatic_tasks

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/internal/logx"
	"github.com/JieWaZi/NeuxBot/server/internal/util"
	workflowinput "github.com/JieWaZi/NeuxBot/server/internal/workflow/input"
	"github.com/JieWaZi/NeuxBot/server/services/schedule"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// NewLocalHandler 创建任务级密钥保护的本机 HTTP 入口，由 Core 注入监听组件。
func NewLocalHandler(service *schedule.Service, config schedule.Config, address string) http.Handler {
	api := &taskAPI{service: service, config: config}
	return api.localHandler(address)
}

func (s *taskAPI) localHandler(address string) http.Handler {
	ip := net.ParseIP(address)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !apiHostAllowed(r.Host, ip) || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			writeAPIError(w, status_error.AutomaticTaskTokenInvalid)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 || len(parts) > 7 || parts[0] != "v1" || parts[1] != "automatic-tasks" || parts[2] == "" || r.URL.RawQuery != "" {
			writeAPIError(w, status_error.AutomaticRunNotFound)
			return
		}
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") {
			writeAPIError(w, status_error.AutomaticTaskTokenInvalid)
			return
		}
		token := strings.TrimPrefix(authorization, "Bearer ")
		if len(parts) == 4 && parts[3] == "files" && r.Method == http.MethodPost {
			s.uploadAPIFile(w, r, parts[2], token)
			return
		}
		if parts[3] != "runs" {
			writeAPIError(w, status_error.AutomaticRunNotFound)
			return
		}
		if len(parts) == 6 && parts[5] == "result" && r.Method == http.MethodGet {
			result, err := s.ReadAPIResult(r.Context(), parts[2], token, parts[4])
			if err != nil {
				writeAPIError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
		if len(parts) == 7 && parts[5] == "files" && r.Method == http.MethodGet {
			s.downloadAPIFile(w, r, parts[2], token, parts[4], parts[6])
			return
		}
		if len(parts) == 5 && r.Method == http.MethodGet {
			result, err := s.ReadAPIState(r.Context(), parts[2], token, parts[4])
			if err != nil {
				writeAPIError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, result)
			return
		}
		if len(parts) != 4 || r.Method != http.MethodPost {
			writeAPIError(w, status_error.BadRequest)
			return
		}
		contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || contentType != "application/json" {
			writeAPIError(w, status_error.BadRequest)
			return
		}
		// 先验证令牌，再读取有上限的正文，避免匿名请求占用接纳或解码资源。
		task, err := s.service.AuthenticateTask(r.Context(), parts[2], token)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, constants.AutomaticTaskRequestMaximumBytes))
		if err != nil || !utf8.Valid(data) {
			writeAPIError(w, status_error.BadRequest)
			return
		}
		if err := workflowinput.ValidateJSONEnvelope(data); err != nil {
			writeAPIError(w, err)
			return
		}
		var presence map[string]json.RawMessage
		if json.Unmarshal(data, &presence) != nil || presence == nil {
			writeAPIError(w, status_error.BadRequest)
			return
		}
		var request *struct {
			// Input 是伙伴或团队目标的本次附加文本。
			Input string `json:"input"`
			// Args 仅用于工作流选择由 API 传入的顶层参数。
			Args json.RawMessage `json:"args"`
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request == nil {
			writeAPIError(w, status_error.BadRequest)
			return
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			writeAPIError(w, status_error.BadRequest)
			return
		}
		input := request.Input
		var callArgs map[string]any
		if task.WorkflowID != "" {
			if _, present := presence["input"]; present {
				writeAPIError(w, status_error.BadRequest)
				return
			}
			if request.Args != nil {
				if json.Unmarshal(request.Args, &callArgs) != nil || callArgs == nil {
					writeAPIError(w, status_error.AutomaticTaskInputInvalid)
					return
				}
				input = ""
			}
		} else if request.Args != nil {
			writeAPIError(w, status_error.BadRequest)
			return
		}
		view, err := s.TriggerAPI(r.Context(), parts[2], token, r.Header.Get("Idempotency-Key"), input, callArgs)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, publicRun(view))
	})
}

func apiHostAllowed(value string, bindIP net.IP) bool {
	host := value
	if strings.Contains(value, ":") {
		var err error
		host, _, err = net.SplitHostPort(value)
		if err != nil {
			return false
		}
	}
	if bindIP == nil {
		return false
	}
	if host == "localhost" {
		return bindIP.IsLoopback()
	}
	ip := net.ParseIP(host)
	if ip == nil || (ip.To4() == nil) != (bindIP.To4() == nil) {
		return false
	}
	return bindIP.IsUnspecified() || ip.Equal(bindIP)
}

func writeJSON(w http.ResponseWriter, code int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, err error) {
	var coded interface {
		error
		StatusCode() int
	}
	status := http.StatusInternalServerError
	if errors.As(err, &coded) {
		status = coded.StatusCode()
	}
	result := map[string]any{"errorCode": util.ErrorCode(err, status_error.AutomaticTaskServiceInvalid.Key())}
	var detail *statuserror.StatusErr
	if errors.As(err, &detail) {
		fields := map[string]string{}
		for _, key := range []string{"jsonPointer", "constraint", "expectedType"} {
			if value := detail.Fields[key]; value != "" {
				fields[key] = value
			}
		}
		if len(fields) > 0 {
			result["fields"] = fields
		}
	}
	writeJSON(w, status, result)
}

func (s *taskAPI) uploadAPIFile(w http.ResponseWriter, r *http.Request, taskID, token string) {
	task, err := s.service.AuthenticateTask(r.Context(), taskID, token)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if task.WorkflowID == "" || s.config.Workflows == nil {
		writeAPIError(w, status_error.BadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, constants.WorkflowFileMaximumBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeAPIError(w, status_error.WorkflowFileInvalid)
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.Value) != 0 || len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 {
		writeAPIError(w, status_error.WorkflowFileInvalid)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIError(w, status_error.WorkflowFileInvalid)
		return
	}
	defer file.Close()
	upload, err := s.config.Workflows.UploadFile(r.Context(), task.WorkflowID, task.ID, header.Filename, file)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, upload)
}

func (s *taskAPI) downloadAPIFile(w http.ResponseWriter, r *http.Request, taskID, token, runID, fileID string) {
	run, err := s.authenticatedWorkflowRun(r.Context(), taskID, token, runID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	file, name, err := s.config.Workflows.OpenOutputFile(r.Context(), run.Configuration.WorkflowID, run.WorkflowRunID, fileID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	defer file.Close()
	serveWorkflowFile(w, r, file, name)
}

func serveWorkflowFile(w http.ResponseWriter, r *http.Request, file *os.File, name string) {
	info, err := file.Stat()
	if err != nil {
		logx.Errorf("stat automatic workflow download failed: error=%v", err)
		writeAPIError(w, status_error.WorkflowFileStorageError)
		return
	}
	if info.Size() > constants.WorkflowFileMaximumBytes {
		writeAPIError(w, status_error.WorkflowBudgetExceeded.WithField("budget", "file_bytes"))
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// 固定响应读取范围，文件在下载期间增长也不能扩大本次响应。
	http.ServeContent(w, r, name, info.ModTime(), io.NewSectionReader(file, 0, info.Size()))
}
