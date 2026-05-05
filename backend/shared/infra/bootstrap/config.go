package bootstrap

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
)

// LoadConfig 统一加载后端进程配置。
//
// 职责边界：
// 1. 只负责把 config.yaml 读入 viper，不解释具体业务配置语义。
// 2. 同时兼容从仓库根目录和 backend 目录启动的两种路径。
// 3. 失败时返回 error，由各进程入口决定是否退出。
func LoadConfig() error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("backend")
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	log.Println("Config loaded successfully")
	return nil
}
