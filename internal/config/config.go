/*
Copyright 2025 linux.do

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"encoding/json"
	"log"
	"os"

	"github.com/spf13/viper"
)

var Config *configModel

func init() {
	// 加载配置文件路径
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config.yaml"
	}

	// 设置配置文件
	viper.SetConfigFile(configPath)
	viper.AutomaticEnv()

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("[Config] read config failed: %v\n", err)
	}

	// 解析配置到结构体
	var c configModel
	if err := viper.Unmarshal(&c); err != nil {
		log.Fatalf("[Config] parse config failed: %v\n", err)
	}

	if err := c.Features.Validate(); err != nil {
		log.Fatalf("[Config] invalid feature configuration: %v\n", err)
	}

	// 设置全局配置
	Config = &c

	// 打印配置
	printConfig(&c)
}

// printConfig 打印配置内容
func printConfig(c *configModel) {
	configJSON, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		log.Printf("[Config] failed to marshal config: %v\n", err)
		return
	}
	log.Printf("[Config] loaded configuration:\n%s\n", string(configJSON))
}
