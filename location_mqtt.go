package main

import (
	"encoding/json"
	"github.com/eclipse/paho.mqtt.golang"
	"github.com/lib/pq"
	"log"
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
	log.Print("Connecting to MQTT")
	var mqttClientOptions = mqtt.NewClientOptions()
	if env.configuration.MQTTURL != "" {
		mqttClientOptions.AddBroker(env.configuration.MQTTURL)
	} else {
		mqttClientOptions.AddBroker("tcp://localhost:1883")
	}
	if env.configuration.MQTTUsername != "" && env.configuration.MQTTPassword != "" {
		log.Printf("Authenticating to MQTT as %v", env.configuration.MQTTUsername)
		mqttClientOptions.SetUsername(env.configuration.MQTTUsername)
		mqttClientOptions.SetPassword(env.configuration.MQTTPassword)
	} else {
		log.Print("Anon MQTT auth")
	}
	mqttClientOptions.SetClientID(env.configuration.MQTTClientId)
	mqttClientOptions.SetAutoReconnect(true)
	mqttClientOptions.SetConnectionLostHandler(connectionLostHandler)
	mqttClient := mqtt.NewClient(mqttClientOptions)

	mqttClientToken := mqttClient.Connect()
	defer mqttClient.Disconnect(250)

	if mqttClientToken.Wait() && mqttClientToken.Error() != nil {
		log.Printf("Error connecting to mqtt: %v", mqttClientToken.Error())
		return mqttClientToken.Error()
	}
	log.Print("MQTT Connected")

	err := subscribeToMQTT(mqttClient, env.configuration.MQTTTopic, env.handler)
	if err != nil {
		return err
	}

	select {
	case <-quit:
		log.Print("MQTT Unsubscribing")
		mqttUnsubscribeToken := mqttClient.Unsubscribe(env.configuration.MQTTTopic)
		if mqttUnsubscribeToken.Wait() && mqttUnsubscribeToken.Error() != nil {
			log.Printf("Error unsubscribing from mqtt: %v", mqttUnsubscribeToken.Error())
		}
		mqttClient.Disconnect(100)
		log.Print("Closing MQTT")
		return nil
	}
}

func subscribeToMQTT(mqttClient mqtt.Client, topic string, handler mqtt.MessageHandler) error {
	log.Printf("MQTT Subscribing to %v", topic)
	mqttSubscribeToken := mqttClient.Subscribe(topic, 0, handler)
	if mqttSubscribeToken.Wait() && mqttSubscribeToken.Error() != nil {
		log.Printf("Error connecting to mqtt: %v", mqttSubscribeToken.Error())
		mqttClient.Disconnect(250)
		return mqttSubscribeToken.Error()
	}
	log.Printf("MQTT Subscribed to %v", topic)
	return nil
}

var connectionLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	log.Printf("MQTT Connection lost: %v", err)
}

func (env *Env) handler(client mqtt.Client, msg mqtt.Message) {
	log.Printf("Received mqtt message from %v", msg.Topic())
	var locator MQTTMsg
	err := json.Unmarshal(msg.Payload(), &locator)

	if err != nil {
		log.Printf("Error decoding MQTT message: %v", err)
		log.Print(msg.Payload())
		return
	}
	if locator.Type != "location" {
		log.Printf("Received message is of type %v. Skipping", locator.Type)
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
	env.insertLocationToDatabase(locator)
}

func (env *Env) insertLocationToDatabase(locator MQTTMsg) {
	defer timeTrack(time.Now())
	newLocation := false
	dozebool := bool(locator.Doze)
	_, err := env.db.Exec(
		"insert into locations "+
			"(timestamp,devicetimestamp,accuracy,doze,batterylevel,connectiontype,point, altitude, verticalaccuracy, speed, \"user\", device) "+
			"values ($1,$2,$3,$4,$5,$6, ST_SetSRID(ST_MakePoint($7, $8), 4326), $9, $10, $11, $12, $13)",

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
	)

	switch i := err.(type) {
	case nil:
		newLocation = true
		break
	case *pq.Error:
		log.Printf("Pg error: %v", err)
		log.Printf("Location: %+v", locator)
	default:
		log.Printf("%T %v", err, err)
		InternalError(i)
		return
	}
	if newLocation {
		GeocodingWorkQueue <- true
	}
}
