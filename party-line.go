package main

import (
	"bufio"
	// "bytes"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"github.com/kevinburke/nacl"
	"github.com/kevinburke/nacl/box"
	"github.com/kevinburke/nacl/sign"
	// "io"
	"log"
	"net"
	"strconv"
	// "sync"
	"fmt"
	"time"
)

/*
TODO:
	send table
	announce
	pulse
	disconnect
	private message

	private channel
	advertise file
	advertise shared file

	fix byte array that doesn't encode
*/

type Self struct {
	ID      string
	Handle  string
	EncPub  nacl.Key
	EncPrv  nacl.Key
	SignPub sign.PublicKey
	SignPrv sign.PrivateKey
	Address string
}

type Peer struct {
	ID      string
	Handle  string
	EncPub  nacl.Key
	SignPub sign.PublicKey
	Address string
	Conn    net.Conn `json:"-"`
}

type Envelope struct {
	Type string
	From string
	To   string
	Data string
}

type MessageSuggested struct {
	Peer           Peer
	SuggestedPeers []Peer
}

type MessageChat struct {
	Chat string
	Time time.Time
}

var self Self
var peerSelf Peer

var seenChats map[string]bool
var chatChan chan string
var statusChan chan string

func getKeys() {
	r := rand.Reader
	signPub, signPrv, err := sign.Keypair(r)
	if err != nil {
		log.Fatal(err)
	}

	encPub, encPrv, err := box.GenerateKey(r)
	if err != nil {
		log.Fatal(err)
	}

	self.ID = hex.EncodeToString(signPub[:])
	self.Handle = *handleFlag
	self.SignPub = signPub
	self.SignPrv = signPrv
	self.EncPub = encPub
	self.EncPrv = encPrv
	log.Println(self.ID)

	peerSelf.ID = self.ID
	peerSelf.Handle = self.Handle
	peerSelf.SignPub = self.SignPub
	peerSelf.EncPub = self.EncPub
	peerSelf.Address = self.Address
}

func recv(address string, port uint16) {
	addr := net.UDPAddr{
		Port: int(port),
		IP:   net.ParseIP(address),
	}
	// set up listener
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		log.Fatal(err)
	}

	defer conn.Close()
	log.Println("listening...")

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			setStatus("error reading")
		}

		processMessage(line)
	}

}

var debugFlag *bool
var portFlag *uint
var handleFlag *string
var ipFlag *string
var nonatFlag *bool

func main() {
	debugFlag = flag.Bool("debug", false, "Debug.")
	portFlag = flag.Uint("port", 3499, "Port.")
	handleFlag = flag.String("handle", "anon", "Handle.")
	ipFlag = flag.String("ip", "", "Manually set external IP.")
	nonatFlag = flag.Bool("nonat", false, "Disable UPNP and PMP.")
	flag.Parse()

	// get port
	var port uint16 = uint16(*portFlag)

	// get external ip and open ports
	var extIP net.IP
	if *nonatFlag {
		if *ipFlag == "" {
			log.Fatal("Must provide an IP address with nonat flag.")
		}

		extIP = net.ParseIP(*ipFlag)
	} else {
		extIP = natStuff(port)
		defer natCleanup()
	}

	// build self info (addr, keys, id)
	portStr := strconv.FormatUint(uint64(port), 10)
	self.Address = extIP.String() + ":" + portStr
	getKeys()

	calculateIdealTableSelf(self.SignPub)
	initTable(self.SignPub)

	seenChats = make(map[string]bool)
	chatChan = make(chan string, 1)
	statusChan = make(chan string, 1)
	bsId := fmt.Sprintf("%s/%s/%s", extIP.String(), portStr, self.ID)
	log.Println(bsId)
	chatStatus(bsId)

	// var wg sync.WaitGroup
	// ctrlChan := make(chan bool, 1)

	// start network receiver
	go recv("", port)

	userInterface()
}
