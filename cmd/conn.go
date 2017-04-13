package cmd

import (
	"github.com/ceph/go-ceph/rados"
)

var conn *rados.Conn
var iocx *rados.IOContext
var err error

func BackInit() {
	if conn == nil {
		conn, err = rados.NewConnWithUser(user)
		logger.Info("Creating ceph connection")
		if err != nil {
			logger.Fatal("Error creating connection.", err)
		}
		logger.Info("Reading ceph config file")
		err = conn.ReadDefaultConfigFile()
		if err != nil {
			logger.Fatal("Error reading default config file.", err)
		}
		logger.Info("Connecting to ceph cluster")
		err = conn.Connect()
		if err != nil {
			logger.Fatal("Error establishing connection.", err)
		} else {
			logger.Info("Connected to ceph cluster")
		}
	}
	if iocx == nil {
		logger.Info("Opening ceph IO Context")
		iocx, err = conn.OpenIOContext("rbd")
		if err != nil {
			logger.Fatal("Error opening IOContext.", err)
		}
	}
	logger.Infof("instance id: %d", conn.GetInstanceID())
}
