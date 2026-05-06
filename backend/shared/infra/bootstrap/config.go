package bootstrap

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/viper"
)

const configFileEnv = "SMARTFLOW_CONFIG_FILE"

func LoadConfig() error {
	viper.Reset()
	viper.SetEnvPrefix("SMARTFLOW")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if configFile := os.Getenv(configFileEnv); configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("failed to read config file %q: %w", configFile, err)
		}
		log.Printf("Config loaded successfully from %s", configFile)
		return nil
	}

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
