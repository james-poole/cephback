package cmd

import (
	//	"github.com/ceph/go-ceph/rbd"
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

var rbdSnapshotRegex = "[0-9]{4}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2}"

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

func excludeImages(images []string) []string {
	for x := range imageExclude {
		i := 0
		l := len(images)
		for i < l {
			if images[i] == imageExclude[x] {
				logger.Infof("Excluding image %s", images[i])
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

func purgeSnapsOnFailedPV() {

	// get a list of images for Failed phase pv's
	// for each of these, snap purge
	if err = CephConnInit(); err != nil {
		logger.Error(err.Error())
		return
	}
	images, err := getRbdPvImages("Failed")
	if err != nil {
		logger.Error(err.Error())
		return
	}

	logger.Infof("purgeSnaps - Processing %d images", len(images))

	for i := range images {
		imageName := images[i]
		logger.Debug("purgeSnaps - Processing image: ", imageName)

		purgeSnaps(imageName)
	}
}

func processImages() {

	if err = CephConnInit(); err != nil {
		logger.Error(err.Error())
		return
	}
	images, err := getBoundRbdPvImages()
	if err != nil {
		logger.Error(err.Error())
		return
	}
	images = excludeImages(images)

	logger.Infof("processImages - Processing %d images", len(images))

	for i := range images {
		imageName := images[i]
		logger.Debug("Processing image: ", imageName)

		metricRBDSnapshotsCreated.Add(float64(createSnap(imageName, rbdSnapAgeMin, false)))
		metricRBDSnapshotsDeleted.Add(float64(deleteSnap(imageName, rbdSnapAgeMax, rbdSnapCountMin)))

		metricRBDImagesChecked.Inc()
	}
}

// returns true if all images have a snapshot within the duration, false and a slice of unhealthy image names otherwise
func checkRbdImagesSnapHealth(youngerThan time.Duration) (healthy bool, imagesUnhealthy []string) {
	images, err := getBoundRbdPvImages()
	if err != nil {
		logger.Errorf("Error retrieving PV's. %s", err)
		return false, imagesUnhealthy
	}
	images = excludeImages(images)

	for i := range images {
		if !checkSnapshotHealth(images[i], youngerThan) {
			imagesUnhealthy = append(imagesUnhealthy, images[i])
			healthy = false
		}
	}

	if len(imagesUnhealthy) == 0 {
		healthy = true
	}

	return healthy, imagesUnhealthy
}
