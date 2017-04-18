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

var RootCmd = &cobra.Command{
	Use:   "cephback",
	Short: "A service to snapshot RBD's and backup files via rsync",

	Run: func(cmd *cobra.Command, args []string) {

		out, _ := os.OpenFile("/var/log/cephback.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
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

		if err = CephConnInit(); err != nil {
			logger.Error(err.Error())
			return
		}

		// remove the cephfs rbd from the list - we'll handle this separately
		imageExclude = append(imageExclude, cephfsRbdName)
		// start the rbd routine
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
		logger.Error(err.Error())
		os.Exit(-1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cephback.yaml)")
	RootCmd.PersistentFlags().StringVarP(&user, "user", "u", "admin", "Ceph user")
	RootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debugging")
	RootCmd.PersistentFlags().IntVar(&rbdSnapAgeMin, "rbd-snap-age-min", 6, "Duration in hours since the last snapshot before we take another one")
	RootCmd.PersistentFlags().IntVar(&rbdSnapAgeMax, "rbd-snap-age-max", 168, "Snapshots older in hours than this will be deleted")
	RootCmd.PersistentFlags().IntVar(&checkRbdInterval, "rbd-interval", 60, "Interval in minutes between RBD snapshot checks")
	RootCmd.PersistentFlags().StringSliceVar(&imageExclude, "exclude", []string{}, "Images to exclude from processing")
	RootCmd.PersistentFlags().StringVarP(&httpListen, "listen", "l", ":9090", "Port/IP to listen on")
	RootCmd.PersistentFlags().StringVar(&cephfsMount, "cephfs-mount", "/cephfs", "Mountpoint for cephfs")
	RootCmd.PersistentFlags().StringVar(&backupMount, "backup-mount", "/backup", "Mountpoint for backup destination")
	RootCmd.PersistentFlags().IntVar(&checkCephfsInterval, "cephfs-interval", 60, "Interval in minutes between CephFS RBD snapshot checks")
	RootCmd.PersistentFlags().IntVar(&rsyncCephfsInterval, "cephfs-rsync-interval", 1, "Interval between CephFS rsyncs")
	RootCmd.PersistentFlags().StringVar(&cephfsRsyncLock, "cephfs-rsync-lock", "/backup/rsync.lock", "Path to lock file for CephFS rsync")
	RootCmd.PersistentFlags().StringVar(&cephfsSuccessFile, "cephfs-success-file", "/backup/rsync_success", "Path to CephFS rsync success file")
	RootCmd.PersistentFlags().StringVar(&cephfsRbdName, "cephfs-rbd-name", "cephfs_backup", "RBD name that CephFS is backed up to")
	RootCmd.PersistentFlags().IntVar(&cephfsSnapAgeMax, "cephfs-snap-age-max", 168, "Snapshots older in hours than this will be deleted")

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
	viper.BindPFlag("cephfs-snap-age-max", RootCmd.PersistentFlags().Lookup("cephfs-snap-age-max"))

}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" { // enable ability to specify config file via flag
		viper.SetConfigFile(cfgFile)
	}

	viper.SetConfigName("cephback")
	viper.AddConfigPath("$HOME")
	viper.AddConfigPath("/etc/cephback")
	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		logger.Info("Using config file:", viper.ConfigFileUsed())
	}
}
