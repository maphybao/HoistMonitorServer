package main

import (
	_ "HoistMonitorServer/routers"
	_ "github.com/denisenkom/go-mssqldb"

	"HoistMonitorServer/models"
	"HoistMonitorServer/socket"
	"container/list"
	"database/sql"
	"fmt"
	"github.com/astaxie/beego"
	"net"
	"sync"
)

/*
#cgo CFLAGS: -Ihikvision/include
#cgo LDFLAGS: -Lhikvision/lib -lHCNetSDK

#include <stdio.h>
#include <stdbool.h>
#include "HCNetSDK_c.h"

NET_DVR_DEVICEINFO_V40 deviceInfo;

LONG Login()
{
	NET_DVR_USER_LOGIN_INFO loginInfo =
	{
		.sDeviceAddress = "192.168.1.4",
		.sUserName = "admin",
		.sPassword = "HIKVISION520",
		.bUseAsynLogin = 0,
		.wPort = 8000
	};

	return NET_DVR_Login_V40(&loginInfo, &deviceInfo);
}

bool CaptureImage(LONG userID, char* path)
{
    NET_DVR_JPEGPARA strPicPara =
    {
    	.wPicQuality = 2,
    	.wPicSize = 0
    };
    int iRet;
    iRet = NET_DVR_CaptureJPEGPicture(userID, deviceInfo.struDeviceV30.byStartChan, &strPicPara, path);
    if (!iRet)
    {
        printf("pyd1---NET_DVR_CaptureJPEGPicture error, %d\n", NET_DVR_GetLastError());
        return false;
    }
}
*/
import "C"

var (
	// 心跳包是“ZZ”
	Heartbeat = []byte{90, 90}
	// 数据包头是“RY”, 内容是16进制的RFID卡号
	DataHead = []byte{82, 89}
	// 存放设备状态，应该存放在Redis中
	deviceStateMap             = make(map[string]*models.Device, 100)
	crewMap                    = make(map[string]*list.List, 100)
	deviceMapLock  *sync.Mutex = new(sync.Mutex)
)

func init() {
	// 需要启用了监控的租户列表
	// Todo: 分租户加载合法人员名单到crewMap
	// Todo: 分租户加载读头到deviceStateMap
}

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

func main() {
	if beego.BConfig.RunMode == "dev" {
		beego.BConfig.WebConfig.DirectoryIndex = true
		beego.BConfig.WebConfig.StaticDir["/swagger"] = "swagger"
	}

	C.NET_DVR_Init()
	userId := C.Login()

	// Prepare Socket Server
	service := ":3025"
	tcpAddr, _ := net.ResolveTCPAddr("tcp4", service)
	l, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		beego.Error("Error listen tcp on 3025: ", err)
		return
	}
	go startSocketServer(l)

	beego.Run()

	C.NET_DVR_Logout(userId)
	C.NET_DVR_Cleanup()
}

func startSocketServer(listener *net.TCPListener) {

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}

		sc := new(socket.SocketConnection)
		go sc.ConnectionHandler(conn)
	}
}

func DeactiveDevice(lock *sync.Mutex, deviceId string) {
	lock.Lock()
	device := deviceStateMap[deviceId]
	device.DeactiveDevice()
	lock.Unlock()
}

func UpdateDeviceActiveTime(lock *sync.Mutex, deviceId string) {
	lock.Lock()
	device := deviceStateMap[deviceId]
	device.UpdateDeviceActiveTime()
	lock.Unlock()
}
