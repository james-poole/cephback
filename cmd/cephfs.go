package cmd

import (
	"fmt"
	"github.com/alexflint/go-filemutex"
	"github.com/prometheus/client_golang/prometheus"
	"io/ioutil"
	"os"
	"regexp"
	"time"
)

var cephFSSnapshotRegex = "cephfs_[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2}"
var cephFSLastSuccess time.Time
var rsyncCephfsIntervalHours time.Duration
var rsyncLogFileFormat = "2006-01-02_15:04"

var (
	metricCephFSSnapshotsCreated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cephback_cephfs_snapshots_created",
			Help: "The number of snapshots created",
		},
	)
	metricCephFSSnapshotsDeleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cephback_cephfs_snapshots_deleted",
			Help: "The number of snapshots deleted",
		},
	)
	metricRsyncPerformed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cephback_cephfs_rsync_performed",
			Help: "How many rsyncs we have performed",
		},
	)
)

func init() {
	prometheus.MustRegister(metricCephFSSnapshotsCreated)
	prometheus.MustRegister(metricCephFSSnapshotsDeleted)
	prometheus.MustRegister(metricRsyncPerformed)
}

func pruneRsyncLogs() bool {
	// iterate through rsync logs and remove older than 7 days

	files, err := ioutil.ReadDir(backupMount)
	if err != nil {
		logger.Errorf("Error reading %s directory:%s", backupMount, err.Error())
		return false
	}

	for _, file := range files {
		re := regexp.MustCompile("rsync_([0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2})\\.log")
		timestamp := re.FindStringSubmatch(file.Name())
		if timestamp == nil {
			continue
		}
		age, err := time.Parse(layout, timestamp[1])
		if err != nil {
			logger.Errorf("Error parsing rsync log file timestamp %s: %s", timestamp[1], err.Error())
			continue
		}

		if time.Since(age) > cephfsThresholdMax {
			filepath := fmt.Sprintf("%s/%s", backupMount, file.Name())
			err = os.Remove(filepath)
			if err == nil {
				logger.Infof("Deleted log file %s", filepath)
			} else {
				logger.Errorf("Error deleting log file %s", filepath)
			}
		}
	}
	return true
}

func processCephFS() bool {

	CephConnInit()
	var bail bool = false

	cephfsMounted, err := mounted(cephfsMount)
	if err != nil {
		logger.Error("CephFS mount check error:", err.Error())
		bail = true
	}
	if !cephfsMounted {
		logger.Errorf("CephFS not mounted at %s", cephfsMount)
		bail = true
	}

	backupMounted, err := mounted(backupMount)
	if err != nil {
		logger.Error("Backup mount check error:", err.Error())
		bail = true
	}
	if !backupMounted {
		logger.Errorf("CephFS not mounted at %s", backupMount)
		bail = true
	}

	if bail {
		logger.Error("CephFS backup process failed due to mount errors")
		return false
	}

	// look for last rsync success file timestamp /backup/last_success

	rsyncCephfsIntervalHours = time.Duration(rsyncCephfsInterval) * time.Hour
	if successFile, err := os.Stat(cephfsSuccessFile); err == nil {
		cephFSLastSuccess = successFile.ModTime()
	} else {
		cephFSLastSuccess = time.Time{} // epoch 0
	}

	if time.Since(cephFSLastSuccess) > rsyncCephfsIntervalHours {
		m, err := filemutex.New(cephfsRsyncLock)
		if err != nil {
			logger.Error("Rsync lock file could not created")
		}

		m.Lock() // Will block until lock can be acquired - should consider whether to use the non-blocking method

		logFileName := fmt.Sprintf("%s/rsync_%s.log", backupMount, time.Now().Format(rsyncLogFileFormat))

		var cmdArgs []string
		cmdArgs = append(cmdArgs, cephfsRsyncArgs...)
		cmdArgs = append(cmdArgs, []string{fmt.Sprintf("--log-file=%s", logFileName),
			fmt.Sprintf("%s/", cephfsMount),
			fmt.Sprintf("%s/backup/", backupMount),
		}...)

		if execHelper("rsync", cmdArgs) {
			// touch success file
			var _, err = os.Stat(cephfsSuccessFile)
			if os.IsNotExist(err) {
				var file, err = os.Create(cephfsSuccessFile)
				if err != nil {
					logger.Error("Rsync success file could not be created", err.Error())
				}
				file.Close()
			} else {
				now := time.Now()
				if err := os.Chtimes(cephfsSuccessFile, now, now); err != nil {
					logger.Error("There was an error updating the rsync success file timestamp:", err.Error())
				}
			}
		}

		m.Unlock()
	}

	if createSnap(cephfsRbdName, cephfsThresholdMin, true) {
		metricCephFSSnapshotsCreated.Inc()
	}

	if deleteSnap(cephfsRbdName, cephfsThresholdMax) {
		metricCephFSSnapshotsDeleted.Inc()
	}

	pruneRsyncLogs()

	return true
}
