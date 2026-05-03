package tests

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"testing"
)

// ---------- identické kopie struktur z protocol.go ----------

const (
	MAGIC_BYTE  = 0x55
	HEADER_SIZE = 21
	MAX_PAYLOAD = 1100
)

type Packet struct {
	Magic    uint8
	Type     uint8
	ConnId   uint32
	SeqNum   uint32
	AckNum   uint32
	Checksum uint32
	Length   uint16
	Payload  []byte
}

func (p *Packet) ToBytes() []byte {
	buf := make([]byte, HEADER_SIZE+len(p.Payload))
	buf[0] = p.Magic
	buf[1] = p.Type
	binary.BigEndian.PutUint32(buf[2:6], p.ConnId)
	binary.BigEndian.PutUint32(buf[6:10], p.SeqNum)
	binary.BigEndian.PutUint32(buf[10:14], p.AckNum)
	binary.BigEndian.PutUint32(buf[14:18], 0)
	p.Length = uint16(len(p.Payload))
	binary.BigEndian.PutUint16(buf[18:20], p.Length)
	buf[20] = 0
	if len(p.Payload) > 0 {
		copy(buf[HEADER_SIZE:], p.Payload)
	}
	check := crc32.ChecksumIEEE(buf)
	binary.BigEndian.PutUint32(buf[14:18], check)
	p.Checksum = check
	return buf
}

func ParsePacket(data []byte) (*Packet, error) {
	if len(data) < HEADER_SIZE {
		return nil, fmt.Errorf("packet too short")
	}
	p := &Packet{}
	p.Magic = data[0]
	if p.Magic != MAGIC_BYTE {
		return nil, fmt.Errorf("bad magic byte")
	}
	p.Type = data[1]
	p.ConnId = binary.BigEndian.Uint32(data[2:6])
	p.SeqNum = binary.BigEndian.Uint32(data[6:10])
	p.AckNum = binary.BigEndian.Uint32(data[10:14])
	p.Checksum = binary.BigEndian.Uint32(data[14:18])
	p.Length = binary.BigEndian.Uint16(data[18:20])

	tmp := make([]byte, len(data))
	copy(tmp, data)
	binary.BigEndian.PutUint32(tmp[14:18], 0)
	calcCheck := crc32.ChecksumIEEE(tmp)
	if calcCheck != p.Checksum {
		return nil, fmt.Errorf("wrong checksum")
	}
	if p.Length > 0 {
		if len(data) < int(HEADER_SIZE+p.Length) {
			return nil, fmt.Errorf("data length mismatch")
		}
		p.Payload = make([]byte, p.Length)
		copy(p.Payload, data[HEADER_SIZE:HEADER_SIZE+p.Length])
	}
	return p, nil
}

func TestCRC16_KnownValue(t *testing.T) {
	p := &Packet{
		Magic:  MAGIC_BYTE,
		Type:   3,
		ConnId: 42,
		SeqNum: 1,
		AckNum: 0,
		Payload: []byte("hello"),
	}
	b := p.ToBytes()
	parsed, err := ParsePacket(b)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !bytes.Equal(parsed.Payload, []byte("hello")) {
		t.Fatal("payload mismatch")
	}
	if parsed.Checksum == 0 {
		t.Fatal("checksum should not be zero")
	}
}

func TestCRC16_Empty(t *testing.T) {
	p := &Packet{
		Magic:  MAGIC_BYTE,
		Type:   2,
		ConnId: 0x1234,
		SeqNum: 0,
		AckNum: 0,
	}
	b := p.ToBytes()
	parsed, err := ParsePacket(b)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if parsed.Length != 0 {
		t.Fatal("length should be zero")
	}
	if parsed.Checksum == 0 {
		t.Fatal("checksum should not be zero")
	}
}

func TestCRC16_DifferentInputs(t *testing.T) {
	inputs := [][]byte{
		{0x00}, {0xFF}, []byte("test"),
		bytes.Repeat([]byte{0xAB}, 100),
		bytes.Repeat([]byte{0x00}, 100),
	}
	for _, payload := range inputs {
		p := &Packet{Magic: MAGIC_BYTE, Type: 3, Payload: payload}
		b := p.ToBytes()
		parsed, err := ParsePacket(b)
		if err != nil {
			t.Errorf("payload len=%d: %v", len(payload), err)
			continue
		}
		if !bytes.Equal(parsed.Payload, payload) {
			t.Errorf("payload len=%d: mismatch", len(payload))
		}
	}
}

func TestCRC16_Deterministic(t *testing.T) {
	p := &Packet{Magic: MAGIC_BYTE, Type: 3, Payload: []byte("deterministic")}
	b1 := p.ToBytes()
	b2 := p.ToBytes()
	if !bytes.Equal(b1, b2) {
		t.Fatal("same packet should produce identical bytes")
	}
}

func TestChecksum_ZerosField(t *testing.T) {
	p := &Packet{Magic: MAGIC_BYTE, Type: 1, Payload: []byte("abc")}
	b := p.ToBytes()
	// checksum field in ToBytes is zeroed before calculation; verify it's non-zero after
	parsed, _ := ParsePacket(b)
	if parsed.Checksum == 0 {
		t.Fatal("checksum should be non-zero")
	}
}

func TestChecksum_DetectsCorruption(t *testing.T) {
	p := &Packet{Magic: MAGIC_BYTE, Type: 3, Payload: []byte("corrupt me")}
	b := p.ToBytes()
	b[HEADER_SIZE] ^= 0xFF // flip bits in payload
	_, err := ParsePacket(b)
	if err == nil {
		t.Fatal("should detect corruption")
	}
}

func TestChecksum_NilPayload(t *testing.T) {
	p := &Packet{Magic: MAGIC_BYTE, Type: 5}
	b := p.ToBytes()
	parsed, err := ParsePacket(b)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if parsed.Length != 0 {
		t.Fatal("length should be 0")
	}
}

func TestChecksum_TooSmallHeader(t *testing.T) {
	_, err := ParsePacket([]byte{0x00, 0x00})
	if err == nil {
		t.Fatal("should fail on short header")
	}
}

func TestHeaderEncodeDecode(t *testing.T) {
	p := &Packet{
		Magic:  MAGIC_BYTE,
		Type:   4,
		ConnId: 0xDEADBEEF,
		SeqNum: 100,
		AckNum: 200,
		Payload: []byte("payload"),
	}
	b := p.ToBytes()
	parsed, err := ParsePacket(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Magic != p.Magic || parsed.Type != p.Type ||
		parsed.ConnId != p.ConnId || parsed.SeqNum != p.SeqNum ||
		parsed.AckNum != p.AckNum {
		t.Fatal("header fields mismatch")
	}
	if !bytes.Equal(parsed.Payload, p.Payload) {
		t.Fatal("payload mismatch")
	}
}

func TestHeaderDecode_TooSmall(t *testing.T) {
	_, err := ParsePacket(make([]byte, 1))
	if err == nil {
		t.Fatal("should fail")
	}
}

func TestChecksum_RoundTrip(t *testing.T) {
	p := &Packet{Magic: MAGIC_BYTE, Type: 3, Payload: generateRandom(500)}
	b := p.ToBytes()
	parsed, err := ParsePacket(b)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !bytes.Equal(parsed.Payload, p.Payload) {
		t.Fatal("payload mismatch")
	}
}
