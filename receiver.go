// SetReadDeadline was recommended by gemini to make it more reliable than using gorutines
package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
)

func runServer(cfg *Config) error {
	addr := cfg.Address
	if addr == "" {
		addr = "0.0.0.0"
	}
	logf("Starting server on %s:%d (timeout: %ds)\n", addr, cfg.Port, cfg.Timeout)
	if cfg.Output != "" && cfg.Output != "-" {
		logf("write to file %s\n", cfg.Output)
	} else {
		logf("write to stdout\n")
	}

	listenAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(addr, strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("bad bind address %v", err)
	}

	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("cannot listen %v", err)
	}
	defer conn.Close()

	buffer := make([]byte, 2048)
	var activeConnId uint32
	var clientAddr *net.UDPAddr

	// wait for SYN
	timeoutDur := time.Duration(cfg.Timeout) * time.Second
	for {
		conn.SetReadDeadline(time.Now().Add(timeoutDur))
		n, cAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return fmt.Errorf("timeout waiting for client SYN %v", err)
		}

		p, err := ParsePacket(buffer[:n])
		if err != nil {
			logf("bad packet ignored\n")
			continue
		}

		if p.Type == TYPE_SYN {
			logf("got SYN from %v\n", cAddr)
			activeConnId = p.ConnId
			clientAddr = cAddr
			break
		}
	}

	// send SYNACK
	synack := Packet{
		Magic:  MAGIC_BYTE,
		Type:   TYPE_SYNACK,
		ConnId: activeConnId,
		SeqNum: 0,
		AckNum: 0,
	}
	conn.WriteToUDP(synack.ToBytes(), clientAddr)
	logf("Sending SYNACK\n")

	// retransmission loop for SYNACK
	handshakeStart := time.Now()
	gotAck := false

	for !gotAck {
		if time.Since(handshakeStart) > timeoutDur {
			return fmt.Errorf("timeout waiting for handshake ACK")
		}

		conn.WriteToUDP(synack.ToBytes(), clientAddr)

		// wait for ACK with timeout
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			// loop and retransmit SYNACK
			continue
		}

		resp, err := ParsePacket(buffer[:n])
		if err != nil {
			continue
		}

		if resp.Type == TYPE_ACK && resp.ConnId == activeConnId {
			logf("got ACK handshake complete\n")
			gotAck = true
		} else if (resp.Type == TYPE_DATA || resp.Type == TYPE_FIN) && resp.ConnId == activeConnId {
			// if ACK was lost but DATA or FIN arrived handshake is complete
			logf("got packet type %d instead of ACK handshake complete\n", resp.Type)
			gotAck = true
		}
	}

	// clear deadline for data phase
	conn.SetReadDeadline(time.Time{})

	// prepare output file
	var outFile *os.File
	if cfg.Output == "" || cfg.Output == "-" {
		outFile = os.Stdout
	} else {
		f, err := os.Create(cfg.Output)
		if err != nil {
			return fmt.Errorf("cannot create output file %v", err)
		}
		defer f.Close()
		outFile = f
	}

	logf("waiting for data packets\n")

	expectedSeq := uint32(0)
	finReceived := false
	finSeq := uint32(0)
	// map to buffer out of order packets
	packetBuffer := make(map[uint32][]byte)

	for {
		// refresh timeout every loop
		conn.SetReadDeadline(time.Now().Add(timeoutDur))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return fmt.Errorf("timeout or error %v", err)
		}

		p, err := ParsePacket(buffer[:n])
		if err != nil {
			logf("bad packet ignore\n")
			continue
		}

		if p.ConnId != activeConnId {
			logf("wrong connection\n")
			continue
		}

		if p.Type == TYPE_DATA {
			// got data
			if p.SeqNum == expectedSeq {
				// write to file
				outFile.Write(p.Payload)
				expectedSeq += uint32(p.Length)

				// check if we have next packets in buffer
				for {
					if bufferedData, ok := packetBuffer[expectedSeq]; ok {
						outFile.Write(bufferedData)
						delete(packetBuffer, expectedSeq)
						expectedSeq += uint32(len(bufferedData))
					} else {
						break
					}
				}
			} else if p.SeqNum > expectedSeq {
				// packet from future save
				// copy because buffer array will be overwritten
				dataCopy := make([]byte, len(p.Payload))
				copy(dataCopy, p.Payload)
				packetBuffer[p.SeqNum] = dataCopy
			}

			// send ACK back with expectedSeq
			ack := Packet{
				Magic:  MAGIC_BYTE,
				Type:   TYPE_ACK,
				ConnId: activeConnId,
				SeqNum: 0,
				AckNum: expectedSeq,
			}
			conn.WriteToUDP(ack.ToBytes(), clientAddr)
		} else if p.Type == TYPE_FIN {
			finReceived = true
			finSeq = p.SeqNum
			logf("got FIN for seq %d buffering until all data is written\n", finSeq)
		}

		// check if we can safely teardown
		if finReceived {
			if len(packetBuffer) == 0 && expectedSeq >= finSeq {
				logf("all data written sending FINACK\n")
			finack := Packet{
				Magic:  MAGIC_BYTE,
				Type:   TYPE_FINACK,
				ConnId: activeConnId,
				SeqNum: 0,
				AckNum: expectedSeq,
			}
			conn.WriteToUDP(finack.ToBytes(), clientAddr)
			logf("sent FINACK entering TIME_WAIT\n")

			// TIME_WAIT loop to re-ack any retransmitted FINs
			timeWaitEnd := time.Now().Add(2 * time.Second)
			for time.Now().Before(timeWaitEnd) {
				conn.SetReadDeadline(timeWaitEnd)
				n, _, err := conn.ReadFromUDP(buffer)
				if err != nil {
					break
				}
				p, err := ParsePacket(buffer[:n])
				if err == nil && p.Type == TYPE_FIN && p.ConnId == activeConnId {
					// client not get our FINACK send again
					conn.WriteToUDP(finack.ToBytes(), clientAddr)
					logf("retransmitted FINACK\n")
				}
			}

			logf("server goodbye\n")
			break
			}
		}
	}

	return nil
}
