package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	fmt.Println("Starting Peril client...")
	const connectionString = "amqp://guest:guest@localhost:5672/"

	connection, err := amqp.Dial(connectionString)
	if err != nil {
		log.Fatalf("could not connect to RabbitMQ: %v", err)
	}
	defer connection.Close()
	fmt.Println("Peril game client connected to RabbitMQ!")

	channel, err := connection.Channel()
	if err != nil {
		log.Fatalf("could not create channel: %v", err)
	}

	username, err := gamelogic.ClientWelcome()
	if err != nil {
		log.Fatalf("could not get username: %v", err)
	}
	gameState := gamelogic.NewGameState(username)

	err = pubsub.SubscribeJSON(
		connection,
		routing.ExchangePerilTopic,
		routing.ArmyMovesPrefix+"."+gameState.GetUsername(),
		routing.ArmyMovesPrefix+".*",
		pubsub.SimpleQueueTransient,
		handlerMove(channel, gameState),
	)
	if err != nil {
		log.Fatalf("could not subscribe to army moves: %v", err)
	}
	err = pubsub.SubscribeJSON(
		connection,
		routing.ExchangePerilDirect,
		routing.PauseKey+"."+gameState.GetUsername(),
		routing.PauseKey,
		pubsub.SimpleQueueTransient,
		handlerPause(gameState),
	)
	if err != nil {
		log.Fatalf("could not subscribe to pause: %v", err)
	}
	err = pubsub.SubscribeJSON(connection,
		routing.ExchangePerilTopic,
		routing.WarRecognitionsPrefix,
		routing.WarRecognitionsPrefix+".*",
		pubsub.SimpleQueueDurable,
		handlerWar(channel, gameState),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	for {
		command := gamelogic.GetInput()
		if len(command) == 0 {
			continue
		}
		switch command[0] {
		case "move":
			commandMove, err := gameState.CommandMove(command)
			if err != nil {
				fmt.Println(err)
				continue
			}

			err = pubsub.PublishJSON(
				channel,
				routing.ExchangePerilTopic,
				routing.ArmyMovesPrefix+"."+commandMove.Player.Username,
				commandMove,
			)
			if err != nil {
				fmt.Printf("error: %s\n", err)
				continue
			}
			fmt.Printf("Moved %v units to %s\n", len(commandMove.Units), commandMove.ToLocation)
		case "spawn":
			err = gameState.CommandSpawn(command)
			if err != nil {
				fmt.Println(err)
				continue
			}
		case "status":
			gameState.CommandStatus()
		case "help":
			gamelogic.PrintClientHelp()
		case "spam":
			if len(command) < 2 {
				fmt.Println("Not enough arguments")
				continue
			}
			n, err := strconv.Atoi(command[1])
			if err != nil {
				fmt.Println(err)
				continue
			}
			for i := 0; i < n; i++ {
				malice := gamelogic.GetMaliciousLog()
				err := pubsub.PublishJSON(channel, routing.ExchangePerilTopic, routing.GameLogSlug+"."+username, malice)
				if err != nil {
					fmt.Println(err)
					continue
				}
			}
		case "quit":
			gamelogic.PrintQuit()
			return
		default:
			fmt.Println("unknown command")
		}
	}
}

func handlerMove(channel *amqp.Channel, gameState *gamelogic.GameState) func(gamelogic.ArmyMove) pubsub.Acktype {
	return func(move gamelogic.ArmyMove) pubsub.Acktype {
		defer fmt.Print("> ")
		warKey := routing.WarRecognitionsPrefix + "." + gameState.GetUsername()
		moveOutcome := gameState.HandleMove(move)
		switch moveOutcome {
		case gamelogic.MoveOutcomeSamePlayer:
			return pubsub.NackDiscard
		case gamelogic.MoveOutcomeSafe:
			return pubsub.Ack
		case gamelogic.MoveOutcomeMakeWar:
			err := pubsub.PublishJSON(channel, routing.ExchangePerilTopic, warKey, gamelogic.RecognitionOfWar{
				Attacker: move.Player,
				Defender: gameState.GetPlayerSnap(),
			})
			if err != nil {
				log.Println("error publishing war message", err)
				return pubsub.NackRequeue
			}
			return pubsub.Ack
		default:
			return pubsub.NackDiscard
		}
	}
}

func handlerPause(gameState *gamelogic.GameState) func(routing.PlayingState) pubsub.Acktype {
	return func(playingState routing.PlayingState) pubsub.Acktype {
		defer fmt.Print("> ")
		gameState.HandlePause(playingState)
		return pubsub.Ack
	}
}

func handlerWar(channel *amqp.Channel, gameState *gamelogic.GameState) func(gamelogic.RecognitionOfWar) pubsub.Acktype {
	return func(war gamelogic.RecognitionOfWar) pubsub.Acktype {
		defer fmt.Println("> ")
		out, winner, loser := gameState.HandleWar(war)
		switch out {
		case gamelogic.WarOutcomeNotInvolved:
			return pubsub.NackRequeue
		case gamelogic.WarOutcomeNoUnits:
			return pubsub.NackDiscard
		case gamelogic.WarOutcomeOpponentWon:
			message := fmt.Sprintf("%s won a war against %s\n", winner, loser)
			return publishGameLog(channel, message, gameState.GetUsername())
		case gamelogic.WarOutcomeYouWon:
			message := fmt.Sprintf("%s won a war against %s\n", winner, loser)
			return publishGameLog(channel, message, gameState.GetUsername())
		case gamelogic.WarOutcomeDraw:
			message := fmt.Sprintf("A war between %s and %s resulted in a draw\n", winner, loser)
			return publishGameLog(channel, message, gameState.GetUsername())
		default:
			err := fmt.Errorf("Outcome Not Found")
			fmt.Println(err)
			return pubsub.NackDiscard
		}
	}
}

func publishGameLog(channel *amqp.Channel, message, username string) pubsub.Acktype {
	gameLog := routing.GameLog{
		CurrentTime: time.Now(),
		Message:     message,
		Username:    username,
	}

	err := pubsub.PublishGob(channel, routing.ExchangePerilTopic,
		routing.GameLogSlug+"."+username,
		gameLog)
	if err != nil {
		fmt.Println(err)
		return pubsub.NackRequeue
	}
	return pubsub.Ack
}
