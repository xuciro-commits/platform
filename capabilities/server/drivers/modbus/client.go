// Package modbus implements the read-only Modbus TCP profile. Register values
// use high byte and high word first (ABCD); serial RTU and writes are excluded.
package modbus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"platformserver/platform"
)

type Config struct {
	Address string
	UnitID  byte
	Timeout time.Duration
}

type Client struct {
	mu      sync.Mutex
	conn    net.Conn
	config  Config
	request uint16
}

type Result struct {
	Value any
	Error string
}

type location struct {
	index    int
	function byte
	start    int
	width    int
	bit      int
	kind     string
}

// Addresses name the area and the zero-based wire offset, e.g. holding:0.
// A register bit is explicit, e.g. holding:191.3, with bit 0 least significant.
func parsePoint(point platform.DriverPoint) (location, error) {
	l := location{width: 1, bit: -1, kind: strings.ToUpper(point.DataType)}
	area, address, ok := strings.Cut(strings.TrimSpace(point.Address), ":")
	if !ok {
		return l, fmt.Errorf("use a zero-based address such as holding:0 or coil:0")
	}
	switch area {
	case "coil":
		l.function = 1
	case "discrete":
		l.function = 2
	case "holding":
		l.function = 3
	case "input":
		l.function = 4
	default:
		return l, fmt.Errorf("Modbus area must be coil, discrete, holding or input")
	}
	if base, bit, found := strings.Cut(address, "."); found {
		n, err := strconv.ParseUint(bit, 10, 4)
		if err != nil || l.function < 3 || l.kind != "BOOL" {
			return l, fmt.Errorf("only a BOOL register point can select bit 0..15")
		}
		address, l.bit = base, int(n)
	}
	n, err := strconv.ParseUint(address, 10, 16)
	if err != nil {
		return l, fmt.Errorf("Modbus wire offset must be 0..65535")
	}
	l.start = int(n)
	switch l.kind {
	case "BOOL":
		if point.Type != "boolean" || (l.function >= 3 && l.bit < 0) {
			return l, fmt.Errorf("BOOL needs a boolean point and a coil, discrete input or explicit register bit")
		}
	case "INT16", "UINT16", "INT32", "UINT32", "FLOAT32":
		if l.function < 3 || !slices.Contains([]string{"integer", "decimal", "number"}, point.Type) || l.kind == "FLOAT32" && point.Type == "integer" {
			return l, fmt.Errorf("numeric Modbus types need a compatible numeric register point")
		}
		if l.kind == "INT32" || l.kind == "UINT32" || l.kind == "FLOAT32" {
			l.width = 2
		}
	default:
		return l, fmt.Errorf("Modbus data type must be BOOL, INT16, UINT16, INT32, UINT32 or FLOAT32")
	}
	if l.start+l.width > 65536 {
		return l, fmt.Errorf("Modbus point extends beyond wire offset 65535")
	}
	return l, nil
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	address := config.Address
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(strings.Trim(address, "[]"), "502")
	}
	conn, err := (&net.Dialer{Timeout: config.Timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, config: config}, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}

// Read combines adjacent/overlapping declarations without reading undeclared
// gaps. A device exception or decode error affects that batch, not later areas.
func (c *Client) Read(ctx context.Context, points []platform.DriverPoint) ([]Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	results := make([]Result, len(points))
	locations := make([]location, 0, len(points))
	for i, point := range points {
		l, err := parsePoint(point)
		if err != nil {
			results[i].Error = err.Error()
			continue
		}
		l.index = i
		locations = append(locations, l)
	}
	slices.SortStableFunc(locations, func(a, b location) int {
		if a.function != b.function {
			return int(a.function) - int(b.function)
		}
		return a.start - b.start
	})
	for at := 0; at < len(locations); {
		first := locations[at]
		end, next, limit := first.start+first.width, at+1, 125
		if first.function < 3 {
			limit = 2000
		}
		for next < len(locations) {
			point := locations[next]
			if point.function != first.function || point.start > end || max(end, point.start+point.width)-first.start > limit {
				break
			}
			end = max(end, point.start+point.width)
			next++
		}
		data, err := c.read(ctx, first.function, first.start, end-first.start)
		var refusal *exception
		if err != nil && !errors.As(err, &refusal) {
			return nil, err
		}
		for _, point := range locations[at:next] {
			result := &results[point.index]
			if err != nil {
				result.Error = err.Error()
				continue
			}
			offset := point.start - first.start
			if point.function < 3 {
				result.Value = data[offset/8]&(1<<uint(offset%8)) != 0
				continue
			}
			value := data[offset*2 : (offset+point.width)*2]
			switch point.kind {
			case "BOOL":
				result.Value = binary.BigEndian.Uint16(value)&(1<<uint(point.bit)) != 0
			case "INT16":
				result.Value = strconv.FormatInt(int64(int16(binary.BigEndian.Uint16(value))), 10)
			case "UINT16":
				result.Value = strconv.FormatUint(uint64(binary.BigEndian.Uint16(value)), 10)
			case "INT32":
				result.Value = strconv.FormatInt(int64(int32(binary.BigEndian.Uint32(value))), 10)
			case "UINT32":
				result.Value = strconv.FormatUint(uint64(binary.BigEndian.Uint32(value)), 10)
			case "FLOAT32":
				n := float64(math.Float32frombits(binary.BigEndian.Uint32(value)))
				if math.IsNaN(n) || math.IsInf(n, 0) {
					result.Error = "Modbus FLOAT32 is not finite"
				} else {
					result.Value = strconv.FormatFloat(n, 'f', -1, 32)
				}
			}
		}
		at = next
	}
	return results, nil
}

type exception struct{ code byte }

func (e *exception) Error() string { return fmt.Sprintf("Modbus device exception 0x%02x", e.code) }

func (c *Client) read(ctx context.Context, function byte, start, count int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(c.config.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	c.request++
	request := make([]byte, 12)
	binary.BigEndian.PutUint16(request, c.request)
	binary.BigEndian.PutUint16(request[4:], 6)
	request[6], request[7] = c.config.UnitID, function
	binary.BigEndian.PutUint16(request[8:], uint16(start))
	binary.BigEndian.PutUint16(request[10:], uint16(count))
	if n, err := c.conn.Write(request); err != nil {
		return nil, err
	} else if n != len(request) {
		return nil, io.ErrShortWrite
	}
	header := make([]byte, 7)
	if _, err := io.ReadFull(c.conn, header); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(header[4:]))
	if binary.BigEndian.Uint16(header) != c.request || binary.BigEndian.Uint16(header[2:]) != 0 || header[6] != c.config.UnitID || length < 3 || length > 254 {
		return nil, fmt.Errorf("Modbus reply has an invalid transaction, protocol, unit or length")
	}
	pdu := make([]byte, length-1)
	if _, err := io.ReadFull(c.conn, pdu); err != nil {
		return nil, err
	}
	if pdu[0] == function|0x80 && len(pdu) == 2 {
		return nil, &exception{code: pdu[1]}
	}
	want := count * 2
	if function < 3 {
		want = (count + 7) / 8
	}
	if pdu[0] != function || int(pdu[1]) != want || len(pdu) != want+2 {
		return nil, fmt.Errorf("Modbus reply does not contain the requested values")
	}
	return pdu[2:], nil
}
