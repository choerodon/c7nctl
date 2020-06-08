// Copyright © 2018 choerodon <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"github.com/choerodon/c7nctl/pkg/action"
	"github.com/choerodon/c7nctl/pkg/c7nclient"
	"github.com/choerodon/c7nctl/pkg/cli"
	"github.com/choerodon/c7nctl/pkg/config"
	"github.com/choerodon/c7nctl/pkg/consts"
	c7n_utils "github.com/choerodon/c7nctl/pkg/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
)

var (
	clientPlatformConfig c7nclient.C7NConfig
	clientConfig         c7nclient.C7NContext

	envSettings = cli.New()
)

func main() {
	actionConfig := action.NewCfg()

	cmd := newRootCmd(actionConfig, os.Stdout, os.Args[1:])
	cobra.OnInitialize(initConfig)
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
	defer viper.WriteConfig()
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if envSettings.Debug {
		log.SetLevel(log.DebugLevel)
	}
	if envSettings.CfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(envSettings.CfgFile)
	} else {
		// set default configuration is $HOME/.c7n/config.yml
		viper.AddConfigPath(consts.DefaultConfigPath)
		viper.SetConfigName(consts.DefaultConfigFileName)
		viper.SetConfigType("yml")
	}

	// read in environment variables that match
	viper.AutomaticEnv()

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found; ignore error if desired
			log.Error(err)
		} else {
			// Config file was found but another error was produced
			c7n_utils.CheckErrAndExit(err, 1)
		}
	}
	log.WithField("config", viper.ConfigFileUsed()).Info("using configuration file")
	err := viper.Unmarshal(&config.Cfg)
	c7n_utils.CheckErrAndExit(err, 1)
}