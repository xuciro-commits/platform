package s7

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
)

// TestServer is a local S7comm peer for the documented read-only prototype.
// It is a protocol test service, not a PLC model or hardware compatibility proof.
type TestServer struct {
	listener net.Listener
	blocks   map[uint16][]byte
	pdu      int
	mu       sync.Mutex
	clients  map[net.Conn]bool
	closed   bool
}

func ListenTest(address string, blocks map[uint16][]byte, pdu int) (*TestServer, error) {
	if pdu < 64 || pdu > 960 {
		return nil, fmt.Errorf("test S7 PDU must be 64..960 bytes")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	s := &TestServer{listener: listener, blocks: map[uint16][]byte{}, pdu: pdu, clients: map[net.Conn]bool{}}
	for id, data := range blocks {
		s.blocks[id] = append([]byte(nil), data...)
	}
	go s.accept()
	return s, nil
}

func (s *TestServer) Address() string { return s.listener.Addr().String() }

func (s *TestServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	err := s.listener.Close()
	for conn := range s.clients {
		_ = conn.Close()
	}
	return err
}

func (s *TestServer) accept() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = conn.Close()
			return
		}
		s.clients[conn] = true
		s.mu.Unlock()
		go func() {
			defer func() {
				_ = conn.Close()
				s.mu.Lock()
				delete(s.clients, conn)
				s.mu.Unlock()
			}()
			s.serve(conn)
		}()
	}
}

func (s *TestServer) serve(conn net.Conn) {
	cr, err := readPacket(conn)
	if err != nil || len(cr) != 18 || cr[0] != 17 || cr[1] != 0xe0 || cr[7] != 0xc1 || cr[8] != 2 || cr[11] != 0xc2 || cr[12] != 2 || cr[15] != 0xc0 || cr[16] != 1 {
		return
	}
	cc := append([]byte(nil), cr...)
	cc[1] = 0xd0
	copy(cc[2:4], cr[4:6])
	binary.BigEndian.PutUint16(cc[4:6], 1)
	if writePacket(conn, cc) != nil {
		return
	}
	negotiated := 0
	for {
		request, err := readPacket(conn)
		if err != nil || len(request) < 15 || request[0] != 2 || request[1] != 0xf0 || request[2] != 0x80 || request[3] != 0x32 || request[4] != 1 || binary.BigEndian.Uint16(request[11:13]) != 0 {
			return
		}
		if int(binary.BigEndian.Uint16(request[9:11])) != len(request)-13 {
			return
		}
		params := request[13:]
		answer := []byte{2, 0xf0, 0x80, 0x32, 3, 0, 0, request[7], request[8], 0, 0, 0, 0, 0, 0}
		var reply, data []byte
		switch {
		case params[0] == 0xf0 && negotiated == 0:
			if len(params) != 8 || params[1] != 0 || binary.BigEndian.Uint16(params[2:4]) != 1 || binary.BigEndian.Uint16(params[4:6]) != 1 {
				return
			}
			negotiated = min(int(binary.BigEndian.Uint16(params[6:])), s.pdu)
			reply = append([]byte(nil), params...)
			binary.BigEndian.PutUint16(reply[6:], uint16(negotiated))
		case params[0] == 4 && negotiated > 0:
			count := int(params[1])
			if count == 0 || count > 20 || len(params) != 2+12*count || len(request)-3 > negotiated {
				return
			}
			reply = []byte{4, byte(count)}
			for i := 0; i < count; i++ {
				item := params[2+12*i : 14+12*i]
				if item[0] != 0x12 || item[1] != 10 || item[2] != 0x10 || item[8] != 0x84 {
					return
				}
				value := s.readItem(item)
				data = append(data, value...)
				if len(value)%2 != 0 && i+1 < count {
					data = append(data, 0)
				}
			}
		default:
			return
		}
		binary.BigEndian.PutUint16(answer[9:11], uint16(len(reply)))
		binary.BigEndian.PutUint16(answer[11:13], uint16(len(data)))
		if writePacket(conn, append(append(answer, reply...), data...)) != nil {
			return
		}
	}
}

func (s *TestServer) readItem(item []byte) []byte {
	db := binary.BigEndian.Uint16(item[6:8])
	block, exists := s.blocks[db]
	if !exists {
		return []byte{0x0a, 0, 0, 0}
	}
	bit := int(item[9])<<16 | int(item[10])<<8 | int(item[11])
	at := bit / 8
	amount := int(binary.BigEndian.Uint16(item[4:6]))
	if amount < 1 || at >= len(block) {
		return []byte{5, 0, 0, 0}
	}
	switch item[3] {
	case 1:
		if amount != 1 {
			return []byte{6, 0, 0, 0}
		}
		return []byte{0xff, 3, 0, 1, (block[at] >> (bit % 8)) & 1}
	case 2:
		if bit%8 != 0 || at+amount > len(block) {
			return []byte{5, 0, 0, 0}
		}
		value := []byte{0xff, 4, 0, 0}
		binary.BigEndian.PutUint16(value[2:], uint16(amount*8))
		return append(value, block[at:at+amount]...)
	default:
		return []byte{6, 0, 0, 0}
	}
}
