package models

import (
	_ "github.com/denisenkom/go-mssqldb"

	"database/sql"
	"fmt"
	"github.com/astaxie/beego"
	"net"
	"time"
)

func Dial() *sql.DB {
	server := beego.AppConfig.String("mssqlserver")
	account := beego.AppConfig.String("mssqlaccount")
	password := beego.AppConfig.String("mssqlpass")
	dbinstance := beego.AppConfig.String("hoistdb")

	dsn := "server=" + server + ";user id=" + account + ";password=" + password + ";database=" + dbinstance
	dbconn, err := sql.Open("mssql", dsn)
	if err != nil {
		fmt.Println("Cannot connect: ", err.Error())
		return nil
	}
	err = dbconn.Ping()
	if err != nil {
		fmt.Println("Cannot connect: ", err.Error())
		return nil
	}

	return dbconn
}

// 存放设备状态，应该存放在Redis中
var DeviceStateMap = make(map[string]*Device, 100)

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

func LoadDeviceByTenantIDs(tenantIdArray []string) {

}
