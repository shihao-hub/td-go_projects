package service

import "errors"

// OperationError 是稳定的业务错误，退出码由 CLI 适配层决定。
type OperationError struct {
	Code    string
	Message string
}

func (e *OperationError) Error() string { return e.Message }

// ErrorCode 返回稳定业务错误码。
func (e *OperationError) ErrorCode() string { return e.Code }

func coded(code, message string) error {
	return &OperationError{Code: code, Message: message}
}

func asCoded(err error) error {
	var op *OperationError
	if errors.As(err, &op) {
		return op
	}
	return nil
}
