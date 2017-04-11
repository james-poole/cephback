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
			logger.Error("Error creating connection.", err)
			return
		}
		logger.Info("Reading ceph config file")
		err = conn.ReadDefaultConfigFile()
		if err != nil {
			logger.Error("Error reading default config file.", err)
			return
		}
		logger.Info("Connecting to ceph cluster")
		err = conn.Connect()
		if err != nil {
			logger.Error("Error establishing connection.", err)
			return
		}
	}
	if iocx == nil {
		logger.Info("Opening ceph IO Context")
		iocx, err = conn.OpenIOContext("rbd")
		if err != nil {
			logger.Error("Error opening IOContext.", err)
			return
		}
	}
	logger.Infof("instance id: %d", conn.GetInstanceID())
}
