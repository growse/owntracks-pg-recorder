package main

import (
	"encoding/json"
	"github.com/eclipse/paho.mqtt.golang"
	log "github.com/sirupsen/logrus"
	"strings"
	"time"
)

type MQTTMsg struct {
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
	mqttClientOptions.SetClientID(env.configuration.MQTTClientId)
	mqttClientOptions.SetAutoReconnect(true)
	mqttClientOptions.SetConnectionLostHandler(connectionLostHandler)
	mqttClientOptions.SetReconnectingHandler(reconnectingHandler)
	mqttClientOptions.SetOnConnectHandler(func(client mqtt.Client) {
		err := subscribeToMQTT(client, env.configuration.MQTTTopic, env.handler)
		if err != nil {
			log.WithField("topic", env.configuration.MQTTTopic).WithError(err).Error("Unable to subscribe to MQTT topic")
		}
		log.WithField("topic", env.configuration.MQTTTopic).Info("MQTT subscribed")
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
		log.Info("MQTT Unsubscribing")
		mqttUnsubscribeToken := mqttClient.Unsubscribe(env.configuration.MQTTTopic)
		if mqttUnsubscribeToken.Wait() && mqttUnsubscribeToken.Error() != nil {
			log.WithError(mqttUnsubscribeToken.Error()).Error("Error unsubscribing from mqtt")
		}
		mqttClient.Disconnect(100)
		log.Info("Closing MQTT")
		return nil
	}
}

func subscribeToMQTT(mqttClient mqtt.Client, topic string, handler mqtt.MessageHandler) error {
	log.WithField("topic", topic).Info("MQTT Subscribing")
	mqttSubscribeToken := mqttClient.Subscribe(topic, 0, handler)
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
	log.WithField("mqttTopic", msg.Topic()).Info("Received mqtt message")
	var locator MQTTMsg
	err := json.Unmarshal(msg.Payload(), &locator)

	if err != nil {
		log.WithError(err).WithField("payload", msg.Payload()).Error("Error decoding MQTT message")
		return
	}
	if locator.Type != "location" {
		log.WithField("msgType", locator.Type).WithField("topic", msg.Topic()).Info("Skipping received message")
		return
	}

	locator.DeviceTimestamp = time.Unix(locator.DeviceTimestampAsInt, 0)
	topicParts := strings.Split(msg.Topic(), "/")

	if len(topicParts) == 2 {
		locator.User = topicParts[1]
	} else if len(topicParts) > 2 {
		locator.Device = topicParts[len(topicParts)-1]
		locator.User = topicParts[len(topicParts)-2]
	}
	if env.configuration.FilterUsers != "" && !filterUsersContainsUser(env.configuration.FilterUsers, locator.User) {
		log.WithField("user", locator.User).Info("Message from user not in filterUsers list. Skipping")
		return
	}
	env.insertLocationToDatabase(locator)
}

func (env *Env) insertLocationToDatabase(locator MQTTMsg) {
	defer timeTrack(time.Now())
	dozebool := bool(locator.Doze)
	var lastInsertId int
	err := env.db.QueryRow(
		"insert into locations "+
			"(timestamp,devicetimestamp,accuracy,doze,batterylevel,connectiontype,point, altitude, verticalaccuracy, speed, \"user\", device) "+
			"values ($1,$2,$3,$4,$5,$6, ST_SetSRID(ST_MakePoint($7, $8), 4326), $9, $10, $11, $12, $13) RETURNING id",

		time.Now(),
		locator.DeviceTimestamp,
		locator.Accuracy,
		dozebool,
		locator.Battery,
		locator.Connection,
		locator.Longitude,
		locator.Latitude,
		locator.Altitude,
		locator.VerticalAccuracy,
		locator.Speed,
		locator.User,
		locator.Device,
	).Scan(&lastInsertId)

	if err == nil {
		log.WithField("id", lastInsertId).Debug("Inserted database location")
		GeocodingWorkQueue <- lastInsertId
	} else {
		log.WithError(err).Error("Error writing location to database. Not geocoding")
	}
}
