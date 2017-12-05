package cmd

import (
	"github.com/Sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"io"
	"os"
	"strconv"
	"time"
)

var cfgFile string

var cephUser string
var debug bool
var rbdSnapCountMin int
var rbdSnapAgeMin time.Duration
var rbdSnapAgeMax time.Duration
var checkRbdInterval time.Duration
var checkPurgedInterval time.Duration
var healthCheckInterval time.Duration
var imageExclude []string
var httpListen string
var cephfsMount string
var backupMount string
var checkCephfsInterval time.Duration
var rsyncCephfsInterval time.Duration
var cephfsRsyncLock string
var cephfsRsyncArgs []string
var cephfsRsyncValidExitCodes []int
var cephfsSuccessFile string
var cephfsRbdName string
var cephfsSnapCountMin int
var cephfsSnapAgeMin time.Duration
var cephfsSnapAgeMax time.Duration

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

		httpServe()

		if err = CephConnInit(); err != nil {
			logger.Error(err.Error())
			return
		}

		// remove the cephfs rbd from the list - we'll handle this separately
		imageExclude = append(imageExclude, cephfsRbdName)
		// start the rbd routine
		logger.Infof("Starting RBD routine every %s", checkRbdInterval)
		imageTicker := time.NewTicker(checkRbdInterval)
		go func() {
			processImages()
			for _ = range imageTicker.C {
				processImages()
			}
		}()

		// start the failed pv routine - this is to handle Failed pv's - Openshift fails to delete the pv if the rbd has snapshots
		logger.Infof("Starting PVs Failed routine every %s", checkPurgedInterval)
		pvFailedTicker := time.NewTicker(checkPurgedInterval)
		go func() {
			purgeSnapsOnFailedPV()
			for _ = range pvFailedTicker.C {
				purgeSnapsOnFailedPV()
			}
		}()

		// start the cephfs routine
		logger.Infof("Starting CephFS routine every %s", checkCephfsInterval)
		cephfsTicker := time.NewTicker(checkCephfsInterval)
		go func() {
			processCephFS()
			for _ = range cephfsTicker.C {
				processCephFS()
			}
		}()

		// start the health check routine
		logger.Infof("Starting health check routine every %s", healthCheckInterval)
		healthTicker := time.NewTicker(healthCheckInterval)
		go func() {
			checkHealth()
			for _ = range healthTicker.C {
				checkHealth()
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
	RootCmd.PersistentFlags().Int("rbd-snap-count-min", 7, "The minimum number of RBD snapshots before we consider deleting older ones")
	RootCmd.PersistentFlags().String("rbd-snap-age-min", "24h", "Duration since the last snapshot before we take another one")
	RootCmd.PersistentFlags().String("rbd-snap-age-max", "168h", "Snapshots older than this will be deleted")
	RootCmd.PersistentFlags().String("rbd-interval", "15m", "Interval between RBD snapshot checks")
	RootCmd.PersistentFlags().String("purge-interval", "15m", "Interval between checks for snapshots to purge")
	RootCmd.PersistentFlags().String("healthcheck-interval", "15m", "Interval between snapshot healthchecks")
	RootCmd.PersistentFlags().StringSlice("exclude", []string{}, "Images to exclude from processing")
	RootCmd.PersistentFlags().StringP("listen", "l", ":9090", "Port/IP to listen on")
	RootCmd.PersistentFlags().String("cephfs-mount", "/cephfs", "Mountpoint for cephfs")
	RootCmd.PersistentFlags().String("backup-mount", "/backup", "Mountpoint for backup destination")
	RootCmd.PersistentFlags().String("cephfs-interval", "15m", "Interval between CephFS RBD snapshot checks")
	RootCmd.PersistentFlags().String("cephfs-rsync-interval", "24h", "Interval between CephFS rsyncs")
	RootCmd.PersistentFlags().String("cephfs-rsync-lock", "/backup/rsync.lock", "Path to lock file for CephFS rsync")
	RootCmd.PersistentFlags().StringSlice("cephfs-rsync-args", []string{"-ah", "--delete", "--delete-excluded"}, "Rsync args for the cephfs backup")
	RootCmd.PersistentFlags().StringSlice("cephfs-rsync-valid-exit-codes", []string{"0", "24"}, "Rsync valid exit codes for the cephfs backup")
	RootCmd.PersistentFlags().String("cephfs-success-file", "/backup/rsync_success", "Path to CephFS rsync success file")
	RootCmd.PersistentFlags().String("cephfs-rbd-name", "cephfs_backup", "RBD name that CephFS is backed up to")
	RootCmd.PersistentFlags().Int("cephfs-snap-count-min", 7, "The minimum number of CephFS RBD snapshots before we consider deleting older ones")
	RootCmd.PersistentFlags().String("cephfs-snap-age-min", "24h", "Duration since the last snapshot before we take another one")
	RootCmd.PersistentFlags().String("cephfs-snap-age-max", "168h", "Snapshots older than this will be deleted")
}

func durationSettingParser(t string) time.Duration {
	td, err := time.ParseDuration(viper.GetString(t))
	if err != nil {
		logger.Fatalf("Unable to parse '%s' setting: '%s'. %s", t, viper.GetString(t), err.Error())
	}
	return td
}

func setConfigVars() {

	cephUser = viper.GetString("ceph-user")
	debug = viper.GetBool("debug")
	rbdSnapCountMin = viper.GetInt("rbd-snap-count-min")
	rbdSnapAgeMin = durationSettingParser("rbd-snap-age-min")
	rbdSnapAgeMax = durationSettingParser("rbd-snap-age-max")
	checkRbdInterval = durationSettingParser("rbd-interval")
	checkPurgedInterval = durationSettingParser("purge-interval")
	healthCheckInterval = durationSettingParser("healthcheck-interval")
	imageExclude = viper.GetStringSlice("exclude")
	httpListen = viper.GetString("listen")
	cephfsMount = viper.GetString("cephfs-mount")
	backupMount = viper.GetString("backup-mount")
	checkCephfsInterval = durationSettingParser("cephfs-interval")
	rsyncCephfsInterval = durationSettingParser("cephfs-rsync-interval")
	cephfsRsyncLock = viper.GetString("cephfs-rsync-lock")
	cephfsRsyncArgs = viper.GetStringSlice("cephfs-rsync-args")
	// clear slice
	cephfsRsyncValidExitCodes = cephfsRsyncValidExitCodes[:0]
	ec := viper.GetStringSlice("cephfs-rsync-valid-exit-codes")
	for a := range ec {
		val, err := strconv.Atoi(ec[a])
		if err == nil {
			cephfsRsyncValidExitCodes = append(cephfsRsyncValidExitCodes, val)
		} else {
			logger.Errorf("Error processing cephfs-rsync-valid-exit-codes - unable to convert %s to int: %s", ec[a], err.Error())
		}
	}
	cephfsSuccessFile = viper.GetString("cephfs-success-file")
	cephfsRbdName = viper.GetString("cephfs-rbd-name")
	cephfsSnapCountMin = viper.GetInt("cephfs-snap-count-min")
	cephfsSnapAgeMin = durationSettingParser("cephfs-snap-age-min")
	cephfsSnapAgeMax = durationSettingParser("cephfs-snap-age-max")

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

	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		logger.Infof("Config file changed:", e.Name)
		setConfigVars()
	})
}
