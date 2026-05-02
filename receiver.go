package main

import (
	"fmt"
	"net"
	"time"
)

func runServer(cfg *Config) error {
	addr := cfg.Address
	if addr == "" {
		addr = "0.0.0.0"
	}
	fmt.Printf("Starting server on %s:%d (timeout: %ds)\n", addr, cfg.Port, cfg.Timeout)
	if cfg.Output != "" && cfg.Output != "-" {
		fmt.Printf("Will write to file: %s\n", cfg.Output)
	} else {
		fmt.Printf("Will write to stdout\n")
	}

	listenAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", addr, cfg.Port))
	if err != nil {
		return fmt.Errorf("bad bind address: %v", err)
	}

	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return fmt.Errorf("cannot listen: %v", err)
	}
	defer conn.Close()

	buffer := make([]byte, 2048)
	var activeConnId uint32
	var clientAddr *net.UDPAddr

	// wait for SYN
	for {
		// don't set timeout here waiting for first client
		n, cAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			continue
		}

		p, err := ParsePacket(buffer[:n])
		if err != nil {
			fmt.Println("bad packet ignored")
			continue
		}

		if p.Type == TYPE_SYN {
			fmt.Printf("Got SYN from %v\n", cAddr)
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
	fmt.Println("Sending SYNACK...")

	// Retransmission loop for SYNACK
	handshakeStart := time.Now()
	timeoutDur := time.Duration(cfg.Timeout) * time.Second
	gotAck := false

	for !gotAck {
		if time.Since(handshakeStart) > timeoutDur {
			return fmt.Errorf("timeout waiting for handshake ACK")
		}

		conn.WriteToUDP(synack.ToBytes(), clientAddr)

		// wait for ACK with short timeout
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			// timeout, loop and retransmit SYNACK
			continue
		}

		resp, err := ParsePacket(buffer[:n])
		if err != nil {
			continue
		}

		if resp.Type == TYPE_ACK && resp.ConnId == activeConnId {
			fmt.Println("Got ACK, handshake complete")
			gotAck = true
		} else if (resp.Type == TYPE_DATA || resp.Type == TYPE_FIN) && resp.ConnId == activeConnId {
			// If clients ACK was lost but DATA or FIN arrived handshake is complete
			fmt.Printf("Got packet type %d instead of ACK, handshake complete\n", resp.Type)
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
			return fmt.Errorf("cannot create output file: %v", err)
		}
		defer f.Close()
		outFile = f
	}

	fmt.Println("Waiting for data packets...")
	
	expectedSeq := uint32(0)
	finReceived := false
	finSeq := uint32(0)
	// simple map to buffer out of order packets
	packetBuffer := make(map[uint32][]byte)

	for {
		// refresh timeout every loop
		conn.SetReadDeadline(time.Now().Add(timeoutDur))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return fmt.Errorf("timeout or error: %v", err)
		}

		p, err := ParsePacket(buffer[:n])
		if err != nil {
			fmt.Println("Bad packet, ignore")
			continue
		}

		if p.ConnId != activeConnId {
			fmt.Println("Packet from wrong connection")
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
				// packet from future, save it
				// MUST copy because buffer array will be overwritten
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
			fmt.Printf("Got FIN for seq %d, buffering until all data is written\n", finSeq)
		}

		// Check if we can safely teardown
		if finReceived && len(packetBuffer) == 0 && expectedSeq >= finSeq {
			fmt.Println("All data written, sending FINACK")
			finack := Packet{
				Magic:  MAGIC_BYTE,
				Type:   TYPE_FINACK,
				ConnId: activeConnId,
				SeqNum: 0,
				AckNum: expectedSeq,
			}
			conn.WriteToUDP(finack.ToBytes(), clientAddr)
			fmt.Println("Sent FINACK, entering TIME_WAIT...")
			
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
					// client didn't get our FINACK, send it again
					conn.WriteToUDP(finack.ToBytes(), clientAddr)
					fmt.Println("Retransmitted FINACK")
				}
			}
			
			fmt.Println("Server goodbye")
			break
		}
	}

	return nil
}
