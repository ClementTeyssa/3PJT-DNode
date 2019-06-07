package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	gonet "net"
	"net/http"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/gorilla/mux"
)

var listenPort string

type Node struct {
	PhAddr string `json:"ipAdress"`
	TxAddr string `json:"adress"`
}

type Nodes struct {
	NodesTab []Node `json:"nodes"`
}

var MyNodes Nodes

var MaxPeerPort int

type Peer struct {
	PeerAddress string `json:"PeerAddress"`
}

func (p Peer) Addr() string {
	return p.PeerAddress
}

type PeerProfile struct { // connections of one peer
	ThisPeer  Peer   `json:"ThisPeer"`  // any node
	PeerPort  int    `json:"PeerPort"`  // port of peer
	Neighbors []Peer `json:"Neighbors"` // edges to that node
	Status    bool   `json:"Status"`    // Status: Alive or Dead
	Connected bool   `json:"Connected"` // If a node is connected or not [To be used later]
}

var PeerGraph = make(map[string]PeerProfile) // Key = Node.PeerAddress; Value.Neighbors = Edges
var graphMutex sync.RWMutex
var verbose *bool
var ip *string

func init() {
	log.SetFlags(log.Lshortfile)
	verbose = flag.Bool("v", false, "enable verbose")
	ip = flag.String("ip", "global", "ip range")
	flag.Parse()
	MaxPeerPort = 3499 // starting peer port
}

func main() {
	log.Fatal(launchMUXServer())
}

func launchMUXServer() error { // launch MUX server
	mux := makeMUXRouter()
	log.Println("HTTP MUX server listening on " + GetMyIP(*ip) + ":" + listenPort) // listenPort is a global const
	s := &http.Server{
		Addr:           ":" + listenPort,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	if err := s.ListenAndServe(); err != nil {
		return err
	}
	return nil
}

func makeMUXRouter() http.Handler { // create handlers
	muxRouter := mux.NewRouter()
	muxRouter.HandleFunc("/query-p2p-graph", handleQuery).Methods("GET")
	muxRouter.HandleFunc("/enroll-p2p-net", handleEnroll).Methods("POST")
	muxRouter.HandleFunc("/port-request", handlePortReq).Methods("GET")
	muxRouter.HandleFunc("/node-addr", handleNodeAddr).Methods("POST")
	muxRouter.HandleFunc("/get-nodes", getAllNodes).Methods("GET")
	muxRouter.HandleFunc("/remove-peer", removePeer).Methods("POST")
	return muxRouter
}

func handleQuery(w http.ResponseWriter, r *http.Request) {
	log.Println("handleQuery() API called")
	graphMutex.RLock()
	defer graphMutex.RUnlock() // until the end of the handleQuery()
	bytes, err := json.Marshal(PeerGraph)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	io.WriteString(w, string(bytes))
	if *verbose {
		log.Println("PeerGraph = ", PeerGraph)
		spew.Dump(PeerGraph)
	}
}

func handleEnroll(w http.ResponseWriter, r *http.Request) {
	log.Println("handleEnroll() API called")
	w.Header().Set("Content-Type", "application/json")
	var incomingPeer PeerProfile
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&incomingPeer); err != nil {
		log.Println(err)
		respondWithJSON(w, r, http.StatusBadRequest, r.Body)
		return
	}
	defer r.Body.Close()

	_ = updatePeerGraph(incomingPeer)
	log.Println("Enroll request from:", incomingPeer.ThisPeer, "successful")
	respondWithJSON(w, r, http.StatusCreated, incomingPeer)
}

func respondWithJSON(w http.ResponseWriter, r *http.Request, code int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")

	response, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("HTTP 500: Internal Server Error"))
		return
	}
	w.WriteHeader(code)
	w.Write(response)
}

func handlePortReq(w http.ResponseWriter, r *http.Request) {
	log.Println("handlePortReq() API called")
	MaxPeerPort = MaxPeerPort + 1
	bytes, err := json.Marshal(MaxPeerPort)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	io.WriteString(w, string(bytes))
	if *verbose {
		log.Println("MaxPeerPort = ", MaxPeerPort)
		spew.Dump(MaxPeerPort)
	}
}

///// LIST OF HELPER FUNCTIONS

func updatePeerGraph(inPeer PeerProfile) error {
	if *verbose {
		log.Println("incomingPeer = ", inPeer)
		spew.Dump(PeerGraph)
	}

	// Update PeerGraph
	graphMutex.Lock()
	if *verbose {
		log.Println("PeerGraph before update = ", PeerGraph)
	}
	PeerGraph[inPeer.ThisPeer.Addr()] = inPeer
	for _, neighbor := range inPeer.Neighbors {
		profile := PeerGraph[neighbor.Addr()]
		profile.Neighbors = append(profile.Neighbors, inPeer.ThisPeer)
		PeerGraph[neighbor.Addr()] = profile
	}
	if *verbose {
		log.Println("PeerGraph after update = ", PeerGraph)
		spew.Dump(PeerGraph)
	}
	graphMutex.Unlock()
	return nil
}

func GetMyIP(ipRange string) string {
	if ipRange == "local" {
		var MyIP string
		conn, err := gonet.Dial("udp", "8.8.8.8:80")
		if err != nil {
			log.Fatalln(err)
		} else {
			localAddr := conn.LocalAddr().(*gonet.UDPAddr)
			MyIP = localAddr.IP.String()
		}
		return MyIP
	} else {
		url := "https://api.ipify.org?format=text"
		fmt.Printf("Getting IP address from ipify\n")
		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		MyIP, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		return string(MyIP)
	}

}

func handleNodeAddr(w http.ResponseWriter, r *http.Request) {
	log.Println("handleNodeAddr() API called")
	w.Header().Set("Content-Type", "application/json")
	var node Node
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&node); err != nil {
		log.Println(err)
		respondWithJSON(w, r, http.StatusBadRequest, r.Body)
		return
	}
	MyNodes.NodesTab = append(MyNodes.NodesTab, node)
	defer r.Body.Close()
	respondWithJSON(w, r, http.StatusCreated, r.Body)
}

func removePeer(w http.ResponseWriter, r *http.Request) {
	log.Println("removePeer() API called")
	w.Header().Set("Content-Type", "application/json")
	var incomingPeer PeerProfile
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&incomingPeer); err != nil {
		log.Println(err)
		respondWithJSON(w, r, http.StatusBadRequest, r.Body)
		return
	}
	_ = removePeerGraph(incomingPeer)
	var i int
	for i = 0; i < len(MyNodes.NodesTab)-1; i++ {
		if MyNodes.NodesTab[i].PhAddr == incomingPeer.ThisPeer.Addr() {
			break
		}
	}
	MyNodes.NodesTab = append(MyNodes.NodesTab[:i], MyNodes.NodesTab[i+1:]...)
}

func removePeerGraph(inPeer PeerProfile) error {
	if *verbose {
		log.Println("incomingPeer = ", inPeer)
		spew.Dump(PeerGraph)
	}

	// Update PeerGraph
	graphMutex.Lock()
	if *verbose {
		log.Println("PeerGraph before update = ", PeerGraph)
	}
	//PeerGraph[inPeer.ThisPeer.Addr()] = inPeer
	for _, neighbor := range inPeer.Neighbors {
		profile := PeerGraph[neighbor.Addr()]
		//profile.Neighbors = append(profile.Neighbors, inPeer.ThisPeer)
		var i int
		for i = 0; i < len(profile.Neighbors)-1; i++ {
			if profile.Neighbors[i] == inPeer.ThisPeer {
				break
			}
		}
		profile.Neighbors = append(profile.Neighbors[:i], profile.Neighbors[i+1:]...)
		//PeerGraph[neighbor.Addr()] = profile
		delete(PeerGraph, neighbor.Addr())
	}
	delete(PeerGraph, inPeer.ThisPeer.Addr())
	if *verbose {
		log.Println("PeerGraph after update = ", PeerGraph)
		spew.Dump(PeerGraph)
	}
	graphMutex.Unlock()
	return nil
}

func getAllNodes(w http.ResponseWriter, r *http.Request) {
	log.Println("getAllNodes() API called")
	w.Header().Set("Content-Type", "application/json")
	respondWithJSON(w, r, http.StatusCreated, MyNodes)
}
