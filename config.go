package main

import (
	"github.com/fatih/structs"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Configuration struct {
	DbUser                 string
	DbName                 string
	DbPassword             string
	DbHost                 string
	DbSslMode              string
	GeocodeApiURL          string
	ReverseGeocodeApiURL   string
	Domain                 string
	Port                   int
	MaxDBOpenConnections   int
	MQTTURL                string
	MQTTUsername           string
	MQTTPassword           string
	MQTTClientId           string
	MQTTTopic              string
	EnableGeocodingCrawler bool
	Debug                  bool
	FilterUsers            string
	DefaultUser            string
}

func getConfiguration() *Configuration {
	viper.AutomaticEnv()
	viper.SetConfigName("owntracks-pg-recorder.toml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/owntracks-pg-recorder")
	viper.SetConfigType("toml")
	viper.SetEnvPrefix("OT_PG_RECORDER")
	//Config parsing

	if err := viper.ReadInConfig(); err != nil {
		switch err.(type) {
		default:
			log.WithError(err).Fatal("Error loading config file")
		case viper.ConfigFileNotFoundError:
			log.WithError(err).Warn("No config file, using defaults / environment")
		}
	}

	// Defaults
	config := Configuration{
		DbUser:                 "",
		DbName:                 "",
		DbPassword:             "",
		DbHost:                 "",
		DbSslMode:              "require",
		GeocodeApiURL:          "",
		ReverseGeocodeApiURL:   "",
		Domain:                 "",
		Port:                   1,
		MaxDBOpenConnections:   10,
		MQTTURL:                "",
		MQTTUsername:           "",
		MQTTPassword:           "",
		MQTTClientId:           "owntracks-pg-recorder",
		MQTTTopic:              "owntracks/#",
		EnableGeocodingCrawler: false,
		Debug:                  false,
		FilterUsers:            "",
		DefaultUser:            "",
	}

	// This hack is needed to pull in configs from Env vars
	for key := range structs.Map(config) {
		viper.Set(key, viper.Get(key))
	}

	err := viper.Unmarshal(&config)
	if err != nil {
		log.Fatalf("Unable to parse configuration file: %v", err)
	}
	return &config
}
