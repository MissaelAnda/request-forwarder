package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gorilla/websocket"
)

var addr = flag.String("address", "localhost:3000", "The server address")
var service = flag.String("service", "test", "The service to subscribe to")
var ssl = flag.Bool("ssl", false, "Wether the server has ssl enabled")
var forwardTo = flag.String("forward", "http://localhost:8000", "The address to forward the request to")
var forwardToUrl *url.URL

func SendRequest(payload []byte) {
	event := WebhookEvent{}
	if err := json.Unmarshal(payload, &event); err != nil {
		log.Println("Malformed payload:", err)
		return
	}

	for k := range forwardToUrl.Query() {
		delete(forwardToUrl.Query(), k)
	}

	var body io.Reader
	if event.Method == fiber.MethodGet {
		body = nil

		if event.Payload != nil {
			data := make(map[string]string)
			json.Unmarshal(event.Payload, &data)
			for key, val := range data {
				forwardToUrl.Query().Add(key, val)
			}
		}
	} else {
		body = bytes.NewReader(event.Payload)
	}

	req, err := http.NewRequest(event.Method, forwardToUrl.String(), body)
	if err != nil {
		log.Println("Error creating request:", err)
	}

	for header, value := range event.Headers {
		req.Header.Set(header, value)
	}

	if _, err := http.DefaultClient.Do(req); err != nil {
		log.Println("Error forwarding request:", err)
	}
}

func main() {
	flag.Parse()

	var err error
	if forwardToUrl, err = url.Parse(*forwardTo); err != nil {
		log.Fatal("Incorrect forward url:", err)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	scheme := "ws"
	if *ssl {
		scheme += "s"
	}

	u := url.URL{Scheme: scheme, Host: *addr, Path: "ws/" + *service}
	log.Printf("connecting to %s", u.String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	} else {
		log.Println("Successfully connected to", u.String())
	}
	defer conn.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			log.Printf("received webhook event: %s", message)
			go SendRequest(message)
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-interrupt:
			log.Println("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}

}
