package models

import (
	_ "github.com/denisenkom/go-mssqldb"

	"database/sql"
	"fmt"
	"github.com/astaxie/beego"
	"net"
	"time"
)

const (
	// Todo: 建设备表
	QueryDeviceCmd = "SELECT name, description, device_id, camera_port, camera_account, camera_password, tenant_layer_code, crew_limit FROM hoist_monitor_device WHERE del_flag = 0;"
)

// 存放设备状态，应该存放在Redis中
var deviceStateMap = make(map[string]*Device, 100)

func Dial() *sql.DB {
	server := beego.AppConfig.String("mssqlserver")
	account := beego.AppConfig.String("mssqlaccount")
	password := beego.AppConfig.String("mssqlpass")
	dbinstance := beego.AppConfig.String("hoistdb")

	dsn := "server=" + server + ";user id=" + account + ";password=" + password + ";database=" + dbinstance
	dbconn, err := sql.Open("mssql", dsn)
	if err != nil {
		fmt.Println("Connect failed: ", err.Error())
		return nil
	}
	err = dbconn.Ping()
	if err != nil {
		fmt.Println("Cannot ping: ", err.Error())
		return nil
	}

	return dbconn
}

type Device struct {
	Name              string    `required:"true" description:"监控点名称,数据库字段"`
	Description       string    `required:"false" description:"详细描述,数据库字段"`
	DeviceID          string    `required:"true" description:"设备编号,数据库字段"`
	CameraPort        int       `required:"true" description:"摄像头端口,数据库字段"`
	CameraAccount     string    `required:"true" description:"摄像头账号,数据库字段"`
	CameraPassword    string    `required:"true" description:"摄像头密码,数据库字段"`
	TenantLayerCode   int64     `required:"true" description:"租户组织机构层次码,数据库字段"`
	CrewLimit         int       `required:"true" description:"乘员上限,数据库字段"`
	Heartbeat         string    `required:"true" description:"保留字段,数据库字段"`
	PacketHead        string    `required:"true" description:"保留字段,数据库字段"`
	IPAddress         net.Addr  `required:"false" description:"设备的IP地址, 连接成功后设置"`
	LastHeartbeatTime time.Time `required:"false" description:"设备的心跳时间, 每次收到心跳包后设置"`
	IsActive          bool      `required:"false" description:"设备是否活跃, 连接/断开时设置"`
}

func (d *Device) DeactiveDevice() {
	d.IsActive = false
}

func (d *Device) UpdateDeviceActiveTime() {
	d.LastHeartbeatTime = time.Now()
}

func (d *Device) PlayWarning() {
}

func (d *Device) TakePicture() {
}

func LoadDevice() {
	conn := Dial()
	defer conn.Close()

	stmt, err := conn.Prepare(QueryDeviceCmd)
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		beego.Error("Error query device by tenantId", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		device := new(Device)
		rows.Scan(&device.Name, &device.Description, &device.DeviceID, &device.CameraPort, &device.CameraAccount, &device.CameraPassword, &device.TenantLayerCode,
			&device.CrewLimit)
		deviceStateMap[device.DeviceID] = device
	}
}

func GetDevice(deviceId string) *Device {
	return deviceStateMap[deviceId]
}
