// Copyright © 2017 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"github.com/Sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io"
	"os"
	"time"
)

var cfgFile string
var user string
var ageMin int
var ageMax int
var thresholdMin time.Duration
var thresholdMax time.Duration
var debug bool
var imageExclude []string
var logger = logrus.New()
var checkInterval int
var httpListen string

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "cephback",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Run: func(cmd *cobra.Command, args []string) {

		out, _ := os.OpenFile("cephback.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		multi := io.MultiWriter(out, os.Stderr)
		logger.Out = multi
		logger.Formatter = &logrus.TextFormatter{}
		if debug {
			logger.Level = logrus.DebugLevel
		} else {
			logger.Level = logrus.InfoLevel
		}

		thresholdMin = time.Duration(ageMin) * time.Hour
		thresholdMax = time.Duration(ageMax) * time.Hour
		checkIntervalMinutes := time.Duration(checkInterval) * time.Minute

		httpServe()

		BackInit()

		processImages()
		imageTicker := time.NewTicker(checkIntervalMinutes)
		go func() {
			for _ = range imageTicker.C {
				processImages()
			}
		}()

		// block forever
		select {}
	},
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		logger.Error(err)
		os.Exit(-1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports Persistent Flags, which, if defined here,
	// will be global for your application.

	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cephback.yaml)")
	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	RootCmd.PersistentFlags().StringVarP(&user, "user", "u", "admin", "Ceph user")
	RootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debugging")
	RootCmd.PersistentFlags().IntVarP(&ageMin, "agemin", "m", 5, "Duration since the last snapshot before we take another one (default 5 hours)")
	RootCmd.PersistentFlags().IntVarP(&ageMax, "agemax", "M", 168, "Snapshots older than this will be deleted (default 168 hours - 7 days)")
	RootCmd.PersistentFlags().IntVarP(&checkInterval, "interval", "i", 5, "Interval between snapshot checks (default 5 minutes)")
	RootCmd.PersistentFlags().StringSliceVar(&imageExclude, "exclude", []string{}, "Images to exclude from processing")
	RootCmd.PersistentFlags().StringVarP(&httpListen, "listen", "l", ":9090", "Port/IP to listen on (default :9090)")

	viper.BindPFlag("user", RootCmd.PersistentFlags().Lookup("user"))
	viper.BindPFlag("debug", RootCmd.PersistentFlags().Lookup("debug"))
	viper.BindPFlag("agemin", RootCmd.PersistentFlags().Lookup("agemin"))
	viper.BindPFlag("agemax", RootCmd.PersistentFlags().Lookup("agemax"))
	viper.BindPFlag("interval", RootCmd.PersistentFlags().Lookup("interval"))
	viper.BindPFlag("exclude", RootCmd.PersistentFlags().Lookup("exclude"))
	viper.BindPFlag("listen", RootCmd.PersistentFlags().Lookup("listen"))

}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" { // enable ability to specify config file via flag
		viper.SetConfigFile(cfgFile)
	}

	viper.SetConfigName(".cephback") // name of config file (without extension)
	viper.AddConfigPath("$HOME")     // adding home directory as first search path
	viper.AutomaticEnv()             // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		logger.Info("Using config file:", viper.ConfigFileUsed())
	}
}
