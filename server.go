package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"
)

/*
map from token to the connection of the room
*/
var tokenToConn map[string]net.Conn = make(map[string]net.Conn)
var tokenGenMutex sync.Mutex

const (
	connType              = "tcp"
	maxTimePerTurnSeconds = 60
)

var connPort string

func init() {
	rand.Seed(time.Now().UnixNano())
	cmd := flag.String("port", "12345", "port on which to run server")
	flag.Parse()
	connPort = *cmd
}

//generateToken generates a unique token to be used for a game room identifier
func generateToken(conn net.Conn) string {
	tokenGenMutex.Lock()
	var tok string
	for {
		token := ""
		for i := 0; i < 5; i++ {
			token += string(rune(rand.Intn(26) + 'a'))
		}
		if _, isDup := tokenToConn[token]; !isDup {
			tok = token
			break
		}
	}
	tokenToConn[tok] = conn
	tokenGenMutex.Unlock()
	return tok
}

/*
connecion that are unused and must be closed
*/
var toClose chan net.Conn = make(chan net.Conn, 128)

func start() {
	var quickOpponent net.Conn
	//those who connect tot a room with a token
	connectors := make(chan net.Conn, 128)
	//those who created a room and wait for ssomeone to connect to them
	waiters := make(chan net.Conn, 128)
	//those who wait for a random player to connect to them
	quick := make(chan net.Conn, 128)

	// Start the server and listen for incoming connections.
	listener, err := net.Listen("tcp", ":"+connPort)

	if err != nil {
		fmt.Println("Problem in creating tcp socket")
		log.Fatal(err)
	}

	fmt.Println("listening on port " + connPort)

	go func() {
		for {
			// Listen for an incoming connection.
			conn, err := listener.Accept()

			if err != nil {
				fmt.Println("error on accepting connection")
				continue
			}

			fmt.Println("Client " + conn.RemoteAddr().String() + " connected.")

			var playerType string
			fmt.Fscan(conn, &playerType)
			if playerType == "connect" {
				connectors <- conn
			} else if playerType == "wait" {
				waiters <- conn
			} else if playerType == "quick" {
				quick <- conn
			}
		}
	}()

	var mutex sync.Mutex

	for {
		select {
		case conn := <-connectors:
			go func() {
				opponentToken := ""
				_, err := fmt.Fscan(conn, &opponentToken)
				if err != nil {
					toClose <- conn
				}
				var connectTo net.Conn

				ok := false
				// check if conn is in map, synchronized so we dont get two player to connect to one
				mutex.Lock()
				if connectTo, ok = tokenToConn[opponentToken]; ok {
					delete(tokenToConn, opponentToken)
				}
				mutex.Unlock()

				if ok {
					startGame(conn, connectTo)
				} else {
					fmt.Fprintf(conn, "wrong_token\n")
					toClose <- conn
				}
			}()
		case conn := <-waiters:
			go func() {
				token := generateToken(conn)
				_, err := fmt.Fprintf(conn, "%s\n", token)
				if err != nil {
					toClose <- conn
				}
			}()
		case conn := <-quick:
			if quickOpponent == nil {
				quickOpponent = conn
			} else {
				go startGame(conn, quickOpponent)
				quickOpponent = nil
			}
		case conn := <-toClose:
			go func() {
				conn.Close()
				fmt.Println("Client " + conn.RemoteAddr().String() + " disconnected.")
			}()
		}
	}
}

func main() {
	start()
}

//sendMsg - reads a string from the connection "from" and sent it to "to"
//if it takes more than 60 seconds return false. if the msg is "end" it means the game has end
//therefore return false
func sendMsg(from, to net.Conn) bool {
	var msg string
	c := make(chan bool)

	go func() {
		_, err := fmt.Fscan(from, &msg)
		if err != nil {
			c <- false
		}
		_, err = fmt.Fprintf(to, "%s\n", msg)
		if err != nil {
			c <- false
		}
		if msg == "end" {
			c <- false
		} else {
			c <- true
		}
	}()

	select {
	case ok := <-c:
		return ok
	case <-time.After(maxTimePerTurnSeconds * time.Second):
		return false
	}
	return false
}

func startGame(conn1, conn2 net.Conn) {
	defer func() {
		toClose <- conn1
		toClose <- conn2
	}()

	_, err2 := fmt.Fprintf(conn2, "second\n")
	if err2 != nil {
		fmt.Println(conn1, "error")
		return
	}
	_, err1 := fmt.Fprintf(conn1, "first\n")
	if err1 != nil {
		fmt.Println(conn2, "error")
		return
	}
	go func() {
		for {
			if !sendMsg(conn1, conn2) {
				return
			}
		}
	}()
	for {
		if !sendMsg(conn2, conn1) {
			return
		}
	}
}
