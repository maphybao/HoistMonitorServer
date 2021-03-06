package socket

import (
	"HoistMonitorServer/models"
	"container/list"
	"fmt"
	"github.com/astaxie/beego"
	"io"
	"net"
	"time"
)

const (
	RFIDLifeTime = 3
)

type SocketConnection struct {
	conn         net.Conn
	device       *models.Device
	crewList     *list.List
	employeeList []string
	crewRFIDSet  map[string]*time.Time
}

func (s *SocketConnection) Init(conn net.Conn) {
	s.crewList = new(list.List)
	s.crewRFIDSet = make(map[string]*time.Time, 20)
	s.conn = conn
}

func (s *SocketConnection) ReadLoginPacket() (string, error) {
	loginBuf := make([]byte, 8)
	_, err := io.ReadFull(s.conn, loginBuf)
	if err != nil {
		beego.Error("Error read login packet: ", err)
		return "", err
	}

	var deviceId = fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x", loginBuf[0], loginBuf[1], loginBuf[2],
		loginBuf[3], loginBuf[4], loginBuf[5], loginBuf[6], loginBuf[7])

	fmt.Println("设备号：", deviceId)
	return deviceId, nil
}

func (s *SocketConnection) ConnectionHandler() {
	var deviceId string

	defer s.conn.Close()
	defer s.device.DeactiveDevice()

	addr := s.conn.RemoteAddr()
	beego.Info("reader ip: ", addr)
	deviceId, err := s.ReadLoginPacket()
	beego.Info("reader id: ", deviceId)
	s.device = models.GetDevice(deviceId)
	if err != nil {
		beego.Error("Error read from socket. Connection will be close: ", err)
		return
	}
	s.employeeList, _ = models.LoadEmployeeRFIDByTenant(s.device.TenantLayerCode)

	s.device.IPAddress = addr
	s.device.LastHeartbeatTime = time.Now()
	s.device.IsActive = true

	buff := make([]byte, 10*1024)
	var tail []byte = nil
	for {
		length, err := s.conn.Read(buff)
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
			if s.isRFIDPacket(buff) {
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
		tail = s.PreprocessPacket(data, deviceId)
	}
}

func (s *SocketConnection) PreprocessPacket(data []byte, deviceId string) []byte {
	if s.isHeartbeat(data) {
		go s.HeartbeatPacketHandler()
		if len(data) > 2 {
			return s.PreprocessPacket(data[2:], deviceId)
		}

		return nil
	}

	if s.isRFIDPacket(data) {
		//Todo: 处理RFID
		packet, tail := s.parseRFIDPackets(data)
		if packet != nil {
			go s.RFIDPacketHandler(packet)
			if tail != nil {
				return s.PreprocessPacket(tail, deviceId)
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

func (s *SocketConnection) HeartbeatPacketHandler() {
	s.device.UpdateDeviceActiveTime()
	beego.Info("Heartbeat.")
	return
}

func (s *SocketConnection) RFIDPacketHandler(packet []byte) {
	idx := 2
	activeTime := time.Now()
	plength := len(packet)
	for {
		rlen := int(packet[idx])
		rdata := packet[idx+1 : rlen+idx+1]
		rfidArray := s.extractRFID(rdata)
		for _, rfid := range rfidArray {
			if s.isValidCrewRFID(rfid) {
				s.crewRFIDSet[rfid] = &activeTime
			}
			fmt.Println(rfid, " ")
		}

		idx += rlen + 1
		if idx >= plength {
			break
		}
	}

	for rfid := range s.crewRFIDSet {
		if s.isOutOfDate(rfid) {
			delete(s.crewRFIDSet, rfid)
		}
	}

	crewCount := len(s.crewRFIDSet)
	fmt.Println("在场人数：", crewCount)
	if crewCount > s.device.CrewLimit {
		go s.OverloadHandler()
	}

	return
}

func (s *SocketConnection) isOutOfDate(rfid string) bool {
	tt := s.crewRFIDSet[rfid]
	if tt != nil && time.Now().Sub(*tt) > time.Duration(RFIDLifeTime)*time.Second {
		return true
	}

	return false
}

func (s *SocketConnection) OverloadHandler() {
	// Todo: 建告警表，记录告警信息
	s.device.PlayWarning()
	s.device.TakePicture()
}

// 检查是否是人员的RFID, 需要忽略设备、环境等发卡
func (s *SocketConnection) isValidCrewRFID(rfid string) bool {
	for _, erfid := range s.employeeList {
		if erfid == rfid {
			return true
		}
	}

	return false
}

func (s *SocketConnection) printRFIDData(packet []byte) {
	fmt.Printf("RFID Data：%02X ", packet[0])
	for i := 1; i < len(packet); i++ {
		fmt.Printf("%02X ", packet[i])
	}
	fmt.Printf("\n")
}

func (s *SocketConnection) extractRFID(packet []byte) []string {
	plength := len(packet)
	dlen := (plength - 5) / 4
	ret := make([]string, dlen)
	for i := 0; i < dlen; i++ {
		rfid := fmt.Sprintf("%02X%02X%02X%02X", packet[i*4+3], packet[i*4+4], packet[i*4+5], packet[i*4+6])
		ret[i] = rfid
	}

	return ret
}

func (s *SocketConnection) formatRFIDData(packet []byte) string {
	ret := ""
	for i := 0; i < len(packet); i++ {
		ret = ret + fmt.Sprintf("%02X", packet[i])
	}

	return ret
}

func (s *SocketConnection) isHeartbeat(packet []byte) bool {
	return (packet[0] == s.device.Heartbeat[0]) && (packet[1] == s.device.Heartbeat[1])
}

func (s *SocketConnection) isRFIDPacket(packet []byte) bool {
	return (packet[0] == s.device.PacketHead[0]) && (packet[1] == s.device.PacketHead[1])
}

func (s *SocketConnection) parseRFIDPackets(data []byte) ([]byte, []byte) {
	// 处理粘包场景
	for i := 2; i < len(data)-1; i++ {
		if s.isHeartbeat(data[i:]) || s.isRFIDPacket(data[i:]) {
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
