package models

import (
	"net"
	"time"
)

type Device struct {
	Name              string
	Description       string
	DeviceID          string
	CameraPort        int
	CameraAccount     string
	CameraPassword    string
	TenantID          string
	CrewLimit         int
	Heartbeat         string
	PacketHead        string
	IPAddress         net.Addr
	LastHeartbeatTime time.Time
	IsActive          bool
}

func (d *Device) DeactiveDevice() {
	d.IsActive = false
}

func (d *Device) UpdateDeviceActiveTime() {
	d.LastHeartbeatTime = time.Now()
}
