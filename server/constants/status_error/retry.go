package status_error

// nonRetryableError 为已经交付的调用保留原始错误，同时阻止上游重复执行副作用。
type nonRetryableError struct {
	// cause 保留既有稳定错误及其原始诊断。
	cause error
}

// Error 保留原有错误文案。
func (e *nonRetryableError) Error() string { return e.cause.Error() }

// Unwrap 保留既有稳定错误的类别判定。
func (e *nonRetryableError) Unwrap() error { return e.cause }

// Retryable 明确禁止知识核心自动重新提交已经执行的模型调用。
func (e *nonRetryableError) Retryable() bool { return false }

// WithoutRetry 保留错误类别，并通过结构化端口禁止调用方自动重复执行。
func WithoutRetry(err error) error {
	if err == nil {
		return nil
	}
	return &nonRetryableError{cause: err}
}
