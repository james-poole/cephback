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

var cephUser string
var debug bool
var rbdSnapAgeMin int
var rbdSnapAgeMax int
var checkRbdInterval int
var imageExclude []string
var httpListen string
var cephfsMount string
var backupMount string
var checkCephfsInterval int
var rsyncCephfsInterval int
var cephfsRsyncLock string
var cephfsRsyncArgs []string
var cephfsSuccessFile string
var cephfsRbdName string
var cephfsSnapAgeMin int
var cephfsSnapAgeMax int

var rbdThresholdMin time.Duration
var rbdThresholdMax time.Duration
var cephfsThresholdMin time.Duration
var cephfsThresholdMax time.Duration
var logger = logrus.New()

var RootCmd = &cobra.Command{
	Use:   "cephback",
	Short: "A service to snapshot RBD's and backup files via rsync",
	PreRun: func(cmd *cobra.Command, args []string) {
		viper.BindPFlags(cmd.PersistentFlags())
		setConfigVars()
		out, _ := os.OpenFile("/var/log/cephback.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		multi := io.MultiWriter(out, os.Stderr)
		logger.Out = multi
		logger.Formatter = &logrus.TextFormatter{}
		if debug {
			logger.Level = logrus.DebugLevel
		} else {
			logger.Level = logrus.InfoLevel
		}

		logger.Infof("Called with arguments %s", os.Args)

	},
	Run: func(cmd *cobra.Command, args []string) {

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
		imageTicker := time.NewTicker(checkRbdIntervalMinutes)
		go func() {
			processImages()
			for _ = range imageTicker.C {
				processImages()
			}
		}()

		// start the cephfs routine
		logger.Info("Starting CephFS routine")
		cephfsTicker := time.NewTicker(checkCephfsIntervalMinutes)
		go func() {
			processCephFS()
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

	// replace all below with non-var types (except cfgFile)
	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/cephback.yaml)")
	RootCmd.PersistentFlags().StringP("ceph-user", "u", "admin", "Ceph user")
	RootCmd.PersistentFlags().BoolP("debug", "d", false, "Enable debugging")
	RootCmd.PersistentFlags().Int("rbd-snap-age-min", 24, "Duration in hours since the last snapshot before we take another one")
	RootCmd.PersistentFlags().Int("rbd-snap-age-max", 168, "Snapshots older in hours than this will be deleted")
	RootCmd.PersistentFlags().Int("rbd-interval", 60, "Interval in minutes between RBD snapshot checks")
	RootCmd.PersistentFlags().StringSlice("exclude", []string{}, "Images to exclude from processing")
	RootCmd.PersistentFlags().StringP("listen", "l", ":9090", "Port/IP to listen on")
	RootCmd.PersistentFlags().String("cephfs-mount", "/cephfs", "Mountpoint for cephfs")
	RootCmd.PersistentFlags().String("backup-mount", "/backup", "Mountpoint for backup destination")
	RootCmd.PersistentFlags().Int("cephfs-interval", 60, "Interval in minutes between CephFS RBD snapshot checks")
	RootCmd.PersistentFlags().Int("cephfs-rsync-interval", 24, "Interval in hours between CephFS rsyncs")
	RootCmd.PersistentFlags().String("cephfs-rsync-lock", "/backup/rsync.lock", "Path to lock file for CephFS rsync")
	RootCmd.PersistentFlags().StringSlice("cephfs-rsync-args", []string{"-ah", "--delete", "--delete-excluded"}, "Rsync args for the cephfs backup")
	RootCmd.PersistentFlags().String("cephfs-success-file", "/backup/rsync_success", "Path to CephFS rsync success file")
	RootCmd.PersistentFlags().String("cephfs-rbd-name", "cephfs_backup", "RBD name that CephFS is backed up to")
	RootCmd.PersistentFlags().Int("cephfs-snap-age-min", 24, "Duration in hours since the last snapshot before we take another one")
	RootCmd.PersistentFlags().Int("cephfs-snap-age-max", 168, "Snapshots older in hours than this will be deleted")

	//	Replaced with BindPFlags call in PreRun
	//	viper.BindPFlag("user", RootCmd.PersistentFlags().Lookup("user"))
	//	viper.BindPFlag("debug", RootCmd.PersistentFlags().Lookup("debug"))
	//	viper.BindPFlag("rbd-snap-age-min", RootCmd.PersistentFlags().Lookup("rbd-snap-age-min"))
	//	viper.BindPFlag("rbd-snap-age-max", RootCmd.PersistentFlags().Lookup("rbd-snap-age-max"))
	//	viper.BindPFlag("rbd-interval", RootCmd.PersistentFlags().Lookup("rbd-interval"))
	//	viper.BindPFlag("exclude", RootCmd.PersistentFlags().Lookup("exclude"))
	//	viper.BindPFlag("listen", RootCmd.PersistentFlags().Lookup("listen"))
	//	viper.BindPFlag("cephfs-mount", RootCmd.PersistentFlags().Lookup("cephfs-mount"))
	//	viper.BindPFlag("backup-mount", RootCmd.PersistentFlags().Lookup("backup-mount"))
	//	viper.BindPFlag("cephfs-interval", RootCmd.PersistentFlags().Lookup("cephfs-interval"))
	//	viper.BindPFlag("cephfs-rsync-interval", RootCmd.PersistentFlags().Lookup("cephfs-rsync-interval"))
	//	viper.BindPFlag("cephfs-rsync-lock", RootCmd.PersistentFlags().Lookup("cephfs-rsync-lock"))
	//	viper.BindPFlag("cephfs-rsync-args", RootCmd.PersistentFlags().Lookup("cephfs-rsync-args"))
	//	viper.BindPFlag("cephfs-success-file", RootCmd.PersistentFlags().Lookup("cephfs-success-file"))
	//	viper.BindPFlag("cephfs-rbd-name", RootCmd.PersistentFlags().Lookup("cephfs-rbd-name"))
	//	viper.BindPFlag("cephfs-snap-age-min", RootCmd.PersistentFlags().Lookup("cephfs-snap-age-min"))
	//	viper.BindPFlag("cephfs-snap-age-max", RootCmd.PersistentFlags().Lookup("cephfs-snap-age-max"))
}

func setConfigVars() {

	cephUser = viper.GetString("ceph-user")
	debug = viper.GetBool("debug")
	rbdSnapAgeMin = viper.GetInt("rbd-snap-age-min")
	rbdSnapAgeMax = viper.GetInt("rbd-snap-age-max")
	checkRbdInterval = viper.GetInt("rbd-interval")
	imageExclude = viper.GetStringSlice("exclude")
	httpListen = viper.GetString("listen")
	cephfsMount = viper.GetString("cephfs-mount")
	backupMount = viper.GetString("backup-mount")
	checkCephfsInterval = viper.GetInt("cephfs-interval")
	rsyncCephfsInterval = viper.GetInt("cephfs-rsync-interval")
	cephfsRsyncLock = viper.GetString("cephfs-rsync-lock")
	cephfsRsyncArgs = viper.GetStringSlice("cephfs-rsync-args")
	cephfsSuccessFile = viper.GetString("cephfs-success-file")
	cephfsRbdName = viper.GetString("cephfs-rbd-name")
	cephfsSnapAgeMin = viper.GetInt("cephfs-snap-age-min")
	cephfsSnapAgeMax = viper.GetInt("cephfs-snap-age-max")

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
