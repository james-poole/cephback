package cmd

import (
	"bytes"
	"github.com/ceph/go-ceph/rbd"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// format for snapshot - used to parse into an actual time
var layout = "2006-01-02_15:04"

func execHelper(command string, cmdArgs []string) (result bool) {

	cmd := exec.Command(command, cmdArgs...)

	// Set output to Byte Buffers
	var outb, errb bytes.Buffer
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
		logger.Errorf("command %s returned an error: %s", command, err)
		result = false
	} else {
		logger.Errorf("command %s exited successfully", command)
		result = true
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
	defer img.Close()
	snaps, err := img.GetSnapshotNames()
	if err != nil {
		logger.Errorf("Error getting snapshots for image %s: %s", cephfsRbdName, err)
	}
	return snaps
}

// returns true if a snapshot is created
func createSnap(imageName string, younger_than time.Duration, freeze bool) bool {

	snaps := getSnapshots(imageName)

	needs_snap := true
	if len(snaps) != 0 {
		for s := range snaps {
			snap := snaps[s]
			if matchSnapName(snap.Name, rbdSnapshotRegex) {
				t, err := time.Parse(layout, snap.Name)
				if err == nil {
					if time.Since(t) <= younger_than {
						needs_snap = false
					}
				}
			}
		}
	}
	if needs_snap {
		snapName := time.Now().Format(layout)
		logger.Infof("Creating snapshot %s@%s", imageName, snapName)

		if freeze {
			if execHelper("fsfreeze", []string{"-f", backupMount}) {
			}
		}

		img := rbd.GetImage(iocx, imageName)
		img.Open()
		_, err := img.CreateSnapshot(snapName)
		defer img.Close()

		if freeze {
			execHelper("fsfreeze", []string{"-u", backupMount})
		}

		if err != nil {
			logger.Errorf("Error creating snapshot %s@%s: %s", imageName, snapName, err)
			return false
		}
		return true
	} else {
		return false
	}
}

// returns the number of deleted snapshots
func deleteSnap(imageName string, older_than time.Duration) bool {

	snaps := getSnapshots(imageName)

	if len(snaps) == 0 {
		return false
	}

	for s := range snaps {
		snap := snaps[s]
		if matchSnapName(snap.Name, rbdSnapshotRegex) {
			t, err := time.Parse(layout, snap.Name)
			if err == nil {
				if time.Since(t) > older_than {
					img := rbd.GetImage(iocx, imageName)
					img.Open()
					defer img.Close()
					s := img.GetSnapshot(snap.Name)
					protected, err := s.IsProtected()
					if err != nil {
						logger.Errorf("Error checking if snapshot is protected %s@%s: %s", imageName, snap.Name, err)
					}
					if protected {
						logger.Errorf("Cannot delete protected snapshot %s@%s", imageName, snap.Name)
					} else {
						err = s.Remove()
						if err == nil {
							logger.Infof("Deleting snapshot %s@%s", imageName, snap.Name)
							return true
						} else {
							logger.Errorf("Error deleting snapshot %s@%s: %s", imageName, snap.Name, err)
						}
					}
				}
			}
		}
	}
	return false
}
