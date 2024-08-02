package main

type WebhookEvent struct {
	channel *string
	Method  string              `json:"method"`
	Payload []byte              `json:"payload"`
	Query   map[string][]string `json:"query"`
	Headers map[string]string   `json:"headers"`
}
