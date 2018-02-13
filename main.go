package main

import (
	_ "HoistMonitorServer/routers"
	_ "github.com/denisenkom/go-mssqldb"

	"container/list"
	"database/sql"
	"fmt"
	"github.com/astaxie/beego"
	"io"
	"net"
	"sync"
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

const (
	RFIDLifeTime = 3
	MinWarningC  = 9
)

var (
	// 心跳包是“ZZ”
	Heartbeat = []byte{90, 90}
	// 数据包头是“RY”, 内容是16进制的RFID卡号
	DataHead = []byte{82, 89}
	// 存放设备状态，应该存放在Redis中
	deviceStateMap             = make(map[string]*DeviceState, 100)
	crewMap                    = make(map[string]*list.List, 100)
	deviceMapLock  *sync.Mutex = new(sync.Mutex)
)

type DeviceState struct {
	IPAddress         net.Addr
	LastHeartbeatTime time.Time
	IsActive          bool
}

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

		go ConnectionHandler(conn)
	}
}

func deactiveDevice(deviceId string) {
	if len(deviceId) > 0 && deviceStateMap[deviceId] != nil {
		deviceStateMap[deviceId].IsActive = false
	}
}

func DeactiveDevice(lock *sync.Mutex, deviceId string) {
	lock.Lock()
	deactiveDevice(deviceId)
	lock.Unlock()
}

func updateDeviceActiveTime(deviceId string) {
	if len(deviceId) > 0 && deviceStateMap[deviceId] != nil {
		deviceStateMap[deviceId].LastHeartbeatTime = time.Now()
	}
}

func UpdateDeviceActiveTime(lock *sync.Mutex, deviceId string) {
	lock.Lock()
	updateDeviceActiveTime(deviceId)
	lock.Unlock()
}

func ConnectionHandler(conn net.Conn) {
	var deviceId string
	var crewRFIDSet = make(map[string]*time.Time, 20)

	defer conn.Close()
	defer DeactiveDevice(deviceMapLock, deviceId)

	addr := conn.RemoteAddr()
	beego.Info("reader ip: ", addr)
	deviceId, err := ReadLoginPacket(conn)
	beego.Info("reader id: ", deviceId)
	if err != nil {
		beego.Error("Error read from socket. Connection will be close: ", err)
		return
	}

	deviceMapLock.Lock()
	deviceStateMap[deviceId] = &DeviceState{IPAddress: addr, LastHeartbeatTime: time.Now(), IsActive: true}
	deviceMapLock.Unlock()

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
		tail = PreprocessPacket(data, deviceId, crewRFIDSet)
	}
}

func PreprocessPacket(data []byte, deviceId string, crewSet map[string]*time.Time) []byte {
	if isHeartbeat(data) {
		go HeartbeatPacketHandler(data[0:2], deviceId)
		if len(data) > 2 {
			return PreprocessPacket(data[2:], deviceId, crewSet)
		}

		return nil
	}

	if isRFIDPacket(data) {
		//Todo: 处理RFID
		packet, tail := parseRFIDPackets(data)
		if packet != nil {
			go RFIDPacketHandler(packet, crewSet)
			if tail != nil {
				return PreprocessPacket(tail, deviceId, crewSet)
			}
		}

		// 半包
		if packet == nil && tail != nil {
			return tail
		}

		return nil
	}

	beego.Error("Unknown packet received: ", data)
	// 可能是刚好是断开在RFID节点上的半包
	// Todo: 添加包头后递归处理
	// 但如果真的是未知的异常包，继续递归可能导致系统崩溃。
	return nil
}

func HeartbeatPacketHandler(packet []byte, deviceId string) {
	UpdateDeviceActiveTime(deviceMapLock, deviceId)
	beego.Info("Heartbeat.")
	return
}

func RFIDPacketHandler(packet []byte, crewSet map[string]*time.Time) {
	idx := 2
	activeTime := time.Now()
	plength := len(packet)
	for {
		rlen := int(packet[idx])
		rdata := packet[idx+1 : rlen+idx+1]
		rfidArray := extractRFID(rdata)
		for _, rfid := range rfidArray {
			if isValidCrewRFID(rfid) {
				crewSet[rfid] = &activeTime
			}
			fmt.Println(rfid, " ")
		}

		idx += rlen + 1
		if idx >= plength {
			break
		}
	}

	for rfid := range crewSet {
		if isOutOfDate(rfid, crewSet) {
			delete(crewSet, rfid)
		}
	}

	fmt.Println("在场人数：", len(crewSet))

	return
}

func isOutOfDate(rfid string, crewSet map[string]*time.Time) bool {
	tt := crewSet[rfid]
	if tt != nil && time.Now().Sub(*tt) > time.Duration(RFIDLifeTime)*time.Second {
		return true
	}

	return false
}

// 检查是否是人员的RFID, 需要忽略设备、环境等发卡
func isValidCrewRFID(rfid string) bool {
	return true
}

func printRFIDData(packet []byte) {
	fmt.Printf("RFID Data：%02X ", packet[0])
	for i := 1; i < len(packet); i++ {
		fmt.Printf("%02X ", packet[i])
	}
	fmt.Printf("\n")
}

func extractRFID(packet []byte) []string {
	plength := len(packet)
	dlen := (plength - 5) / 4
	ret := make([]string, dlen)
	for i := 0; i < dlen; i++ {
		rfid := fmt.Sprintf("%02X%02X%02X%02X", packet[i*4+3], packet[i*4+4], packet[i*4+5], packet[i*4+6])
		ret[i] = rfid
	}

	return ret
}

func formatRFIDData(packet []byte) string {
	ret := ""
	for i := 0; i < len(packet); i++ {
		ret = ret + fmt.Sprintf("%02X", packet[i])
	}

	return ret
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
