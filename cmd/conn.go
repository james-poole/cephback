package cmd

import (
	"errors"
	"fmt"
	"github.com/ceph/go-ceph/rados"
)

var conn *rados.Conn
var iocx *rados.IOContext
var err error

func CephConnInit() error {
	if conn == nil {
		logger.Infof("Creating ceph connection with user %s", user)
		if conn, err = rados.NewConnWithUser(user); err != nil {
			return errors.New(fmt.Sprintf("Error creating connection. %s", err))
		}
		logger.Info("Reading ceph config file")
		if err = conn.ReadDefaultConfigFile(); err != nil {
			return errors.New(fmt.Sprintf("Error reading default config file. %s", err))
		}
		logger.Info("Connecting to ceph cluster")
		if err = conn.Connect(); err != nil {
			return errors.New(fmt.Sprintf("Error establishing connection to ceph. %s", err))
		} else {
			logger.Info("Connected to ceph cluster")
		}
	}
	if iocx == nil {
		logger.Info("Opening ceph IO Context")
		if iocx, err = conn.OpenIOContext("rbd"); err != nil {
			return errors.New(fmt.Sprintf("Error opening IOContext. %s", err))
		}
	}
	return nil
}
