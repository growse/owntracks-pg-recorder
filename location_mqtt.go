package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/eclipse/paho.mqtt.golang"
	"github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"strings"
	"time"
)

type MQTTMsg struct {
	MessageId            *string            `json:"_id"`
	Type                 string             `json:"_type" binding:"required"`
	TrackerId            string             `json:"tid"`
	Accuracy             float32            `json:"acc"`
	VerticalAccuracy     float32            `json:"vac"`
	Battery              int                `json:"batt"`
	Connection           string             `json:"conn"`
	Doze                 ConvertibleBoolean `json:"doze"`
	Latitude             float64            `json:"lat"`
	Longitude            float64            `json:"lon"`
	Speed                float32            `json:"vel"`
	Altitude             float32            `json:"alt"`
	DeviceTimestampAsInt int64              `json:"tst" binding:"required"`
	DeviceTimestamp      time.Time
	User                 string
	Device               string
}

func (env *Env) SubscribeMQTT(quit <-chan bool) error {
	log.Info("Connecting to MQTT")
	mqtt.ERROR = log.StandardLogger()

	var mqttClientOptions = mqtt.NewClientOptions()
	if env.configuration.MQTTURL != "" {
		mqttClientOptions.AddBroker(env.configuration.MQTTURL)
	} else {
		mqttClientOptions.AddBroker("tcp://localhost:1883")
	}
	if env.configuration.MQTTUsername != "" && env.configuration.MQTTPassword != "" {
		log.WithField("mqttUsername", env.configuration.MQTTUsername).Info("Authenticating to MQTT")
		mqttClientOptions.SetUsername(env.configuration.MQTTUsername)
		mqttClientOptions.SetPassword(env.configuration.MQTTPassword)
	} else {
		log.Info("Anon MQTT auth")
	}
	mqttClientOptions.CleanSession = false
	mqttClientOptions.ResumeSubs = true
	mqttClientOptions.SetAutoReconnect(true)
	mqttClientOptions.ClientID = env.configuration.MQTTClientId
	mqttClientOptions.SetConnectionLostHandler(connectionLostHandler)
	mqttClientOptions.SetReconnectingHandler(reconnectingHandler)
	mqttClientOptions.AutoAckDisabled = true

	mqttClientOptions.SetOnConnectHandler(func(client mqtt.Client) {
		err := subscribeToMQTT(client, env.configuration.MQTTTopic, env.handler)
		if err != nil {
			log.WithField("topic", env.configuration.MQTTTopic).WithError(err).Error("Unable to subscribe to MQTT topic")
		}
	})
	mqttClient := mqtt.NewClient(mqttClientOptions)

	mqttClientToken := mqttClient.Connect()
	defer mqttClient.Disconnect(250)

	if mqttClientToken.Wait() && mqttClientToken.Error() != nil {
		log.WithError(mqttClientToken.Error()).Error("Error connecting to mqtt")
		return mqttClientToken.Error()
	}
	log.Info("MQTT Connected")

	select {
	case <-quit:
		mqttClient.Disconnect(100)
		log.Info("Closing MQTT")
		return nil
	}
}

func subscribeToMQTT(mqttClient mqtt.Client, topic string, handler mqtt.MessageHandler) error {
	qos := byte(1)
	log.WithField("topic", topic).WithField("qos", qos).Info("MQTT Subscribing")
	mqttSubscribeToken := mqttClient.Subscribe(topic, qos, handler)
	if mqttSubscribeToken.Wait() && mqttSubscribeToken.Error() != nil {
		log.WithError(mqttSubscribeToken.Error()).Error("Error connecting to mqtt")
		mqttClient.Disconnect(250)
		return mqttSubscribeToken.Error()
	}
	log.WithField("topic", topic).Info("MQTT Subscribed")
	return nil
}

var connectionLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	log.WithError(err).Error("MQTT Connection lost")
}

var reconnectingHandler mqtt.ReconnectHandler = func(client mqtt.Client, options *mqtt.ClientOptions) {
	log.Info("MQTT Reconnecting")
}

func filterUsersContainsUser(filterUsers string, user string) bool {
	for _, part := range strings.Split(filterUsers, ",") {
		if part == user {
			return true
		}
	}
	return false
}

func (env *Env) handler(client mqtt.Client, msg mqtt.Message) {
	log.WithField("topic", msg.Topic()).
		WithField("qos", msg.Qos()).
		WithField("retained", msg.Retained()).
		Info("Received mqtt message")
	var locationMessage MQTTMsg
	err := json.Unmarshal(msg.Payload(), &locationMessage)

	if err != nil {
		log.WithError(err).WithField("payload", msg.Payload()).Error("Error decoding MQTT message")
		msg.Ack()
		return
	}
	if locationMessage.Type != "location" {
		log.WithField("msgType", locationMessage.Type).WithField("topic", msg.Topic()).Info("Skipping received message")
		msg.Ack()
		return
	}

	locationMessage.DeviceTimestamp = time.Unix(locationMessage.DeviceTimestampAsInt, 0)
	topicParts := strings.Split(msg.Topic(), "/")

	if len(topicParts) == 2 {
		locationMessage.User = topicParts[1]
	} else if len(topicParts) > 2 {
		locationMessage.Device = topicParts[len(topicParts)-1]
		locationMessage.User = topicParts[len(topicParts)-2]
	}
	if env.configuration.FilterUsers != "" && !filterUsersContainsUser(env.configuration.FilterUsers, locationMessage.User) {
		log.WithField("user", locationMessage.User).Info("Message from user not in filterUsers list. Skipping")
		msg.Ack()
		return
	}
	log.WithField("timestamp", locationMessage.DeviceTimestamp.String()).WithField("messageId", locationMessage.MessageId).Info("Inserting into database")
	err = env.insertLocationToDatabase(locationMessage)
	if err != nil {
		var dbErr *pq.Error
		if errors.As(err, &dbErr) {
			if dbErr.Code.Class().Name() == "integrity_constraint_violation" {
				log.WithError(dbErr).Warn("Could not insert location: integrity_constraint_violation")
				msg.Ack()
			} else {
				log.WithError(dbErr).
					WithField("errorCode", dbErr.Code).
					WithField("errorName", dbErr.Code.Name()).
					Error("Unable to write location to database")
			}
		}
	} else {
		msg.Ack()
		log.Info("Inserted into database")
	}
}

func (env *Env) insertLocationToDatabase(locator MQTTMsg) error {
	ctx := context.Background()
	ctx, cancelFn := context.WithTimeout(ctx, 5*time.Second)
	defer timeTrack(time.Now())
	defer cancelFn()
	dozebool := bool(locator.Doze)
	var lastInsertId int
	err := env.db.QueryRowContext(ctx, `insert into locations
(timestamp, devicetimestamp, accuracy, doze, batterylevel, connectiontype, point, altitude, verticalaccuracy, speed,
 "user", device)
values ($1, $2, $3, $4, $5, $6, ST_SetSRID(ST_MakePoint($7, $8), 4326), $9, $10, $11, $12, $13)
RETURNING id`,

		time.Now(), locator.DeviceTimestamp, locator.Accuracy, dozebool, locator.Battery, locator.Connection, locator.Longitude, locator.Latitude, locator.Altitude, locator.VerticalAccuracy, locator.Speed, locator.User, locator.Device).Scan(&lastInsertId)

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		log.WithField("id", lastInsertId).WithField("messageId", locator.MessageId).Debug("Inserted database location")
		GeocodingWorkQueue <- lastInsertId
	} else {
		return err
	}
	return nil
}
