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
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gorilla/websocket"
)

var addr = flag.String("address", "localhost:3000", "The server address")
var service = flag.String("service", "test", "The service to subscribe to")
var ssl = flag.Bool("ssl", false, "Wether the server has ssl enabled")
var logResponses = flag.Bool("log", false, "Wether to log the responses from the server")
var forwardTo = flag.String("forward", "http://localhost:8000", "The address to forward the request to")
var forwardToUrl *url.URL

func SendRequest(payload []byte) {
	log.Println("Received new event...")

	event := WebhookEvent{}
	if err := json.Unmarshal(payload, &event); err != nil {
		log.Println("Malformed payload:", err)
		return
	}

	var body io.Reader = nil
	if event.Method == fiber.MethodPost && event.Payload != nil {
		body = bytes.NewReader(event.Payload)
	}

	forwardToUrl.RawQuery = url.Values(event.Query).Encode()
	url := forwardToUrl.String()
	log.Printf("Forwarding to %s\n", url)

	req, err := http.NewRequest(event.Method, url, body)
	if err != nil {
		log.Println("Error creating request:", err)
		return
	}

	for header, value := range event.Headers {
		req.Header.Set(header, value)
	}

	if response, err := http.DefaultClient.Do(req); err != nil {
		log.Println("Error forwarding request:", err)
	} else {
		if response.StatusCode >= 400 {
			log.Printf("Request forwarded with %d error response code\n", response.StatusCode)
		} else {
			log.Printf("%s request forwarded successfully\n", event.Method)
		}

		LogResponseToFile(response, event.Method)
	}
}

func LogResponseToFile(response *http.Response, method string) {
	if !*logResponses {
		return
	}

	defer response.Body.Close()
	if body, err := io.ReadAll(response.Body); err == nil {
		fileName := method + "-" + *service + "-" + strings.ReplaceAll(time.Now().String(), " ", "_") + ".txt"
		if err := os.WriteFile(fileName, body, 0664); err != nil {
			log.Println("Failed logging error response ", err)
		} else {
			log.Println("Error response saved to ", fileName)
		}
	} else {
		log.Println("Malformed response body from request")
	}
}

func main() {
	flag.Parse()

	var err error
	if forwardToUrl, err = url.Parse(*forwardTo); err != nil {
		log.Fatal("Incorrect forward url:", err)
	} else {
		log.Println("Requests will be forwarded to", forwardToUrl)
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
		log.Fatal("Failed connection:", err)
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
			log.Printf("received webhook event")
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
			err := conn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Client interruption."),
			)

			if err != nil {
				log.Println("Error closing connection:", err)
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
