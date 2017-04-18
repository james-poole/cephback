package cmd

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
)

var (
	metricBoundPVFound = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "cephback_bound_pv_found",
			Help: "The number of bound persistent volumes found",
		},
	)
)

func getRbdPvImages() ([]string, error) {
	// creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	pv, err := clientset.Core().PersistentVolumes().List(v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var boundPVImages []string

	for x := range pv.Items {
		p := pv.Items[x]
		if p.Status.Phase == "Bound" {
			if p.Spec.PersistentVolumeSource.RBD != nil {
				boundPVImages = append(boundPVImages, p.Spec.PersistentVolumeSource.RBD.RBDImage)
			}
		}
	}
	logger.Infof("Found %d bound RBD persistent volumes in the cluster\n", len(boundPVImages))
	metricBoundPVFound.Set(float64(len(boundPVImages)))

	return boundPVImages, nil
}
