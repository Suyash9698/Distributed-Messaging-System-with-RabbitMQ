package headers_exchange

import (
	"encoding/json"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

func PublishHeaders(task, headersJSON, replyTo, corrID string) error {
	var headers map[string]interface{}
	err := json.Unmarshal([]byte(headersJSON), &headers)
	if err != nil {
		return err
	}

	if _, ok := headers["x-match"]; !ok {
		headers["x-match"] = "all"
	}

	err = Ch.ExchangeDeclare("headers_logs", "headers", true, false, false, false, nil)
	if err != nil {
		return err
	}

	err = Ch.Publish(
		"headers_logs",
		"",
		true, false,
		amqp.Publishing{
			Headers:       amqp.Table(headers),
			ContentType:   "text/plain",
			Body:          []byte(task),
			ReplyTo:       replyTo,
			CorrelationId: corrID,
		})
	if err != nil {
		return err
	}

	log.Printf("📤 Headers Publish: %s with headers %v", task, headers)
	return nil
}
