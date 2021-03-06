package whitebox

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kevinburke/nacl/sign"
	"log"
	"math/big"
	"net"
	"time"
)

func (wb *WhiteBox) route(env *Envelope) {
	if env.Time.IsZero() {
		env.Time = time.Now().UTC()
	}

	jsonEnv, err := json.Marshal(env)
	if err != nil {
		log.Println(err)
		return
	}

	shortId, err := wb.IdFront(env.To)
	if err != nil {
		log.Println(err)
		return
	}

	bytesId, err := hex.DecodeString(shortId)
	if err != nil {
		log.Println(err)
		return
	}

	idInt := new(big.Int)
	idInt.SetBytes(bytesId)

	selfDist := new(big.Int)
	selfDist.SetBytes(wb.PeerSelf.SignPub)
	selfDist.Xor(selfDist, idInt)

	closest := wb.findClosestN(bytesId, 3)
	for _, peerEntry := range closest {
		peer := peerEntry.Peer
		if peer != nil {
			peerDist := new(big.Int)
			peerDist.SetBytes(peer.SignPub)
			peerDist.Xor(peerDist, idInt)

			if peerDist.Cmp(selfDist) < 0 {
				peer.Conn.Write([]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
			}
		}
	}
}

func (wb *WhiteBox) flood(env *Envelope) {
	jsonEnv, err := json.Marshal(env)
	if err != nil {
		log.Println(err)
		return
	}

	sentPeers := make(map[string]bool)
	wb.PeerTable.Mutex.Lock()
	for _, list := range wb.PeerTable.Table {
		for curr := list.Front(); curr != nil; curr = curr.Next() {
			currEntry := curr.Value.(*PeerEntry)
			currPeer := currEntry.Peer

			if currPeer == nil {
				continue
			}

			_, sent := sentPeers[currPeer.Id()]
			if !sent {
				if currPeer.Conn != nil {
					currPeer.Conn.Write(
						[]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
				} else {
					wb.chatStatus(fmt.Sprintf(
						"currPeer conn nil %s", currPeer.Id()))
				}
				sentPeers[currPeer.Id()] = true
			}
		}
	}
	wb.PeerTable.Mutex.Unlock()

	wb.setStatus("flooded")
}

func (wb *WhiteBox) sendSuggestionRequest(peer *Peer) {
	env := Envelope{
		Type: "request",
		From: wb.PeerSelf.Id(),
		To:   peer.Id()}

	request := MessageSuggestionRequest{
		Peer: wb.PeerSelf,
		To:   peer.Id(),
		Time: time.Now().UTC()}

	jsonReq, err := json.Marshal(request)
	if err != nil {
		log.Println(err)
		return
	}

	env.Data = sign.Sign([]byte(jsonReq), wb.Self.SignPrv)

	jsonEnv, err := json.Marshal(env)
	if err != nil {
		log.Println(err)
		return
	}

	peer.Conn.Write([]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
	wb.setStatus("suggestion request sent")
}

func (wb *WhiteBox) sendSuggestions(peer *Peer, requestData []byte) {
	env := Envelope{
		Type: "suggestions",
		From: wb.PeerSelf.Id(),
		To:   peer.Id()}

	// calculate ideal for id
	peerIdealTable := calculateIdealTable(peer.SignPub)

	// find closest for each and make a unique list
	peerSetHelper := make(map[string]bool)
	peerSet := make([]Peer, 0)
	peerSet = append(peerSet)
	for _, idInt := range peerIdealTable {
		closestPeerEntry := wb.findClosest(idInt.Bytes())
		if closestPeerEntry == nil {
			continue
		}

		if bytes.Compare(peer.SignPub, closestPeerEntry.Id) == 0 {
			continue
		}

		closestPeer := closestPeerEntry.Peer
		_, contains := peerSetHelper[closestPeer.Id()]

		if !contains {
			peerSetHelper[closestPeer.Id()] = true
			peerSet = append(peerSet, *closestPeer)
		}
	}

	// truncate
	// each encoded peer is about 300 bytes
	// this tops things off around 38kb
	// well below max udp packet size (65kb)
	if len(peerSet) > 128 {
		peerSet = peerSet[:128]
	}

	suggestions := MessageSuggestions{
		Peer:           wb.PeerSelf,
		RequestData:    requestData,
		SuggestedPeers: peerSet,
		Time:           time.Now().UTC()}

	jsonSuggestions, err := json.Marshal(suggestions)
	if err != nil {
		log.Println(err)
		return
	}

	env.Data = sign.Sign([]byte(jsonSuggestions), wb.Self.SignPrv)

	jsonEnv, err := json.Marshal(env)
	if err != nil {
		log.Println(err)
		return
	}

	peer.Conn.Write([]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
}

func (wb *WhiteBox) SendBootstrap(addr, peerId string) {
	env := Envelope{
		Type: "bootstrap",
		From: wb.PeerSelf.Id(),
		To:   peerId}

	timePeer := MessageTimePeer{
		Peer: wb.PeerSelf,
		Time: time.Now().UTC()}

	jsonBs, err := json.Marshal(timePeer)
	if err != nil {
		log.Println(err)
		return
	}

	env.Data = sign.Sign([]byte(jsonBs), wb.Self.SignPrv)

	jsonEnv, err := json.Marshal(env)
	if err != nil {
		log.Println(err)
		return
	}

	conn, err := net.Dial("udp", addr)
	if err != nil {
		log.Println(err)
		return
	}

	conn.Write([]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
	wb.setStatus("bs sent")
}

func (wb *WhiteBox) sendVerify(peer *Peer) {
	env := Envelope{
		Type: "verifybs",
		From: wb.PeerSelf.Id(),
		To:   peer.Id()}

	timePeer := MessageTimePeer{
		Peer: wb.PeerSelf,
		Time: time.Now().UTC()}

	jsonBs, err := json.Marshal(timePeer)
	if err != nil {
		log.Println(err)
		return
	}

	env.Data = sign.Sign([]byte(jsonBs), wb.Self.SignPrv)

	jsonEnv, err := json.Marshal(env)
	if err != nil {
		log.Println(err)
		return
	}

	peer.Conn.Write([]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
	wb.setStatus("verify sent")
}

func (wb *WhiteBox) SendChat(msg string) {
	env := Envelope{
		Type: "chat",
		From: wb.PeerSelf.Id(),
		To:   ""}

	msgChat := MessageChat{
		Message: msg,
		Time:    time.Now().UTC(),
		Min:     wb.PeerSelf.Min()}

	jsonChat, err := json.Marshal(msgChat)
	if err != nil {
		log.Println(err)
		return
	}

	sendPeers := make(map[string]*Peer)
	wb.PeerTable.Mutex.Lock()
	for _, list := range wb.PeerTable.Table {
		curr := list.Front()
		if curr == nil {
			continue
		}

		currEntry := curr.Value.(*PeerEntry)
		currPeer := currEntry.Peer

		sendPeers[currPeer.Id()] = currPeer
	}
	wb.PeerTable.Mutex.Unlock()

	if len(sendPeers) == 0 {
		wb.chatStatus("you have no friends, bootstrap to a peer to get started")
		return
	}

	env.Data = sign.Sign([]byte(jsonChat), wb.Self.SignPrv)
	jsonEnv, err := json.Marshal(env)
	if err != nil {
		log.Println(err)
		wb.setStatus("error marshalling env to json")
		return
	}

	for _, peer := range sendPeers {
		// closed := box.EasySeal([]byte(jsonChat), peer.EncPub, wb.Self.EncPrv)
		peer.Conn.Write([]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
	}
	wb.setStatus("chat sent")
}

func (wb *WhiteBox) sendAnnounce(peer *Peer) {
	env := Envelope{
		Type: "announce",
		From: wb.PeerSelf.Id(),
		To:   ""}

	timePeer := MessageTimePeer{
		Peer: wb.PeerSelf,
		Time: time.Now().UTC()}

	jsonAnnounce, err := json.Marshal(timePeer)
	if err != nil {
		log.Println(err)
		return
	}

	env.Data = sign.Sign([]byte(jsonAnnounce), wb.Self.SignPrv)

	jsonEnv, err := json.Marshal(env)
	if err != nil {
		log.Println(err)
		return
	}

	peer.Conn.Write([]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
	wb.setStatus("announce sent")
}

func (wb *WhiteBox) SendDisconnect() {
	env := Envelope{
		Type: "disconnect",
		From: wb.PeerSelf.Id(),
		To:   ""}

	disconnect := MessageTime{
		MessageType: -1,
		Time:        time.Now().UTC()}

	jsonDisconnect, err := json.Marshal(disconnect)
	if err != nil {
		log.Println(err)
		return
	}

	env.Data = sign.Sign([]byte(jsonDisconnect), wb.Self.SignPrv)
	wb.flood(&env)
	wb.setStatus("disconnect sent")
}

func (wb *WhiteBox) SendPings() {
	for {
		time.Sleep(time.Second * 30)
		wb.removeStalePeers()
		env := Envelope{
			Type: "ping",
			From: wb.PeerSelf.Id(),
			To:   ""}

		ping := MessagePing{
			Min:         wb.PeerSelf.Min(),
			MessageType: 0,
			Time:        time.Now().UTC()}

		jsonPing, err := json.Marshal(ping)
		if err != nil {
			log.Println(err)
			return
		}

		env.Data = sign.Sign([]byte(jsonPing), wb.Self.SignPrv)

		jsonEnv, err := json.Marshal(env)
		if err != nil {
			log.Println(err)
			return
		}

		peerSeen := make(map[string]bool)
		wb.PeerTable.Mutex.Lock()
		for i := 0; i < 256; i++ {
			bucketList := wb.PeerTable.Table[i]
			for curr := bucketList.Front(); curr != nil; curr = curr.Next() {
				entry := curr.Value.(*PeerEntry)
				if entry.Peer != nil {
					log.Println("ping peer found", entry.Peer.Id()[:6])
					_, seen := peerSeen[entry.Peer.Id()]
					if !seen {
						log.Println("pinging", entry.Peer.Id()[:6], "at", i)
						entry.Peer.Conn.Write(
							[]byte(fmt.Sprintf("%s\n", string(jsonEnv))))
						peerSeen[entry.Peer.Id()] = true
					}
				} else {
					log.Println("entry.Peer is nil at", i)
				}
			}
		}
		wb.PeerTable.Mutex.Unlock()
	}
}

func (wb *WhiteBox) sendPulse(min MinPeer) {
	env := Envelope{
		Type: "pulse",
		From: wb.PeerSelf.Id(),
		To:   min.Id()}

	pulse := MessageTime{
		MessageType: 1,
		Time:        time.Now().UTC()}

	jsonPulse, err := json.Marshal(pulse)
	if err != nil {
		log.Println(err)
		return
	}

	env.Data = sign.Sign([]byte(jsonPulse), wb.Self.SignPrv)

	wb.route(&env)
}
