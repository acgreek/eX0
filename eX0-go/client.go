package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/shurcooL/eX0/eX0-go/packet"
	"github.com/shurcooL/go-goon"
)

var hostFlag = flag.String("host", "localhost", "Server host (without port) for client to connect to.")
var nameFlag = flag.String("name", "Unnamed Player", "Local client player name.")

type client struct {
	playerId uint8

	ZOffset float32

	serverConn *Connection

	sentTimeRequestPacketTimes map[uint8]float64
	trpReceived                int
	shortestLatency            float64
	shortestLatencyLocalTime   float64
	shortestLatencyRemoteTime  float64
	finishedSyncingClock       chan struct{}

	pongSentTimes map[uint32]time.Time // PingData -> Time.
}

func startClient() *client {
	if components.logic != nil {
		components.logic.Input <- func() packet.Move {
			return packet.Move{
				MoveDirection: -1,
				Z:             components.logic.playersState[components.client.playerId].LatestPredicted().Z,
			}
		}
	}
	c := &client{
		serverConn:                 newConnection(),
		sentTimeRequestPacketTimes: make(map[uint8]float64),
		shortestLatency:            1000,
		finishedSyncingClock:       make(chan struct{}),
		pongSentTimes:              make(map[uint32]time.Time),
	}
	c.connectToServer()
	return c
}

func (c *client) connectToServer() {
	c.serverConn.dialServer()

	s := c.serverConn

	s.Signature = uint64(time.Now().UnixNano())

	{
		var p = packet.JoinServerRequest{
			TcpHeader: packet.TcpHeader{
				Type: packet.JoinServerRequestType,
			},
			Version:    1,
			Passphrase: [16]byte{'s', 'o', 'm', 'e', 'r', 'a', 'n', 'd', 'o', 'm', 'p', 'a', 's', 's', '0', '1'},
			Signature:  s.Signature,
		}

		p.Length = 26

		var buf bytes.Buffer
		err := binary.Write(&buf, binary.BigEndian, &p)
		if err != nil {
			panic(err)
		}
		err = sendTcpPacket(s, buf.Bytes())
		if err != nil {
			panic(err)
		}
	}

	{
		buf, _, err := receiveTcpPacket(s)
		if err != nil {
			panic(err)
		}
		var r packet.JoinServerAccept
		err = binary.Read(buf, binary.BigEndian, &r)
		if err != nil {
			panic(err)
		}
		goon.Dump(r)

		state.Lock()
		c.playerId = r.YourPlayerId
		components.logic.TotalPlayerCount = r.TotalPlayerCount + 1
		s.JoinStatus = ACCEPTED
		state.Unlock()
	}

	// Upgrade connection to UDP at this point, start listening for UDP packets.
	go c.handleUdp(s)

	{
		// TODO: Try sending multiple times, else it might not get received.
		var p packet.Handshake
		p.Type = packet.HandshakeType
		p.Signature = s.Signature

		var buf bytes.Buffer
		err := binary.Write(&buf, binary.BigEndian, &p)
		if err != nil {
			panic(err)
		}
		err = sendUdpPacket(s, buf.Bytes())
		if err != nil {
			panic(err)
		}
	}

	{
		buf, _, err := receiveTcpPacket(s)
		if err != nil {
			panic(err)
		}
		var r packet.UdpConnectionEstablished
		err = binary.Read(buf, binary.BigEndian, &r)
		if err != nil {
			panic(err)
		}
		goon.Dump(r)

		state.Lock()
		s.JoinStatus = UDP_CONNECTED
		state.Unlock()
	}

	{
		// Start syncing the clock, send a Time Request packet every 50 ms.
		go func() {
			var sn uint8

			for ; ; time.Sleep(50 * time.Millisecond) {
				select {
				case <-c.finishedSyncingClock:
					return
				default:
				}

				var p packet.TimeRequest
				p.Type = packet.TimeRequestType
				p.SequenceNumber = sn
				sn++

				state.Lock()
				c.sentTimeRequestPacketTimes[p.SequenceNumber] = time.Since(components.logic.started).Seconds()
				state.Unlock()

				var buf bytes.Buffer
				err := binary.Write(&buf, binary.BigEndian, &p)
				if err != nil {
					panic(err)
				}
				err = sendUdpPacket(s, buf.Bytes())
				if err != nil {
					panic(err)
				}
			}
		}()
	}

	{
		var p packet.LocalPlayerInfo
		p.Type = packet.LocalPlayerInfoType
		p.NameLength = uint8(len(*nameFlag))
		p.Name = []byte(*nameFlag)
		p.CommandRate = 20
		p.UpdateRate = 20

		p.Length = 3 + uint16(len(*nameFlag))

		var buf bytes.Buffer
		err := binary.Write(&buf, binary.BigEndian, &p.TcpHeader)
		if err != nil {
			panic(err)
		}
		err = binary.Write(&buf, binary.BigEndian, &p.NameLength)
		if err != nil {
			panic(err)
		}
		err = binary.Write(&buf, binary.BigEndian, &p.Name)
		if err != nil {
			panic(err)
		}
		err = binary.Write(&buf, binary.BigEndian, &p.CommandRate)
		if err != nil {
			panic(err)
		}
		err = binary.Write(&buf, binary.BigEndian, &p.UpdateRate)
		if err != nil {
			panic(err)
		}
		err = sendTcpPacket(s, buf.Bytes())
		if err != nil {
			panic(err)
		}

		state.Lock()
		s.JoinStatus = PUBLIC_CLIENT
		state.Unlock()
	}

	{
		buf, _, err := receiveTcpPacket(s)
		if err != nil {
			panic(err)
		}
		var r packet.LoadLevel
		err = binary.Read(buf, binary.BigEndian, &r.TcpHeader)
		if err != nil {
			panic(err)
		}
		r.LevelFilename = make([]byte, r.Length)
		err = binary.Read(buf, binary.BigEndian, &r.LevelFilename)
		if err != nil {
			panic(err)
		}
		goon.Dump(r)
		goon.Dump(string(r.LevelFilename))
	}

	{
		buf, _, err := receiveTcpPacket(s)
		if err != nil {
			panic(err)
		}
		var r packet.CurrentPlayersInfo
		err = binary.Read(buf, binary.BigEndian, &r.TcpHeader)
		if err != nil {
			panic(err)
		}
		r.Players = make([]packet.PlayerInfo, components.logic.TotalPlayerCount)
		for i := range r.Players {
			var playerInfo packet.PlayerInfo
			err = binary.Read(buf, binary.BigEndian, &playerInfo.NameLength)
			if err != nil {
				panic(err)
			}

			if playerInfo.NameLength != 0 {
				playerInfo.Name = make([]byte, playerInfo.NameLength)
				err = binary.Read(buf, binary.BigEndian, &playerInfo.Name)
				if err != nil {
					panic(err)
				}

				err = binary.Read(buf, binary.BigEndian, &playerInfo.Team)
				if err != nil {
					panic(err)
				}

				if playerInfo.Team != packet.Spectator {
					playerInfo.State = new(packet.State)
					err = binary.Read(buf, binary.BigEndian, playerInfo.State)
					if err != nil {
						panic(err)
					}
				}
			}

			r.Players[i] = playerInfo
		}
		goon.Dump(r)

		if components.server == nil {
			state.Lock()
			components.logic.playersStateMu.Lock()
			for id, p := range r.Players {
				if p.NameLength == 0 {
					continue
				}
				ps := playerState{
					Name: string(p.Name),
					Team: p.Team,
				}
				if p.State != nil {
					ps.PushAuthed(sequencedPlayerPosVel{
						playerPosVel: playerPosVel{
							X: p.State.X,
							Y: p.State.Y,
							Z: p.State.Z,
						},
						SequenceNumber: p.State.CommandSequenceNumber,
					})
				}
				components.logic.playersState[uint8(id)] = ps
			}
			components.logic.playersStateMu.Unlock()
			state.Unlock()
		}
	}

	{
		buf, _, err := receiveTcpPacket(s)
		if err != nil {
			panic(err)
		}
		var r packet.EnterGamePermission
		err = binary.Read(buf, binary.BigEndian, &r)
		if err != nil {
			panic(err)
		}
		goon.Dump(r)
	}

	<-c.finishedSyncingClock

	state.Lock()
	s.JoinStatus = IN_GAME
	state.Unlock()

	{
		var p packet.EnteredGameNotification
		p.Type = packet.EnteredGameNotificationType

		p.Length = 0

		var buf bytes.Buffer
		err := binary.Write(&buf, binary.BigEndian, &p)
		if err != nil {
			panic(err)
		}
		err = sendTcpPacket(s, buf.Bytes())
		if err != nil {
			panic(err)
		}
	}

	time.Sleep(3 * time.Second)

	fmt.Println("Client connected and joining team.")
	var debugFirstJoin = true

	{
		var p packet.JoinTeamRequest
		p.Type = packet.JoinTeamRequestType
		p.Team = packet.Red

		p.Length = 1

		var buf bytes.Buffer
		err := binary.Write(&buf, binary.BigEndian, &p.TcpHeader)
		if err != nil {
			panic(err)
		}
		err = binary.Write(&buf, binary.BigEndian, &p.Team)
		if err != nil {
			panic(err)
		}
		err = sendTcpPacket(s, buf.Bytes())
		if err != nil {
			panic(err)
		}
	}

	go func() {
		for {
			buf, tcpHeader, err := receiveTcpPacket(s)
			if err != nil {
				panic(err)
			}

			switch tcpHeader.Type {
			case packet.PlayerJoinedServerType:
				var r = packet.PlayerJoinedServer{TcpHeader: tcpHeader}
				_, err = io.CopyN(ioutil.Discard, buf, packet.TcpHeaderSize)
				if err != nil {
					panic(err)
				}
				err = binary.Read(buf, binary.BigEndian, &r.PlayerId)
				if err != nil {
					panic(err)
				}
				err = binary.Read(buf, binary.BigEndian, &r.NameLength)
				if err != nil {
					panic(err)
				}
				r.Name = make([]byte, r.NameLength)
				err = binary.Read(buf, binary.BigEndian, &r.Name)
				if err != nil {
					panic(err)
				}

				if components.server == nil {
					ps := playerState{
						Name: string(r.Name),
						Team: 2,
					}
					components.logic.playersStateMu.Lock()
					components.logic.playersState[r.PlayerId] = ps
					components.logic.playersStateMu.Unlock()

					fmt.Printf("%v is entering the game.\n", ps.Name)
				}
			case packet.PlayerLeftServerType:
				var r = packet.PlayerLeftServer{TcpHeader: tcpHeader}
				_, err = io.CopyN(ioutil.Discard, buf, packet.TcpHeaderSize)
				if err != nil {
					panic(err)
				}
				err = binary.Read(buf, binary.BigEndian, &r.PlayerId)
				if err != nil {
					panic(err)
				}

				if components.server == nil {
					components.logic.playersStateMu.Lock()
					ps := components.logic.playersState[r.PlayerId]
					delete(components.logic.playersState, r.PlayerId)
					components.logic.playersStateMu.Unlock()

					fmt.Printf("%v left the game.\n", ps.Name)
				}
			case packet.PlayerJoinedTeamType:
				var r = packet.PlayerJoinedTeam{TcpHeader: tcpHeader}
				_, err = io.CopyN(ioutil.Discard, buf, packet.TcpHeaderSize)
				if err != nil {
					panic(err)
				}
				err = binary.Read(buf, binary.BigEndian, &r.PlayerId)
				if err != nil {
					panic(err)
				}
				err = binary.Read(buf, binary.BigEndian, &r.Team)
				if err != nil {
					panic(err)
				}
				if r.Team != packet.Spectator {
					r.State = new(packet.State)
					err = binary.Read(buf, binary.BigEndian, r.State)
					if err != nil {
						panic(err)
					}
				}

				state.Lock()
				components.logic.playersStateMu.Lock()
				logicTime := float64(components.logic.GlobalStateSequenceNumber) + (time.Since(components.logic.started).Seconds()-components.logic.NextTickTime)*commandRate
				fmt.Fprintf(os.Stderr, "%.3f: Pl#%v (%q) joined team %v at logic time %.2f/%v [client].\n", time.Since(components.logic.started).Seconds(), r.PlayerId, components.logic.playersState[r.PlayerId].Name, r.Team, logicTime, components.logic.GlobalStateSequenceNumber)
				components.logic.playersStateMu.Unlock()
				state.Unlock()

				if debugFirstJoin {
					debugFirstJoin = false
					r2 := r
					r2.State = &packet.State{CommandSequenceNumber: 123, X: 1.0, Y: 2.0, Z: 3.0} // Override with deterministic value so test passes.
					goon.Dump(r2)
				}

				if components.server == nil {
					components.logic.playersStateMu.Lock()
					ps := components.logic.playersState[r.PlayerId]
					if r.State != nil {
						ps.NewSeries()
						ps.PushAuthed(sequencedPlayerPosVel{
							playerPosVel: playerPosVel{
								X: r.State.X,
								Y: r.State.Y,
								Z: r.State.Z,
							},
							SequenceNumber: r.State.CommandSequenceNumber,
						})
						if r.PlayerId == c.playerId {
							c.ZOffset = 0
						}
					}
					ps.Team = r.Team
					components.logic.playersState[r.PlayerId] = ps
					components.logic.playersStateMu.Unlock()

					fmt.Printf("%v joined %v.\n", ps.Name, ps.Team)
				}
			case packet.PlayerWasHitType:
				var r = packet.PlayerWasHit{TcpHeader: tcpHeader}
				_, err = io.CopyN(ioutil.Discard, buf, packet.TcpHeaderSize)
				if err != nil {
					panic(err)
				}
				err = binary.Read(buf, binary.BigEndian, &r.PlayerId)
				if err != nil {
					panic(err)
				}
				err = binary.Read(buf, binary.BigEndian, &r.HealthGiven)
				if err != nil {
					panic(err)
				}

				fmt.Fprintf(os.Stderr, "[weapons] Player %v was hit for %v.\n", r.PlayerId, r.HealthGiven)
				// TODO: Implement.
			default:
				fmt.Println("[client] got unsupported tcp packet type:", tcpHeader.Type)
			}
		}
	}()
}

func (c *client) handleUdp(s *Connection) {
	for {
		buf, err := receiveUdpPacket(s)
		if err != nil {
			panic(err)
		}

		var udpHeader packet.UdpHeader
		err = binary.Read(buf, binary.BigEndian, &udpHeader)
		if err != nil {
			panic(err)
		}

		switch udpHeader.Type {
		case packet.TimeResponseType:
			logicTimeAtReceive := time.Since(components.logic.started).Seconds()

			var r packet.TimeResponse
			err = binary.Read(buf, binary.BigEndian, &r.SequenceNumber)
			if err != nil {
				panic(err)
			}
			err = binary.Read(buf, binary.BigEndian, &r.Time)
			if err != nil {
				panic(err)
			}

			c.trpReceived++

			if c.trpReceived <= 30 {
				state.Lock()
				latency := logicTimeAtReceive - c.sentTimeRequestPacketTimes[r.SequenceNumber]
				state.Unlock()
				if latency <= c.shortestLatency {
					c.shortestLatency = latency
					c.shortestLatencyLocalTime = logicTimeAtReceive
					c.shortestLatencyRemoteTime = r.Time + 0.5*c.shortestLatency // Remote time now is what server said plus half round-trip time.
				}
			}

			if c.trpReceived == 30 {
				if components.server == nil { // TODO: This check should be more like "if !components.logic.authoritative", and logic timer should be inside components.logic. Better yet, each of server and client components should have a pointer to logic component that they own.
					// Adjust logic clock.
					delta := c.shortestLatencyLocalTime - c.shortestLatencyRemoteTime
					state.Lock()
					components.logic.started = components.logic.started.Add(time.Duration(delta * float64(time.Second)))
					fmt.Fprintf(os.Stderr, "delta: %.3f seconds, started: %v\n", delta, components.logic.started)
					logicTime := time.Since(components.logic.started).Seconds()
					components.logic.GlobalStateSequenceNumber = uint8(logicTime * commandRate) // TODO: Adjust this.
					components.logic.NextTickTime = 0                                           // TODO: Adjust this.
					for components.logic.NextTickTime+1.0/commandRate < logicTime {
						components.logic.NextTickTime += 1.0 / commandRate
					}
					state.Unlock()
				}

				close(c.finishedSyncingClock)
			}
		case packet.PingType:
			var r packet.Ping
			err = binary.Read(buf, binary.BigEndian, &r.PingData)
			if err != nil {
				panic(err)
			}
			r.LastLatencies = make([]uint16, components.logic.TotalPlayerCount)
			err = binary.Read(buf, binary.BigEndian, &r.LastLatencies)
			if err != nil {
				panic(err)
			}

			{
				var p packet.Pong
				p.Type = packet.PongType
				p.PingData = r.PingData

				c.pongSentTimes[r.PingData] = time.Now()

				var buf bytes.Buffer
				err := binary.Write(&buf, binary.BigEndian, &p)
				if err != nil {
					panic(err)
				}
				err = sendUdpPacket(s, buf.Bytes())
				if err != nil {
					panic(err)
				}
			}
		case packet.PungType:
			localTimeAtPungReceive := time.Now()

			var r packet.Pung
			err = binary.Read(buf, binary.BigEndian, &r.PingData)
			if err != nil {
				panic(err)
			}
			err = binary.Read(buf, binary.BigEndian, &r.Time)
			if err != nil {
				panic(err)
			}

			{
				// Get the time sent of the matching Pong packet.
				if localTimeAtPongSend, ok := c.pongSentTimes[r.PingData]; ok {
					delete(c.pongSentTimes, r.PingData)

					// Calculate own latency and update it on the scoreboard.
					latency := localTimeAtPungReceive.Sub(localTimeAtPongSend)
					log.Printf("Own latency is %.5f ms.\n", latency.Seconds()*1000)
				}
			}
		case packet.ServerUpdateType:
			var r packet.ServerUpdate
			err = binary.Read(buf, binary.BigEndian, &r.CurrentUpdateSequenceNumber)
			if err != nil {
				panic(err)
			}
			r.PlayerUpdates = make([]packet.PlayerUpdate, components.logic.TotalPlayerCount)
			for i := range r.PlayerUpdates {
				var playerUpdate packet.PlayerUpdate
				err = binary.Read(buf, binary.BigEndian, &playerUpdate.ActivePlayer)
				if err != nil {
					panic(err)
				}

				if playerUpdate.ActivePlayer != 0 {
					playerUpdate.State = new(packet.State)
					err = binary.Read(buf, binary.BigEndian, playerUpdate.State)
					if err != nil {
						panic(err)
					}
				}

				r.PlayerUpdates[i] = playerUpdate
			}

			// TODO: Verify r.CurrentUpdateSequenceNumber.

			if components.server == nil {
				components.logic.playersStateMu.Lock()
				for id, pu := range r.PlayerUpdates {
					if pu.ActivePlayer != 0 {
						ps := components.logic.playersState[uint8(id)]
						ps.PushAuthed(sequencedPlayerPosVel{
							playerPosVel: playerPosVel{
								X: pu.State.X,
								Y: pu.State.Y,
								Z: pu.State.Z,
							},
							SequenceNumber: pu.State.CommandSequenceNumber,
						})
						components.logic.playersState[uint8(id)] = ps
					}
				}
				components.logic.playersStateMu.Unlock()
			}
		}
	}
}
