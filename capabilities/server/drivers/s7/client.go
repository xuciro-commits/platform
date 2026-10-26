// Package s7 implements the read-only standard-DB subset of S7comm over
// ISO-on-TCP. It does not provide CPU control or claim verified PLC firmware.
package s7

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Point uses Siemens absolute DB addresses, for example DB1.DBX0.3,
// DB1.DBW2 or DB1.DBD4. Type is BOOL, BYTE, WORD, INT, DWORD, DINT or REAL.
type Point struct {
	Name    string
	Address string
	Type    string
}

type Result struct {
	Point Point
	Value any
	Error string
}

type Config struct {
	Address    string
	Rack       int
	Slot       int
	Timeout    time.Duration
	PDU        int
	LocalTSAP  uint16
	RemoteTSAP uint16
}

type Client struct {
	mu      sync.Mutex
	conn    net.Conn
	config  Config
	pdu     int
	request uint16
}

type location struct {
	db    uint16
	start uint32
	bit   bool
	size  int
}

var dbAddress = regexp.MustCompile(`(?i)^DB([0-9]+)\.(DBX|DBB|DBW|DBD)([0-9]+)(?:\.([0-7]))?$`)

func parsePoint(point Point) (location, error) {
	match := dbAddress.FindStringSubmatch(strings.TrimSpace(point.Address))
	if match == nil {
		return location{}, fmt.Errorf("use an absolute standard DB address such as DB1.DBW2")
	}
	db, dbErr := strconv.ParseUint(match[1], 10, 16)
	byteAt, atErr := strconv.ParseUint(match[3], 10, 21)
	if dbErr != nil || atErr != nil || db == 0 {
		return location{}, fmt.Errorf("DB number or byte address is outside the supported range")
	}
	l := location{db: uint16(db), start: uint32(byteAt) * 8}
	kind := strings.ToUpper(point.Type)
	want := ""
	switch kind {
	case "BOOL":
		want, l.bit, l.size = "DBX", true, 1
	case "BYTE":
		want, l.size = "DBB", 1
	case "WORD", "INT":
		want, l.size = "DBW", 2
	case "DWORD", "DINT", "REAL":
		want, l.size = "DBD", 4
	default:
		return location{}, fmt.Errorf("unsupported S7 data type %q", point.Type)
	}
	if strings.ToUpper(match[2]) != want || l.bit != (match[4] != "") {
		return location{}, fmt.Errorf("%s needs %s addressing%s", kind, want, map[bool]string{true: " with a bit number"}[l.bit])
	}
	if l.bit {
		l.start += uint32(match[4][0] - '0')
	}
	return l, nil
}

func ValidatePoint(point Point) error {
	_, err := parsePoint(point)
	return err
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.Rack < 0 || config.Rack > 7 || config.Slot < 0 || config.Slot > 31 {
		return nil, fmt.Errorf("S7 rack must be 0..7 and slot 0..31")
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.PDU == 0 {
		config.PDU = 480
	}
	if config.PDU < 64 || config.PDU > 960 {
		return nil, fmt.Errorf("requested S7 PDU must be 64..960 bytes")
	}
	if config.LocalTSAP == 0 {
		config.LocalTSAP = 0x0100
	}
	if config.RemoteTSAP == 0 {
		config.RemoteTSAP = 0x0100 | uint16(config.Rack*32+config.Slot)
	}
	address := config.Address
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(strings.Trim(address, "[]"), "102")
	}
	conn, err := (&net.Dialer{Timeout: config.Timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, config: config, pdu: config.PDU}
	if err = c.connect(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *Client) connect(ctx context.Context) error {
	cr := []byte{17, 0xe0, 0, 0, 0, 1, 0, 0xc1, 2, 0, 0, 0xc2, 2, 0, 0, 0xc0, 1, 10}
	binary.BigEndian.PutUint16(cr[9:11], c.config.LocalTSAP)
	binary.BigEndian.PutUint16(cr[13:15], c.config.RemoteTSAP)
	answer, err := c.exchange(ctx, cr)
	if err != nil {
		return err
	}
	if len(answer) < 7 || int(answer[0])+1 != len(answer) || answer[1] != 0xd0 || binary.BigEndian.Uint16(answer[2:4]) != 1 {
		return fmt.Errorf("S7 ISO-on-TCP connection was not confirmed")
	}
	parameters := []byte{0xf0, 0, 0, 1, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(parameters[6:], uint16(c.config.PDU))
	params, data, err := c.job(ctx, parameters)
	if err != nil {
		return err
	}
	if len(params) != 8 || params[0] != 0xf0 || len(data) != 0 {
		return fmt.Errorf("S7 communication setup has an invalid answer")
	}
	pdu := int(binary.BigEndian.Uint16(params[6:]))
	if pdu < 64 || pdu > c.config.PDU {
		return fmt.Errorf("S7 peer negotiated an unsupported PDU size %d", pdu)
	}
	c.pdu = pdu
	return nil
}

// Read batches valid points according to the negotiated PDU. An invalid
// address or a PLC item refusal affects that point, not its valid neighbours.
func (c *Client) Read(ctx context.Context, points []Point) ([]Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil, fmt.Errorf("S7 connection is closed")
	}
	results := make([]Result, len(points))
	indexes := []int{}
	locations := make([]location, len(points))
	for i, point := range points {
		results[i].Point = point
		l, err := parsePoint(point)
		if err != nil {
			results[i].Error = err.Error()
			continue
		}
		locations[i] = l
		indexes = append(indexes, i)
	}
	maximum := min(20, (c.pdu-12)/12)
	for len(indexes) > 0 {
		batch := indexes[:min(len(indexes), maximum)]
		parameters := []byte{4, byte(len(batch))}
		for _, i := range batch {
			l := locations[i]
			typeCode, amount := byte(2), l.size
			if l.bit {
				typeCode, amount = 1, 1
			}
			item := []byte{0x12, 10, 0x10, typeCode, 0, 0, 0, 0, 0x84, byte(l.start >> 16), byte(l.start >> 8), byte(l.start)}
			binary.BigEndian.PutUint16(item[4:6], uint16(amount))
			binary.BigEndian.PutUint16(item[6:8], l.db)
			parameters = append(parameters, item...)
		}
		params, data, err := c.job(ctx, parameters)
		if err != nil {
			return results, err
		}
		if len(params) != 2 || params[0] != 4 || int(params[1]) != len(batch) {
			return results, fmt.Errorf("S7 read answer does not match the requested points")
		}
		for n, i := range batch {
			if len(data) < 4 {
				return results, fmt.Errorf("S7 read item is truncated")
			}
			code, transport := data[0], data[1]
			length := int(binary.BigEndian.Uint16(data[2:4]))
			if transport == 3 || transport == 4 || transport == 5 {
				length = (length + 7) / 8
			}
			data = data[4:]
			if length > len(data) {
				return results, fmt.Errorf("S7 read value is truncated")
			}
			if code != 0xff {
				results[i].Error = fmt.Sprintf("S7 device refused this point (0x%02x)", code)
			} else if transport != 3 && transport != 4 && transport != 9 || length != locations[i].size || locations[i].bit && transport != 3 {
				results[i].Error = "S7 returned an incompatible value type or length"
			} else {
				results[i].Value, err = decode(points[i].Type, data[:length])
				if err != nil {
					results[i].Error = err.Error()
				}
			}
			data = data[length:]
			if length%2 == 1 && n < len(batch)-1 {
				if len(data) == 0 {
					return results, fmt.Errorf("S7 read item padding is missing")
				}
				data = data[1:]
			}
		}
		if len(data) != 0 {
			return results, fmt.Errorf("S7 read answer contains unexpected data")
		}
		indexes = indexes[len(batch):]
	}
	return results, nil
}

func decode(kind string, data []byte) (any, error) {
	switch strings.ToUpper(kind) {
	case "BOOL":
		return data[0]&1 != 0, nil
	case "BYTE":
		return strconv.FormatUint(uint64(data[0]), 10), nil
	case "WORD":
		return strconv.FormatUint(uint64(binary.BigEndian.Uint16(data)), 10), nil
	case "INT":
		return strconv.FormatInt(int64(int16(binary.BigEndian.Uint16(data))), 10), nil
	case "DWORD":
		return strconv.FormatUint(uint64(binary.BigEndian.Uint32(data)), 10), nil
	case "DINT":
		return strconv.FormatInt(int64(int32(binary.BigEndian.Uint32(data))), 10), nil
	case "REAL":
		n := math.Float32frombits(binary.BigEndian.Uint32(data))
		if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
			return nil, fmt.Errorf("S7 REAL is not finite")
		}
		return strconv.FormatFloat(float64(n), 'f', -1, 32), nil
	}
	return nil, fmt.Errorf("unsupported S7 value type")
}

func (c *Client) job(ctx context.Context, parameters []byte) ([]byte, []byte, error) {
	c.request++
	header := []byte{2, 0xf0, 0x80, 0x32, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(header[7:9], c.request)
	binary.BigEndian.PutUint16(header[9:11], uint16(len(parameters)))
	answer, err := c.exchange(ctx, append(header, parameters...))
	if err != nil {
		return nil, nil, err
	}
	if len(answer) < 15 || answer[0] != 2 || answer[1] != 0xf0 || answer[2] != 0x80 || answer[3] != 0x32 || answer[4] != 3 || binary.BigEndian.Uint16(answer[7:9]) != c.request {
		return nil, nil, fmt.Errorf("S7 answer has an invalid header or request identity")
	}
	if answer[13] != 0 || answer[14] != 0 {
		return nil, nil, fmt.Errorf("S7 peer rejected the request (0x%02x%02x)", answer[13], answer[14])
	}
	params, size := int(binary.BigEndian.Uint16(answer[9:11])), int(binary.BigEndian.Uint16(answer[11:13]))
	if len(answer) != 15+params+size || len(answer)-3 > c.pdu {
		return nil, nil, fmt.Errorf("S7 answer length differs from its declared PDU")
	}
	return answer[15 : 15+params], answer[15+params:], nil
}

func (c *Client) exchange(ctx context.Context, payload []byte) ([]byte, error) {
	deadline := time.Now().Add(c.config.Timeout)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Now()) })
	defer stop()
	if err := writePacket(c.conn, payload); err != nil {
		return nil, err
	}
	return readPacket(c.conn)
}

func writePacket(conn net.Conn, payload []byte) error {
	header := []byte{3, 0, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)+4))
	_, err := io.Copy(conn, bytes.NewReader(append(header, payload...)))
	return err
}

func readPacket(conn net.Conn) ([]byte, error) {
	header := [4]byte{}
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(header[2:]))
	if header[0] != 3 || header[1] != 0 || length < 7 || length > 4096 {
		return nil, fmt.Errorf("invalid ISO-on-TCP packet")
	}
	payload := make([]byte, length-4)
	_, err := io.ReadFull(conn, payload)
	return payload, err
}
