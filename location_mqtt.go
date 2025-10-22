package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/lib/pq"
)

const mqttDisconnectTimeoutMs = 250

//nolint:tagliatelle,tagalign
type MQTTMsg struct {
	MessageID            *string            `json:"_id"`
	Type                 string             `json:"_type" binding:"required"`
	TrackerID            string             `json:"tid"`
	Accuracy             float32            `json:"acc"`
	VerticalAccuracy     float32            `json:"vac"`
	Battery              int                `json:"batt"`
	Connection           string             `json:"conn"`
	Doze                 ConvertibleBoolean `json:"doze"`
	Latitude             float64            `json:"lat"`
	Longitude            float64            `json:"lon"`
	Speed                float32            `json:"vel"`
	Altitude             float32            `json:"alt"`
	Course               int                `json:"cog"`
	DeviceTimestampAsInt int64              `json:"tst"   binding:"required"`
	DeviceTimestamp      time.Time
	User                 string
	Device               string
}

// slogMQTTAdapter adapts slog to mqtt.Logger interface.
type slogMQTTAdapter struct{}

func (s slogMQTTAdapter) Println(v ...interface{}) {
	slog.ErrorContext(context.Background(), fmt.Sprint(v...))
}

func (s slogMQTTAdapter) Printf(format string, v ...interface{}) {
	slog.ErrorContext(context.Background(), fmt.Sprintf(format, v...))
}

func (env *Env) SubscribeMQTT(ctx context.Context) error {
	slog.InfoContext(ctx, "Connecting to MQTT")

	mqtt.ERROR = slogMQTTAdapter{}

	var mqttClientOptions = mqtt.NewClientOptions()
	if env.configuration.MQTTURL != "" {
		mqttClientOptions.AddBroker(env.configuration.MQTTURL)
	} else {
		mqttClientOptions.AddBroker("tcp://localhost:1883")
	}

	if env.configuration.MQTTUsername != "" && env.configuration.MQTTPassword != "" {
		slog.With("mqttUsername", env.configuration.MQTTUsername).
			InfoContext(ctx, "Authenticating to MQTT")
		mqttClientOptions.SetUsername(env.configuration.MQTTUsername)
		mqttClientOptions.SetPassword(env.configuration.MQTTPassword)
	} else {
		slog.InfoContext(ctx, "Anon MQTT auth")
	}

	mqttClientOptions.CleanSession = false
	mqttClientOptions.ResumeSubs = true
	mqttClientOptions.ProtocolVersion = 4
	mqttClientOptions.SetAutoReconnect(true)
	mqttClientOptions.ClientID = env.configuration.MQTTClientID
	mqttClientOptions.SetConnectionLostHandler(connectionLostHandler)
	mqttClientOptions.SetReconnectingHandler(reconnectingHandler)
	mqttClientOptions.AutoAckDisabled = true

	mqttClientOptions.SetOnConnectHandler(func(client mqtt.Client) {
		err := subscribeToMQTT(ctx, client, env.configuration.MQTTTopic, env.mqttMessageHandler)
		if err != nil {
			slog.With("topic", env.configuration.MQTTTopic).
				With("err", err).
				ErrorContext(ctx, "Unable to subscribe to MQTT topic")
		}
	})

	mqttClient := mqtt.NewClient(mqttClientOptions)

	mqttClientToken := mqttClient.Connect()
	defer mqttClient.Disconnect(mqttDisconnectTimeoutMs)

	if mqttClientToken.Wait() && mqttClientToken.Error() != nil {
		slog.With("err", mqttClientToken.Error()).
			ErrorContext(ctx, "Error connecting to mqtt")

		return mqttClientToken.Error()
	}

	slog.InfoContext(ctx, "MQTT Connected")

	<-ctx.Done()

	mqttClient.Disconnect(mqttDisconnectTimeoutMs)
	slog.InfoContext(ctx, "Closing MQTT")

	return nil
}

func subscribeToMQTT(
	ctx context.Context,
	mqttClient mqtt.Client,
	topic string,
	handler mqtt.MessageHandler,
) error {
	qos := byte(1)
	slog.With("topic", topic).
		With("qos", qos).
		InfoContext(ctx, "MQTT Subscribing")

	mqttSubscribeToken := mqttClient.Subscribe(topic, qos, handler)
	if mqttSubscribeToken.Wait() && mqttSubscribeToken.Error() != nil {
		slog.With("err", mqttSubscribeToken.Error()).
			ErrorContext(ctx, "Error connecting to mqtt")
		mqttClient.Disconnect(mqttDisconnectTimeoutMs)

		return mqttSubscribeToken.Error()
	}

	slog.With("topic", topic).InfoContext(ctx, "MQTT Subscribed")

	return nil
}

var connectionLostHandler mqtt.ConnectionLostHandler = func(_ mqtt.Client, err error) {
	slog.With("err", err).
		ErrorContext(context.Background(), "MQTT Connection lost")
}

var reconnectingHandler mqtt.ReconnectHandler = func(_ mqtt.Client, _ *mqtt.ClientOptions) {
	slog.InfoContext(context.Background(), "MQTT Reconnecting")
}

func filterUsersContainsUser(filterUsers string, user string) bool {
	for _, part := range strings.Split(filterUsers, ",") {
		if part == user {
			return true
		}
	}

	return false
}

//nolint:funlen
func (env *Env) mqttMessageHandler(_ mqtt.Client, msg mqtt.Message) {
	ctx := context.Background()
	slog.With("topic", msg.Topic()).
		With("qos", msg.Qos()).
		With("retained", msg.Retained()).
		InfoContext(ctx, "Received mqtt message")

	var locationMessage MQTTMsg

	err := json.Unmarshal(msg.Payload(), &locationMessage)
	if err != nil {
		slog.With("err", err).
			With("payload", msg.Payload()).
			ErrorContext(ctx, "Error decoding MQTT message")
		msg.Ack()

		return
	}

	if locationMessage.Type != "location" {
		slog.With("msgType", locationMessage.Type).
			With("topic", msg.Topic()).
			InfoContext(ctx, "Skipping received message")
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

	if env.configuration.FilterUsers != "" &&
		!filterUsersContainsUser(env.configuration.FilterUsers, locationMessage.User) {
		slog.With("user", locationMessage.User).
			InfoContext(ctx, "Message from user not in filterUsers list. Skipping")
		msg.Ack()

		return
	}

	slog.With("timestamp", locationMessage.DeviceTimestamp.String()).
		With("messageId", locationMessage.MessageID).
		InfoContext(ctx, "Inserting into database")

	insertFunc := func() (any, error) {
		err2 := insertToDatabase(ctx,
			env.configuration.GeocodeOnInsert,
			env.configuration.EnablePrometheus,
			env.metrics,
			locationMessage,
			msg,
			env.database,
		)

		return nil, err2
	}

	_, err = backoff.Retry(ctx, insertFunc, backoff.WithMaxElapsedTime(1*time.Minute))
	if err != nil {
		slog.With("err", err).
			With("timestamp", locationMessage.DeviceTimestamp.String()).
			With("messageId", locationMessage.MessageID).
			ErrorContext(ctx, "unable to insert location message to database")
	}
}

//nolint:funlen
func insertToDatabase(
	ctx context.Context,
	geoCodeOnInsert bool,
	enablePrometheus bool,
	metrics *Metrics,
	locationMessage MQTTMsg,
	msg mqtt.Message,
	database *sql.DB,
) error {
	ctx, cancelFn := context.WithTimeout(ctx, 5*time.Second)

	defer timeTrack(ctx, time.Now())
	defer cancelFn()

	dozeBoolean := bool(locationMessage.Doze)

	var lastInsertID int

	err := database.QueryRowContext(
		ctx,
		`insert into locations
(timestamp, devicetimestamp, accuracy, doze, batterylevel, connectiontype, point, altitude, verticalaccuracy, speed,
 "user", device, cog)
values ($1, $2, $3, $4, $5, $6, ST_SetSRID(ST_MakePoint($7, $8), 4326), $9, $10, $11, $12, $13, $14)
RETURNING id`,

		time.Now(),
		locationMessage.DeviceTimestamp,
		locationMessage.Accuracy,
		dozeBoolean,
		locationMessage.Battery,
		locationMessage.Connection,
		locationMessage.Longitude,
		locationMessage.Latitude,
		locationMessage.Altitude,
		locationMessage.VerticalAccuracy,
		locationMessage.Speed,
		locationMessage.User,
		locationMessage.Device,
		locationMessage.Course,
	).Scan(&lastInsertID)

	if ctx.Err() != nil { // We may have timed out
		slog.With("err", ctx.Err()).
			With("timestamp", locationMessage.DeviceTimestamp.String()).
			With("messageId", locationMessage.MessageID).
			ErrorContext(ctx, "Context error")

		return ctx.Err()
	}

	if err != nil { // Database error
		var dbErr *pq.Error
		if errors.As(err, &dbErr) {
			if dbErr.Code.Class().Name() == "integrity_constraint_violation" {
				// We're skipping this point
				slog.With("err", dbErr).
					WarnContext(ctx, "Could not insert location: integrity_constraint_violation")
				msg.Ack()

				return nil
			}

			slog.
				With("err", dbErr).
				With("errorCode", dbErr.Code).
				With("errorName", dbErr.Code.Name()).
				ErrorContext(ctx, "Unable to write location to database")

			return err
		}
	}

	msg.Ack()
	slog.With("id", lastInsertID).
		With("messageId", locationMessage.MessageID).
		DebugContext(ctx, "Inserted database location")

	if enablePrometheus {
		metrics.locationsReceived.Inc()
	}

	if geoCodeOnInsert {
		GeocodingWorkQueue <- lastInsertID
	}

	return nil
}
