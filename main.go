package main

import (
	_ "HoistMonitorServer/routers"

	"HoistMonitorServer/models"
	"HoistMonitorServer/socket"
	"github.com/astaxie/beego"
	"net"
)

/*
#cgo CFLAGS: -Ihikvision/include
#cgo LDFLAGS: -Lhikvision/lib -lHCNetSDK

#include <stdio.h>
#include <stdbool.h>
#include "HCNetSDK_c.h"
*/
import "C"

var (
	// 心跳包是“ZZ”
	Heartbeat = []byte{90, 90}
	// 数据包头是“RY”, 内容是16进制的RFID卡号
	DataHead = []byte{82, 89}
)

func init() {
	models.LoadDevice()
}

func main() {
	if beego.BConfig.RunMode == "dev" {
		beego.BConfig.WebConfig.DirectoryIndex = true
		beego.BConfig.WebConfig.StaticDir["/swagger"] = "swagger"
	}

	C.NET_DVR_Init()
	defer C.NET_DVR_Cleanup()

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
