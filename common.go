package main

type WebhookEvent struct {
	channel *string
	Method  string            `json:"method"`
	Payload []byte            `json:"payload"`
	Headers map[string]string `json:"headers"`
}
