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
	Domain                 string
	Port                   int
	MaxDBOpenConnections   int
	MQTTURL                string
	MQTTUsername           string
	MQTTPassword           string
	MQTTClientId           string
	MQTTTopic              string
	EnableGeocodingCrawler bool
}

func getConfiguration() *Configuration {
	viper.SetConfigName("owntracks-pg-recorder.toml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/owntracks-pg-recorder")
	viper.AutomaticEnv()
	viper.SetEnvPrefix("ot_pg_recorder")
	viper.SetConfigType("toml")
	//Config parsing
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Unable to open configuration file: %v", err)
	}

	viper.SetDefault("MQTTTopic", "owntracks/#")
	viper.SetDefault("MQTTClientId", "owntracks-pg-recorder")
	viper.SetDefault("DatabaseMigrationsPath", "databasemigrations")

	var config Configuration

	err = viper.Unmarshal(&config)
	if err != nil {
		log.Fatalf("Unable to parse configuration file: %v", err)
	}
	return &config
}
