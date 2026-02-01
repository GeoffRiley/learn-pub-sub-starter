package pubsub

import (
	"fmt"
	"log"

	amqp "github.com/rabbitmq/amqp091-go"
)

func Subscribe[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	simpleQueueType SimpleQueueType,
	handler func(T) Acktype,
	unmarshaller func([]byte) (T, error),
) error {
	ch, queue, err := DeclareAndBind(conn, exchange, queueName, key, simpleQueueType)
	if err != nil {
		return err
	}

	err = ch.Qos(10, 0, false)

	if err != nil {
		log.Fatalf("could not restrict consuming logs: %v", err)
	}

	deliveryCh, err := ch.Consume(
		queue.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		return err
	}

	go func() {
		for msg := range deliveryCh {
			out, err := unmarshaller(msg.Body)
			if err != nil {
				fmt.Println(err)
				continue
			}
			ack_out := handler(out)

			switch ack_out {
			case Ack:
				msg.Ack(false)
				//fmt.Println("Ack")
			case NackRequeue:
				msg.Nack(false, true)
				//fmt.Println("NackRequeue")
			case NackDiscard:
				msg.Nack(false, false)
				//fmt.Println("NackDiscard")
			}
		}
	}()

	return nil
}
