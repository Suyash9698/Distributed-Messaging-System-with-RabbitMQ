package main

import (
	"encoding/json"

	direct_exchange "rabbitmq/direct_exchange"
	headers_exchange "rabbitmq/headers_exchange"
	pubsub "rabbitmq/pubsub"
	topic_exchange "rabbitmq/topic_exchange"
	work_queue_model "rabbitmq/work_queue_model"
)

func publishDirect(task, rk string) error {
	return direct_exchange.PublishDirect(task, rk)
}

func publishTopic(task, rk string) error {
	return topic_exchange.PublishTopic(task, rk)
}

func publishFanout(task string) error {
	return pubsub.PublishFanout(task)
}

func publishHeaders(task, headersJSON string) error {
	var headers map[string]interface{}
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		return err
	}
	return headers_exchange.PublishHeaders(task, headers)
}

func publishToResilientQueue(task string) error {
	return work_queue_model.PublishToTaskQueue(task)
}
