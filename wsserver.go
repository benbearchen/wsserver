//+build websocket server

package main

import (
	"golang.org/x/net/websocket"

	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	reply := func(session int, msg string) {
		fmt.Printf("session %d reply: %s\n", session, msg)
	}

	wss, err := MakeWebSocketServer(":1028", reply)
	if err != nil {
		fmt.Println(err)
		return
	}

	for {
		time.Sleep(time.Second)
		wss.Broadcast("hello " + time.Now().String())
	}
}

type WebSocketServer struct {
	bind  string
	reply func(int, string)

	mutex sync.Mutex
	ss    map[int]chan<- string
}

func MakeWebSocketServer(bind string, reply func(int, string)) (*WebSocketServer, error) {
	wss := new(WebSocketServer)
	wss.bind = bind
	wss.reply = reply
	wss.ss = make(map[int]chan<- string)

	e := make(chan error)
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/demo/", http.StripPrefix("/demo/", http.FileServer(http.Dir("./"))))
		mux.Handle("/msg", websocket.Handler(wss.accept))
		err := http.ListenAndServe(bind, mux)
		if err != nil {
			e <- err
		}
	}()

	select {
	case err := <-e:
		return wss, err
	case <-time.NewTimer(time.Millisecond * 10).C:
		return wss, nil
	}
}

var (
	nextSessionID int64
)

func createSessionID() int {
	return int(atomic.AddInt64(&nextSessionID, 1))
}

func (wss *WebSocketServer) regSession(session int, c chan<- string) {
	wss.mutex.Lock()
	defer wss.mutex.Unlock()

	wss.ss[session] = c
}

func (wss *WebSocketServer) unregSession(session int) {
	wss.mutex.Lock()
	defer wss.mutex.Unlock()

	delete(wss.ss, session)
}

func (wss *WebSocketServer) accept(conn *websocket.Conn) {
	defer conn.Close()

	session := createSessionID()
	sender := make(chan string, 3)
	wss.regSession(session, sender)

	defer func() {
		wss.unregSession(session)

		close(sender)
		sender = nil
	}()

	go func() {
		for b := range sender {
			websocket.Message.Send(conn, string(b))
		}
	}()

	for {
		var b []byte
		err := websocket.Message.Receive(conn, &b)
		if len(b) > 0 {
			wss.reply(session, string(b))
		}

		if err != nil {
			if err != io.EOF {
				log.Println("websocket accept session", session, "failed:", err)
			}

			return
		}
	}
}

func (wss *WebSocketServer) sendMessage(session int, c chan<- string, msg string) {
	defer func() {
		if err := recover(); err != nil {
			log.Println("websocket send to session", session, "fail:", err)
		}
	}()

	c <- msg
}

func (wss *WebSocketServer) Broadcast(msg string) {
	wss.mutex.Lock()
	defer wss.mutex.Unlock()

	for s, c := range wss.ss {
		wss.sendMessage(s, c, msg)
	}
}
