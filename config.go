package main

import (
	"github.com/kelseyhightower/envconfig"
)

type Configuration struct {
	DbUser                 string `default:"" split_words:"false"`
	DbName                 string `default:"locations" split_words:"false"`
	DbPassword             string `default:"" split_words:"false"`
	DbHost                 string `default:"" split_words:"false"`
	DbSslMode              string `default:"require" split_words:"false"`
	GeocodeApiURL          string `default:"" split_words:"false"`
	ReverseGeocodeApiURL   string `default:"" split_words:"false"`
	Domain                 string `default:"" split_words:"false"`
	Port                   int    `default:"8080" split_words:"false"`
	MaxDBOpenConnections   int    `default:"10" split_words:"false"`
	MQTTURL                string `default:"" split_words:"false"`
	MQTTUsername           string `default:"" split_words:"false"`
	MQTTPassword           string `default:"" split_words:"false"`
	MQTTClientId           string `default:"owntracks-pg-recorder" split_words:"false"`
	MQTTTopic              string `default:"owntracks/#" split_words:"false"`
	EnableGeocodingCrawler bool   `default:"false" split_words:"false"`
	Debug                  bool   `default:"false" split_words:"false"`
	FilterUsers            string `default:"" split_words:"false"`
	DefaultUser            string `default:"" split_words:"false"`
	GeocodeOnInsert        bool   `default:"false" split_words:"true"`
	EnablePrometheus       bool   `default:"false" split_words:"true"`
}

func getConfiguration() (*Configuration, error) {
	var configuration Configuration
	err := envconfig.Process("OT_PG_RECORDER", &configuration)
	if err != nil {
		return nil, err
	}
	return &configuration, nil
}
