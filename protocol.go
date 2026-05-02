package main

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// my packet types
const (
	TYPE_SYN    = 1
	TYPE_SYNACK = 2
	TYPE_DATA   = 3
	TYPE_ACK    = 4
	TYPE_FIN    = 5
	TYPE_FINACK = 6
)

const MAGIC_BYTE = 0x55 // my magic byte for ipk
const MAX_PAYLOAD = 1100 // less than 1200 to be safe
const HEADER_SIZE = 21 // sum of all header fields

// Packet is my struct for sending
type Packet struct {
	Magic    uint8
	Type     uint8
	ConnId   uint32
	SeqNum   uint32
	AckNum   uint32
	Checksum uint32
	Length   uint16 // length of payload
	Payload  []byte
}

// this function make byte array from struct
func (p *Packet) ToBytes() []byte {
	// create buffer header is 21 bytes
	buf := make([]byte, HEADER_SIZE + len(p.Payload))
	
	buf[0] = p.Magic
	buf[1] = p.Type
	binary.BigEndian.PutUint32(buf[2:6], p.ConnId)
	binary.BigEndian.PutUint32(buf[6:10], p.SeqNum)
	binary.BigEndian.PutUint32(buf[10:14], p.AckNum)
	
	// put zero to checksum for now
	binary.BigEndian.PutUint32(buf[14:18], 0)
	
	p.Length = uint16(len(p.Payload))
	binary.BigEndian.PutUint16(buf[18:20], p.Length)
	
	// unused byte just to align
	buf[20] = 0
	
	// copy data to end
	if len(p.Payload) > 0 {
		copy(buf[HEADER_SIZE:], p.Payload)
	}

	// now calculate crc32 and put it inside
	check := crc32.ChecksumIEEE(buf)
	binary.BigEndian.PutUint32(buf[14:18], check)
	p.Checksum = check

	return buf
}

// this parse bytes back to packet
func ParsePacket(data []byte) (*Packet, error) {
	if len(data) < HEADER_SIZE {
		return nil, fmt.Errorf("packet too short")
	}

	p := &Packet{}
	p.Magic = data[0]
	if p.Magic != MAGIC_BYTE {
		return nil, fmt.Errorf("bad byte")
	}

	p.Type = data[1]
	p.ConnId = binary.BigEndian.Uint32(data[2:6])
	p.SeqNum = binary.BigEndian.Uint32(data[6:10])
	p.AckNum = binary.BigEndian.Uint32(data[10:14])
	p.Checksum = binary.BigEndian.Uint32(data[14:18])
	p.Length = binary.BigEndian.Uint16(data[18:20])

	// verify crc
	// need to copy because i will change checksum to 0
	tmp := make([]byte, len(data))
	copy(tmp, data)
	binary.BigEndian.PutUint32(tmp[14:18], 0) // zero out for check
	
	calcCheck := crc32.ChecksumIEEE(tmp)
	if calcCheck != p.Checksum {
		return nil, fmt.Errorf("wrong checksum, packet is broken")
	}

	// get payload if it has some
	if p.Length > 0 {
		// check if we really have enough data
		if len(data) < int(HEADER_SIZE + p.Length) {
			return nil, fmt.Errorf("data length is lying")
		}
		p.Payload = make([]byte, p.Length)
		copy(p.Payload, data[HEADER_SIZE:HEADER_SIZE+p.Length])
	} else {
		p.Payload = nil
	}

	return p, nil
}
