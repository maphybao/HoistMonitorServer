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
	Conn     net.Conn
	Device   *models.Device
	crewList *list.List
}

func (s *SocketConnection) Init() {
	s.crewList = new(list.List)
}

func (s *SocketConnection) ReadLoginPacket() (string, error) {
	loginBuf := make([]byte, 8)
	_, err := io.ReadFull(s.Conn, loginBuf)
	if err != nil {
		beego.Error("Error read login packet: ", err)
		return "", err
	}

	var deviceId = fmt.Sprintf("%02x%02x%02x%02x%02x%02x%02x%02x", loginBuf[0], loginBuf[1], loginBuf[2],
		loginBuf[3], loginBuf[4], loginBuf[5], loginBuf[6], loginBuf[7])

	fmt.Println("设备号：", deviceId)
	return deviceId, nil
}

func (s *SocketConnection) ConnectionHandler(conn net.Conn) {
	s.Conn = conn
	var deviceId string
	var crewRFIDSet = make(map[string]*time.Time, 20)

	defer s.Conn.Close()
	defer s.Device.DeactiveDevice()

	addr := conn.RemoteAddr()
	beego.Info("reader ip: ", addr)
	deviceId, err := s.ReadLoginPacket()
	beego.Info("reader id: ", deviceId)
	if err != nil {
		beego.Error("Error read from socket. Connection will be close: ", err)
		return
	}

	s.Device.IPAddress = addr
	s.Device.LastHeartbeatTime = time.Now()
	s.Device.IsActive = true

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
		tail = s.PreprocessPacket(data, deviceId, crewRFIDSet)
	}
}

func (s *SocketConnection) PreprocessPacket(data []byte, deviceId string, crewSet map[string]*time.Time) []byte {
	if s.isHeartbeat(data) {
		go s.HeartbeatPacketHandler()
		if len(data) > 2 {
			return s.PreprocessPacket(data[2:], deviceId, crewSet)
		}

		return nil
	}

	if s.isRFIDPacket(data) {
		//Todo: 处理RFID
		packet, tail := s.parseRFIDPackets(data)
		if packet != nil {
			go s.RFIDPacketHandler(packet, crewSet)
			if tail != nil {
				return s.PreprocessPacket(tail, deviceId, crewSet)
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
	s.Device.UpdateDeviceActiveTime()
	beego.Info("Heartbeat.")
	return
}

func (s *SocketConnection) RFIDPacketHandler(packet []byte, crewSet map[string]*time.Time) {
	idx := 2
	activeTime := time.Now()
	plength := len(packet)
	for {
		rlen := int(packet[idx])
		rdata := packet[idx+1 : rlen+idx+1]
		rfidArray := s.extractRFID(rdata)
		for _, rfid := range rfidArray {
			if s.isValidCrewRFID(rfid) {
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
		if s.isOutOfDate(rfid, crewSet) {
			delete(crewSet, rfid)
		}
	}

	fmt.Println("在场人数：", len(crewSet))

	return
}

func (s *SocketConnection) isOutOfDate(rfid string, crewSet map[string]*time.Time) bool {
	tt := crewSet[rfid]
	if tt != nil && time.Now().Sub(*tt) > time.Duration(RFIDLifeTime)*time.Second {
		return true
	}

	return false
}

// 检查是否是人员的RFID, 需要忽略设备、环境等发卡
func (s *SocketConnection) isValidCrewRFID(rfid string) bool {
	return true
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
	return (packet[0] == s.Device.Heartbeat[0]) && (packet[1] == s.Device.Heartbeat[1])
}

func (s *SocketConnection) isRFIDPacket(packet []byte) bool {
	return (packet[0] == s.Device.PacketHead[0]) && (packet[1] == s.Device.PacketHead[1])
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
