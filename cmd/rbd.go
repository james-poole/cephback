package cmd

import (
	"github.com/ceph/go-ceph/rbd"
	"github.com/prometheus/client_golang/prometheus"
	"regexp"
	"time"
)

// format for snapshot - used to parse into an actual time
var layout = "2006-01-02_15:04"

var (
	metricSnapshotsCreated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "snapshots_created",
			Help: "The number of RBD snapshots created",
		},
	)
	metricSnapshotsDeleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "snapshots_deleted",
			Help: "The number of RBD snapshots deleted",
		},
	)
	metricImagesChecked = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "images_checked",
			Help: "The number of rbd images checked",
		},
	)
)

func init() {
	prometheus.MustRegister(metricSnapshotsCreated)
	prometheus.MustRegister(metricSnapshotsDeleted)
	prometheus.MustRegister(metricImagesChecked)
}

func matchSnapName(name string) bool {
	match, _ := regexp.MatchString("[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2}", name)
	return match
}

func excludeImages(images []string) []string {
	for x := range imageExclude {
		i := 0
		l := len(images)
		for i < l {
			if images[i] == imageExclude[x] {
				images = append(images[:i], images[i+1:]...)
				l--
			} else {
				i++
			}
		}
		images = images[:i]
	}
	return images
}

func processImages() {

	BackInit()
	// Get images - should probably get the list from openshift bound pv's instead of all rbd's
	images, err := rbd.GetImageNames(iocx)
	images = excludeImages(images)
	if err != nil {
		logger.Fatal("Error getting image names.", err)
	}

	logger.Info("Processing images")

	for i := range images {
		imageName := images[i]
		logger.Debug("Processing image:", imageName)
		// TESTING - REMOVE THIS
		match, _ := regexp.MatchString("jetest.*", imageName)
		if !match {
			continue
		}
		// TESTING - REMOVE THIS
		img := rbd.GetImage(iocx, imageName)
		img.Open()
		snaps, err := img.GetSnapshotNames()
		if err != nil {
			logger.Errorf("Error getting snapshots for image %s: %s", imageName, err)
		}

		metricSnapshotsCreated.Add(float64(createSnaps(imageName, img, snaps)))
		metricSnapshotsDeleted.Add(float64(deleteSnaps(imageName, img, snaps)))
		img.Close()
		metricImagesChecked.Inc()
	}

}

// returns the number of created snapshots
func createSnaps(imageName string, img *rbd.Image, snaps []rbd.SnapInfo) int {
	snapsCreated := 0

	needs_snap := true
	if len(snaps) != 0 {
		for s := range snaps {
			snap := snaps[s]
			if matchSnapName(snap.Name) {
				t, err := time.Parse(layout, snap.Name)
				if err == nil {
					if time.Since(t) <= thresholdMin {
						needs_snap = false
					}
				}
			}
		}
	}
	if needs_snap {
		snapName := time.Now().Format(layout)
		logger.Infof("Creating snapshot %s@%s", imageName, snapName)
		_, err := img.CreateSnapshot(snapName)
		if err == nil {
			snapsCreated++
		} else {
			logger.Errorf("Error creating snapshot %s@%s: %s", imageName, snapName, err)
		}
	}
	return snapsCreated
}

// returns the number of deleted snapshots
func deleteSnaps(imageName string, img *rbd.Image, snaps []rbd.SnapInfo) int {

	snapsDeleted := 0

	if len(snaps) == 0 {
		return 0
	}

	for s := range snaps {
		snap := snaps[s]
		if matchSnapName(snap.Name) {
			t, err := time.Parse(layout, snap.Name)
			if err == nil {
				if time.Since(t) > thresholdMax {
					s := img.GetSnapshot(snap.Name)
					protected, err := s.IsProtected()
					if err != nil {
						logger.Errorf("Error checking if snapshot is protected %s@%s: %s", imageName, snap.Name, err)
						continue
					}
					if protected {
						logger.Errorf("Cannot delete protected snapshot %s@%s", imageName, snap.Name)
					} else {
						err = s.Remove()
						if err == nil {
							snapsDeleted++
							logger.Infof("Deleting snapshot %s@%s", imageName, snap.Name)
						} else {
							logger.Errorf("Error deleting snapshot %s@%s: %s", imageName, snap.Name, err)
						}
					}
				}
			}
		}
	}
	return snapsDeleted
}
