package main

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
)

type client struct {
	isClosing bool
	mu        sync.Mutex
}

type Register struct {
	channel string
	conn    *websocket.Conn
}

var channels = make(map[string]map[*websocket.Conn]*client) // Note: although large maps with pointer-like types (e.g. strings) as keys are slow, using pointers themselves as keys is acceptable and fast
var register = make(chan Register)
var broadcast = make(chan WebhookEvent)
var unregister = make(chan *websocket.Conn)

func runHub() {
	for {
		select {
		case connection := <-register:
			channel, ok := channels[connection.channel]
			if !ok {
				channel = make(map[*websocket.Conn]*client)
				channels[connection.channel] = channel
			}
			channel[connection.conn] = &client{}

		case message := <-broadcast:
			channel, ok := channels[*message.channel]
			if !ok {
				return
			}

			// Send the message to all clients
			for connection, c := range channel {
				data, err := json.Marshal(message)

				if err != nil {
					log.Println("Error marshalling broadcast payload:", err)
					return
				}

				go func(connection *websocket.Conn, c *client, data []byte) { // send to each client in parallel so we don't block on a slow client
					c.mu.Lock()
					defer c.mu.Unlock()
					if c.isClosing {
						return
					}

					if err := connection.WriteMessage(websocket.TextMessage, data); err != nil {
						c.isClosing = true
						log.Println("write error:", err)

						connection.WriteMessage(websocket.CloseMessage, []byte{})
						connection.Close()
						unregister <- connection
					}
				}(connection, c, data)
			}

		case connection := <-unregister:
			// Remove the client from the hub
			for _, channel := range channels {
				delete(channel, connection)
			}
		}
	}
}

func Send(event WebhookEvent) {
	broadcast <- event
}

func main() {
	app := fiber.New()

	app.All(":service", func(c *fiber.Ctx) error {
		service := c.Params("service")
		var payload []byte
		if c.Method() == fiber.MethodGet {
			payload, _ = json.Marshal(c.Queries())
		} else {
			payload = c.Body()
		}

		message := WebhookEvent{
			channel: &service,
			Method:  c.Method(),
			Payload: payload,
			Headers: c.GetReqHeaders(),
		}

		go Send(message)

		return c.SendStatus(200)
	})

	app.Use("ws/:service", func(c *fiber.Ctx) error {
		// IsWebSocketUpgrade returns true if the client
		// requested upgrade to the WebSocket protocol.
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})

	app.Get("/ws/:service", websocket.New(func(c *websocket.Conn) {
		defer func() {
			c.Close()
			unregister <- c
		}()

		register <- Register{
			conn:    c,
			channel: c.Params("service"),
		}

		log.Println("New websocket connection")

		for {
			if _, _, err := c.ReadMessage(); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Println("read error:", err)
				}

				return // Calls the deferred function, i.e. closes the connection on error
			}

		}
	}))

	go runHub()

	app.Listen(":3000")
}
