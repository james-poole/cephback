package cmd

import (
	"github.com/ceph/go-ceph/rbd"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	metricRBDSnapshotsCreated = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cephback_rbd_snapshots_created",
			Help: "The number of snapshots created",
		},
	)
	metricRBDSnapshotsDeleted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cephback_rbd_snapshots_deleted",
			Help: "The number of snapshots deleted",
		},
	)
	metricRBDImagesChecked = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "cephback_rbd_images_checked",
			Help: "The number of images checked",
		},
	)
)

func init() {
	prometheus.MustRegister(metricRBDSnapshotsCreated)
	prometheus.MustRegister(metricRBDSnapshotsDeleted)
	prometheus.MustRegister(metricRBDImagesChecked)
}

var rbdSnapshotRegex = "[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2}"

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

		if createSnap(imageName, rbdThresholdMin, false) {
			metricRBDSnapshotsCreated.Inc()
		}

		if deleteSnap(imageName, rbdThresholdMax) {
			metricRBDSnapshotsDeleted.Inc()
		}

		metricRBDImagesChecked.Inc()
	}

}
