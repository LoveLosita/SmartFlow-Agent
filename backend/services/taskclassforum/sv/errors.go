package sv

import "errors"

var (
	// ErrTaskClassPortMissing 表示计划广场需要访问旧 TaskClass，但 adapter 尚未注入。
	ErrTaskClassPortMissing = errors.New("taskclassforum taskclass adapter is nil")
)
