package modbus

import (
	"encoding/binary"
	"io"
	"maps"
	"net"
	"sync"
)

// Memory and TestServer supply deterministic protocol data for local use.
// They do not model a PLC, control equipment or establish hardware compatibility.
type Memory struct {
	Coils    map[uint16]bool   `json:"coils,omitempty"`
	Discrete map[uint16]bool   `json:"discrete,omitempty"`
	Holding  map[uint16]uint16 `json:"holding,omitempty"`
	Input    map[uint16]uint16 `json:"input,omitempty"`
}

type TestServer struct {
	listener net.Listener
	units    map[byte]Memory
	mu       sync.Mutex
	clients  map[net.Conn]bool
	closed   bool
}

func ListenTest(address string, units map[byte]Memory) (*TestServer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	s := &TestServer{listener: listener, units: map[byte]Memory{}, clients: map[net.Conn]bool{}}
	for id, m := range units {
		s.units[id] = Memory{Coils: maps.Clone(m.Coils), Discrete: maps.Clone(m.Discrete), Holding: maps.Clone(m.Holding), Input: maps.Clone(m.Input)}
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
			for {
				request := make([]byte, 12)
				if _, err := io.ReadFull(conn, request); err != nil || binary.BigEndian.Uint16(request[2:]) != 0 || binary.BigEndian.Uint16(request[4:]) != 6 {
					return
				}
				pdu := s.reply(request[6], request[7], int(binary.BigEndian.Uint16(request[8:])), int(binary.BigEndian.Uint16(request[10:])))
				answer := append(request[:7:7], pdu...)
				binary.BigEndian.PutUint16(answer[4:], uint16(len(pdu)+1))
				if _, err := conn.Write(answer); err != nil {
					return
				}
			}
		}()
	}
}

func (s *TestServer) reply(unit, function byte, start, count int) []byte {
	refuse := func(code byte) []byte { return []byte{function | 0x80, code} }
	m, ok := s.units[unit]
	if !ok {
		return refuse(0x0b)
	}
	limit := 125
	if function == 1 || function == 2 {
		limit = 2000
	} else if function != 3 && function != 4 {
		return refuse(1)
	}
	if count < 1 || count > limit {
		return refuse(3)
	}
	if start+count > 65536 {
		return refuse(2)
	}
	bytes := count * 2
	if function < 3 {
		bytes = (count + 7) / 8
	}
	pdu := make([]byte, bytes+2)
	pdu[0], pdu[1] = function, byte(bytes)
	for i := range count {
		if function < 3 {
			area := m.Coils
			if function == 2 {
				area = m.Discrete
			}
			value, exists := area[uint16(start+i)]
			if !exists {
				return refuse(2)
			}
			if value {
				pdu[2+i/8] |= 1 << uint(i%8)
			}
		} else {
			area := m.Holding
			if function == 4 {
				area = m.Input
			}
			value, exists := area[uint16(start+i)]
			if !exists {
				return refuse(2)
			}
			binary.BigEndian.PutUint16(pdu[2+i*2:], value)
		}
	}
	return pdu
}
