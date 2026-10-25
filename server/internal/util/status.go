package util

import (
	"errors"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/shrewx/ginx/pkg/statuserror"
)

// ErrorCode 读取可安全返回的稳定错误名称，没有状态错误身份时使用调用方指定的领域原因。
func ErrorCode(err error, fallback string) string {
	var keyed interface {
		error
		Key() string
	}
	if errors.As(err, &keyed) {
		return keyed.Key()
	}
	return fallback
}

// HasStatusCode 同时识别原始业务错误及添加上下文后保留的稳定错误码。
func HasStatusCode(err error, expected status_error.StatusError) bool {
	if errors.Is(err, expected) {
		return true
	}
	var commonError statuserror.CommonError
	return errors.As(err, &commonError) && commonError.Code() == expected.Code()
}
