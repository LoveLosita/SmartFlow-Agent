package main

import (
	"github.com/LoveLosita/smartflow/backend/cmd"
)

// main 保留仓库根入口的兼容壳，当前仍转发到 cmd.Start()（等价于 cmd.StartAPI()）。
// 终态会逐步迁移为各服务各自的独立 main.go。
func main() {
	cmd.Start()
}
