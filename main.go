package main

import (
	_ "HoistMonitorServer/routers"
	_ "github.com/denisenkom/go-mssqldb"

	"fmt"
	"github.com/astaxie/beego"
	"io"
	"net"
	"runtime"
	"time"
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
	deviceActiveMap = map[string]*DeviceState{}
)

type DeviceState struct {
	IPAddress         net.Addr
	LastHeartbeatTime time.Time
	IsActive          bool
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

		go ConnectionHandler(conn)
	}
}

func deactiveDevice(deviceId string) {
	if len(deviceId) > 0 && deviceActiveMap[deviceId] != nil {
		deviceActiveMap[deviceId].IsActive = false
	}
}

func updateDeviceActiveTime(deviceId string) {
	if len(deviceId) > 0 && deviceActiveMap[deviceId] != nil {
		deviceActiveMap[deviceId].LastHeartbeatTime = time.Now()
	}
}

func ConnectionHandler(conn net.Conn) {
	var deviceId string

	defer conn.Close()
	defer deactiveDevice(deviceId)

	addr := conn.RemoteAddr()
	beego.Info("reader ip: ", addr)
	deviceId, err := ReadLoginPacket(conn)
	beego.Info("reader id: ", deviceId)
	if err != nil {
		beego.Error("Error read from socket. Connection will be close: ", err)
		return
	}

	deviceActiveMap[deviceId] = &DeviceState{IPAddress: addr, LastHeartbeatTime: time.Now(), IsActive: true}
	buff := make([]byte, 10*1024)
	var tail []byte = nil
	for {
		length, err := conn.Read(buff)
		if err != nil {
			if err.(net.Error).Timeout() {
				continue
			} else {
				beego.Error("Error read from socket. Connection will be close: ", err)
				break
			}
		}

		data := make([]byte, length)
		if tail != nil {
			beego.Info("上次收到的尾包：", tail)
			if isRFIDPacket(buff) {
				// 串口服务器拆分的半包
				data = append(tail, buff[2:length]...)
			} else {
				// 4G网络拆分的半包
				data = append(tail, buff[:length]...)
			}
			beego.Info("拼接完成的新包：", data)
		} else {
			copy(data, buff)
		}
		tail = PreprocessPacket(data, deviceId)
	}
}

func PreprocessPacket(data []byte, deviceId string) []byte {
	if isHeartbeat(data) {
		go HeartbeatPacketHandler(data[0:2], deviceId)
		if len(data) > 2 {
			return PreprocessPacket(data[2:], deviceId)
		}

		return nil
	}

	if isRFIDPacket(data) {
		//Todo: 处理RFID
		packet, tail := parseRFIDPackets(data)
		if packet != nil {
			go RFIDPacketHandler(packet)
			if tail != nil {
				return PreprocessPacket(tail, deviceId)
			}
		}

		// 半包
		if packet == nil && tail != nil {
			return tail
		}

		return nil
	}

	beego.Error("Unknown packet received: ", data)
	return nil
}

func HeartbeatPacketHandler(packet []byte, deviceId string) {
	updateDeviceActiveTime(deviceId)
	fmt.Println("Heartbeat.")
	beego.Info("当前协程数：", runtime.NumGoroutine())
	return
}

func RFIDPacketHandler(packet []byte) {
	// Todo: update reader last active time;
	fmt.Printf("RFID Data：%02X ", packet[0])
	for i := 1; i < len(packet); i++ {
		fmt.Printf("%02X ", packet[i])
	}
	fmt.Printf("\n")
	beego.Info("当前协程数：", runtime.NumGoroutine())

	return
}

func ReadLoginPacket(conn net.Conn) (string, error) {
	loginBuf := make([]byte, 8)
	_, err := io.ReadFull(conn, loginBuf)
	if err != nil {
		beego.Error("Error read login packet: ", err)
		return "", err
	}

	var deviceId = fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x", loginBuf[0], loginBuf[1], loginBuf[2],
		loginBuf[3], loginBuf[4], loginBuf[5], loginBuf[6], loginBuf[7])

	fmt.Println("设备号：", deviceId)
	return deviceId, nil
}

func isHeartbeat(packet []byte) bool {
	return (packet[0] == Heartbeat[0]) && (packet[1] == Heartbeat[1])
}

func isRFIDPacket(packet []byte) bool {
	return (packet[0] == DataHead[0]) && (packet[1] == DataHead[1])
}

func parseRFIDPackets(data []byte) ([]byte, []byte) {
	// 处理粘包场景
	for i := 2; i < len(data)-1; i++ {
		if isHeartbeat(data[i:]) || isRFIDPacket(data[i:]) {
			// 粘包，返回第一个完整数据包，余下的部分是尾包
			beego.Info("发现粘包：", data)
			return data[:i], data[i:]
		}
	}

	// 处理半包场景
	idx := 2
	for {
		plen := int(data[idx])
		if idx+plen+1 > len(data) {
			beego.Info("发现半包：", data)
			return nil, data
		}

		if idx+plen+1 == len(data) {
			// 正常RFID数据包
			break
		}

		idx += plen + 1
	}

	return data, nil
}
