package main

import (
	_ "HoistMonitorServer/routers"

	"HoistMonitorServer/models"
	"HoistMonitorServer/socket"
	"github.com/astaxie/beego"
	"net"
	"strings"
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
)

func init() {
	// 需要启用了监控的租户列表
	// Todo: 分租户加载合法人员名单到crewMap
	// Todo: 分租户加载读头到deviceStateMap
	registeredTenant := beego.AppConfig.String("tenantlist")
	tenantlist := strings.Split(registeredTenant, ",")
	models.LoadDeviceByTenantIDs(tenantlist)
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
		sc.Init(conn)
		go sc.ConnectionHandler()
	}
}
