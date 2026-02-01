package pubsub

import (
	"bytes"
	"encoding/gob"

	amqp "github.com/rabbitmq/amqp091-go"
)

func SubscribeGob[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType, // an enum to represent "durable" or "transient"
	handler func(T) Acktype,
) error {
	err := Subscribe(conn, exchange, queueName, key, queueType, handler, gobHelp)
	if err != nil {
		return err
	}
	return nil
}

func gobHelp[T any](data []byte) (T, error) {
	buf := bytes.NewBuffer(data)
	var out T
	dec := gob.NewDecoder(buf)
	err := dec.Decode(&out)
	return out, err
}
