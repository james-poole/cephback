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
var rbdSnapAgeMin int
var rbdSnapAgeMax int
var cephfsSnapAgeMin int
var cephfsSnapAgeMax int
var rbdThresholdMin time.Duration
var rbdThresholdMax time.Duration
var cephfsThresholdMin time.Duration
var cephfsThresholdMax time.Duration
var debug bool
var imageExclude []string
var logger = logrus.New()
var checkRbdInterval int
var checkCephfsInterval int
var rsyncCephfsInterval int
var httpListen string
var cephfsMount string
var backupMount string
var cephfsSuccessFile string
var cephfsRbdName string
var cephfsRsyncLock string

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

		rbdThresholdMin = time.Duration(rbdSnapAgeMin) * time.Hour
		rbdThresholdMax = time.Duration(rbdSnapAgeMax) * time.Hour
		cephfsThresholdMin = time.Duration(cephfsSnapAgeMin) * time.Hour
		cephfsThresholdMax = time.Duration(cephfsSnapAgeMax) * time.Hour
		checkRbdIntervalMinutes := time.Duration(checkRbdInterval) * time.Minute
		checkCephfsIntervalMinutes := time.Duration(checkCephfsInterval) * time.Minute

		httpServe()

		// remove the cephfs rbd from the list - we'll handle this separately
		BackInit()

		// start the rbd routine
		imageExclude = append(imageExclude, cephfsRbdName)
		logger.Info("Starting RBD routine")
		processImages()
		imageTicker := time.NewTicker(checkRbdIntervalMinutes)
		go func() {
			for _ = range imageTicker.C {
				processImages()
			}
		}()

		// start the cephfs routine
		logger.Info("Starting CephFS routine")
		processCephFS()
		cephfsTicker := time.NewTicker(checkCephfsIntervalMinutes)
		go func() {
			for _ = range cephfsTicker.C {
				processCephFS()
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
	RootCmd.PersistentFlags().StringVarP(&user, "user", "u", "osrbd", "Ceph user")
	RootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debugging")
	RootCmd.PersistentFlags().IntVar(&rbdSnapAgeMin, "rbd-snap-age-min", 5, "Duration since the last snapshot before we take another one (default 5 hours)")
	RootCmd.PersistentFlags().IntVar(&rbdSnapAgeMax, "rbd-snap-age-max", 168, "Snapshots older than this will be deleted (default 168 hours - 7 days)")
	RootCmd.PersistentFlags().IntVar(&checkRbdInterval, "rbd-interval", 5, "Interval between RBD snapshot checks (default 5 minutes)")
	RootCmd.PersistentFlags().StringSliceVar(&imageExclude, "exclude", []string{}, "Images to exclude from processing")
	RootCmd.PersistentFlags().StringVarP(&httpListen, "listen", "l", ":9090", "Port/IP to listen on (default :9090)")
	RootCmd.PersistentFlags().StringVar(&cephfsMount, "cephfs-mount", "/storage/cephfs", "Mountpoint for cephfs (default /cephfs)")
	RootCmd.PersistentFlags().StringVar(&backupMount, "backup-mount", "/backup", "Mountpoint for backup destination (default /backup)")
	RootCmd.PersistentFlags().IntVar(&checkCephfsInterval, "cephfs-interval", 5, "Interval between CephFS RBD snapshot checks (default 5 minutes)")
	RootCmd.PersistentFlags().IntVar(&rsyncCephfsInterval, "cephfs-rsync-interval", 1, "Interval between CephFS rsyncs (default 1 hour)")
	RootCmd.PersistentFlags().StringVar(&cephfsRsyncLock, "cephfs-rsync-lock", "/backup/rsync.lock", "Path to lock file for CephFS rsync (default /backup/rsync.lock)")
	RootCmd.PersistentFlags().StringVar(&cephfsSuccessFile, "cephfs-success-file", "/backup/rsync_success", "Path to CephFS rsync success file (default /backup/rsync_success)")
	RootCmd.PersistentFlags().StringVar(&cephfsRbdName, "cephfs-rbd-name", "cephfs_backup", "RBD name that CephFS is backed up to (default cephfs_backup)")
	RootCmd.PersistentFlags().IntVar(&cephfsSnapAgeMin, "cephfs-snap-age-min", 5, "Duration since the last snapshot of the CephFS backup rbd before we take another one (default 5 hours)")
	RootCmd.PersistentFlags().IntVar(&cephfsSnapAgeMax, "cephfs-snap-age-max", 168, "Snapshots older than this will be deleted (default 168 hours - 7 days)")

	viper.BindPFlag("user", RootCmd.PersistentFlags().Lookup("user"))
	viper.BindPFlag("debug", RootCmd.PersistentFlags().Lookup("debug"))
	viper.BindPFlag("rbd-snap-age-min", RootCmd.PersistentFlags().Lookup("rbd-snap-age-min"))
	viper.BindPFlag("rbd-snap-age-max", RootCmd.PersistentFlags().Lookup("rbd-snap-age-max"))
	viper.BindPFlag("rbd-interval", RootCmd.PersistentFlags().Lookup("rbd-interval"))
	viper.BindPFlag("exclude", RootCmd.PersistentFlags().Lookup("exclude"))
	viper.BindPFlag("listen", RootCmd.PersistentFlags().Lookup("listen"))
	viper.BindPFlag("cephfs-mount", RootCmd.PersistentFlags().Lookup("cephfs-mount"))
	viper.BindPFlag("backup-mount", RootCmd.PersistentFlags().Lookup("backup-mount"))
	viper.BindPFlag("cephfs-interval", RootCmd.PersistentFlags().Lookup("cephfs-interval"))
	viper.BindPFlag("cephfs-rsync-interval", RootCmd.PersistentFlags().Lookup("cephfs-rsync-interval"))
	viper.BindPFlag("cephfs-rsync-lock", RootCmd.PersistentFlags().Lookup("cephfs-rsync-lock"))
	viper.BindPFlag("cephfs-success-file", RootCmd.PersistentFlags().Lookup("cephfs-success-file"))
	viper.BindPFlag("cephfs-rbd-name", RootCmd.PersistentFlags().Lookup("cephfs-rbd-name"))
	viper.BindPFlag("cephfs-snap-age-min", RootCmd.PersistentFlags().Lookup("cephfs-snap-age-min"))
	viper.BindPFlag("cephfs-snap-age-max", RootCmd.PersistentFlags().Lookup("cephfs-snap-age-max"))

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
