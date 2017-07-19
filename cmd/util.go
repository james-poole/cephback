package cmd

import (
	"bytes"
	"fmt"
	"github.com/ceph/go-ceph/rbd"
	"github.com/prometheus/client_golang/prometheus"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

var (
	metricHealth = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "cephback_health",
			Help: "0 if everything is good",
		},
	)
)

func init() {
	prometheus.MustRegister(metricHealth)
}

// format for snapshot - used to parse into an actual time
var layout = "2006-01-02_15:04"

var health HealthStatus

type HealthStatus struct {
	RBD    string
	CephFS string
}

func (h *HealthStatus) Status() string {
	if (h.RBD == "") && (h.CephFS == "") {
		return "OK"
	} else {
		return h.RBD + " " + h.CephFS
	}
}

type DiskStatus struct {
	All  uint64 `json:"all"`
	Used uint64 `json:"used"`
	Free uint64 `json:"free"`
}

func DiskUsage(path string) (disk DiskStatus) {
	fs := syscall.Statfs_t{}
	err := syscall.Statfs(path, &fs)
	if err != nil {
		return
	}
	disk.All = fs.Blocks * uint64(fs.Bsize)
	disk.Free = fs.Bfree * uint64(fs.Bsize)
	disk.Used = disk.All - disk.Free
	return
}

func execHelper(command string, cmdArgs []string, validExitCodes []int) (result bool) {

	var outb, errb bytes.Buffer
	var exitCode int

	cmd := exec.Command(command, cmdArgs...)

	cmd.Stdout = &outb
	cmd.Stderr = &errb

	logger.Infof("Running command %s %s", command, strings.Join(cmdArgs, " "))
	err = cmd.Run()

	stdout := strings.Split(outb.String(), "\n")
	stderr := strings.Split(errb.String(), "\n")

	for l := range stdout {
		if strings.TrimSpace(stdout[l]) != "" {
			logger.Infof("command %s stdout: %s", command, stdout[l])
		}
	}
	for l := range stderr {
		if strings.TrimSpace(stderr[l]) != "" {
			logger.Infof("command %s stderr: %s", command, stderr[l])
		}
	}

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			ws := exitError.Sys().(syscall.WaitStatus)
			exitCode = ws.ExitStatus()
		}
	} else {
		// success, exitCode should be 0 if go is ok
		ws := cmd.ProcessState.Sys().(syscall.WaitStatus)
		exitCode = ws.ExitStatus()
		logger.Infof("command %s exited successfully", command)
	}

	result = false
	for c := range validExitCodes {
		if exitCode == validExitCodes[c] {
			result = true
			break
		}
	}

	if result == false {
		logger.Errorf("command %s returned an error: %s", command, err.Error())
	}
	return result
}

func mounted(mountpoint string) (bool, error) {
	mntpoint, err := os.Stat(mountpoint)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	parent, err := os.Stat(filepath.Join(mountpoint, ".."))
	if err != nil {
		return false, err
	}
	mntpointSt := mntpoint.Sys().(*syscall.Stat_t)
	parentSt := parent.Sys().(*syscall.Stat_t)
	return mntpointSt.Dev != parentSt.Dev, nil
}

func matchSnapName(name string, regex string) bool {
	match, _ := regexp.MatchString(regex, name)
	return match
}

func getSnapshots(imageName string) (snaps []rbd.SnapInfo) {
	img := rbd.GetImage(iocx, imageName)
	img.Open()
	snaps, err := img.GetSnapshotNames()
	if err != nil {
		logger.Errorf("Error getting snapshots for image %s: %s", cephfsRbdName, err.Error())
	}
	img.Close()
	return snaps
}

func checkHealth() {
	cephfsSnapAgeHealthThreshold := time.Duration(cephfsSnapAgeMin * 120 / 100) // add 20%
	if !checkSnapshotHealth(cephfsRbdName, cephfsSnapAgeHealthThreshold) {
		msg := fmt.Sprintf("Snapshot within %s not found for CephFS RBD", cephfsSnapAgeHealthThreshold)
		health.CephFS = msg
		logger.Infof(msg)
	} else {
		health.CephFS = ""
	}

	// Need to add something here to check rsync_success timestamp

	rbdSnapAgeHealthThreshold := time.Duration(rbdSnapAgeMin * 120 / 100) // add 20%
	healthy, unhealthyImages := checkRbdImagesSnapHealth(rbdSnapAgeHealthThreshold)
	if healthy {
		health.RBD = ""
		metricHealth.Set(0)
	} else {
		msg := fmt.Sprintf("Snapshots within %s not found for %d RBD images: %s", rbdSnapAgeHealthThreshold, len(unhealthyImages), strings.Join(unhealthyImages, " "))
		health.RBD = msg
		logger.Infof(msg)
		metricHealth.Set(1)
	}
}

// Given an rbd image name and a time duration, this function returns true if a snapshot exists within (now-duration)
func checkSnapshotHealth(imageName string, youngerThan time.Duration) bool {
	snaps := getSnapshots(imageName)

	for s := range snaps {
		if matchSnapName(snaps[s].Name, rbdSnapshotRegex) {
			t, err := time.Parse(layout, snaps[s].Name)
			if err == nil {
				if time.Since(t) <= youngerThan {
					return true
				}
			}
		}
	}
	return false
}

func createSnap(imageName string, youngerThan time.Duration, freeze bool) int {

	snaps := getSnapshots(imageName)

	needsSnap := true
	if len(snaps) != 0 {
		for s := range snaps {
			snap := snaps[s]
			if matchSnapName(snap.Name, rbdSnapshotRegex) {
				t, err := time.Parse(layout, snap.Name)
				if err == nil {
					if time.Since(t) <= youngerThan {
						needsSnap = false
					}
				}
			}
		}
	}
	if needsSnap {
		snapName := time.Now().Format(layout)
		logger.Infof("Creating snapshot %s@%s", imageName, snapName)

		if freeze {
			if !execHelper("fsfreeze", []string{"-f", backupMount}, []int{0}) {
				return 0
			}
		}

		img := rbd.GetImage(iocx, imageName)
		img.Open()
		_, err := img.CreateSnapshot(snapName)
		defer img.Close()

		if freeze {
			if !execHelper("fsfreeze", []string{"-u", backupMount}, []int{0}) {
				return 0
			}
		}

		if err != nil {
			logger.Errorf("Error creating snapshot %s@%s: %s", imageName, snapName, err.Error())
			return 0
		}
		return 1
	}
	return 0
}

// temporary struct used to enable sorting of snapshots by name
type matchSnaps []rbd.SnapInfo

func (m matchSnaps) Len() int           { return len(m) }
func (m matchSnaps) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m matchSnaps) Less(i, j int) bool { return m[i].Name < m[j].Name }

// returns number of deleted snapshots
func deleteSnap(imageName string, olderThan time.Duration, minKeep int) (snapsDeleted int) {

	snapsDeleted = 0
	var matchingSnaps matchSnaps

	snaps := getSnapshots(imageName)

	for s := range snaps {
		snap := snaps[s]
		if matchSnapName(snap.Name, rbdSnapshotRegex) {
			matchingSnaps = append(matchingSnaps, snap)
		}
	}
	sort.Sort(matchingSnaps)

	matchingSnapCount := len(matchingSnaps)
	if len(matchingSnaps) <= minKeep {
		logger.Debugf("Skipping snapshot delete for image %s since matching snapshot count %d <= than minimum to keep setting %d", imageName, matchingSnapCount, minKeep)
		return snapsDeleted
	}

	for i := range matchingSnaps {
		snap := matchingSnaps[i]
		if matchingSnapCount <= minKeep {
			logger.Debugf("Cancelling snapshot delete for image %s since matching snapshot count %d <= minimum to keep setting %d", imageName, matchingSnapCount, minKeep)
			break
		}
		t, err := time.Parse(layout, snap.Name)
		if err == nil {
			logger.Debugf("Checking snapshots for image %s (looking for olderThan %s", imageName, olderThan)
			if time.Since(t) > olderThan {
				img := rbd.GetImage(iocx, imageName)
				img.Open()
				defer img.Close()
				s := img.GetSnapshot(snap.Name)
				protected, err := s.IsProtected()
				if err != nil {
					logger.Errorf("Error checking if snapshot is protected %s@%s: %s", imageName, snap.Name, err.Error())
				}
				if protected {
					logger.Errorf("Cannot delete protected snapshot %s@%s", imageName, snap.Name)
				} else {
					logger.Infof("Deleting snapshot %s@%s", imageName, snap.Name)
					err = s.Remove()
					if err == nil {
						snapsDeleted++
						matchingSnapCount--
					} else {
						logger.Errorf("Error deleting snapshot %s@%s: %s", imageName, snap.Name, err.Error())
					}
				}
			} else {
				logger.Debugf("Skipping. Snapshot %s@%s is not older than %s", imageName, snap.Name, olderThan)
			}
		}
	}
	return snapsDeleted
}

func purgeSnaps(imageName string) (snapsDeleted int) {

	snapsDeleted = 0

	snaps := getSnapshots(imageName)

	if len(snaps) == 0 {
		return snapsDeleted
	}

	logger.Infof("Purging all snapshots for %s", imageName)

	for i := range snaps {
		snap := snaps[i]
		img := rbd.GetImage(iocx, imageName)
		img.Open()
		defer img.Close()
		s := img.GetSnapshot(snap.Name)
		protected, err := s.IsProtected()
		if err != nil {
			logger.Errorf("Error checking if snapshot is protected %s@%s: %s", imageName, snap.Name, err.Error())
		}
		if protected {
			logger.Errorf("Cannot delete protected snapshot %s@%s", imageName, snap.Name)
		} else {
			err = s.Remove()
			if err == nil {
				logger.Infof("Deleting snapshot %s@%s", imageName, snap.Name)
				snapsDeleted++
			} else {
				logger.Errorf("Error deleting snapshot %s@%s: %s", imageName, snap.Name, err.Error())
			}
		}
	}
	return snapsDeleted
}
