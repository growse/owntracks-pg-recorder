package main

import (
	"github.com/spf13/viper"
	"log"
)

type Configuration struct {
	DbUser                 string
	DbName                 string
	DbPassword             string
	DbHost                 string
	DatabaseMigrationsPath string
	GeocodeApiURL          string
	ReverseGeocodeApiURL   string
	Production             bool
	Domain                 string
	Port                   int
	MaxDBOpenConnections   int
	MQTTURL                string `json:"mqttUrl"`
	MQTTUsername           string `json:"mqttUsername"`
	MQTTPassword           string `json:"mqttPassword"`
	MQTTClientId           string `json:"mqttClientId"`
	MQTTTopic              string `json:"mqttTopic"`
	EnableGeocodingCrawler bool
}

func getConfiguration() *Configuration {
	viper.SetConfigName("owntracks-pg-recorder.conf")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/")
	//Config parsing
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Unable to open configuration file: %v", err)
	}

	defaultConfig := &Configuration{
		DbUser:                 "",
		DbName:                 "",
		DbPassword:             "",
		DbHost:                 "",
		DatabaseMigrationsPath: "databasemigrations",
		GeocodeApiURL:          "",
		Production:             false,
		Domain:                 "",
		Port:                   8080,
		MaxDBOpenConnections:   0,
		MQTTURL:                "",
		MQTTUsername:           "",
		MQTTPassword:           "",
		MQTTClientId:           "owntracks-pg-recorder",
		MQTTTopic:              "owntracks/#",
	}
	err = viper.Unmarshal(&defaultConfig)
	if err != nil {
		log.Fatalf("Unable to parse configuration file: %v", err)
	}
	return defaultConfig
}
