package main

import (
	"context"
	"log"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/crossworth/daikin/aws"
	"github.com/crossworth/daikin/iotalabs"
	"github.com/tidwall/gjson"

	"github.com/billbatista/ha-daikin-smart-ac-br/cmd"
	"github.com/billbatista/ha-daikin-smart-ac-br/config"
)

type Thing struct {
	ThingID   string `json:"thingID"`
	ThingName string `json:"thingName"`
	SecretKey string `json:"secretKey"`
	Heat      bool   `json:"heat"`
}

func main() {

	configChanged := false

	// Read configuration
	c, err := config.NewConfig("/config/config.yaml")
	if err != nil {
		log.Print("No config file exists (/config/config.yaml). A new one will be created.")
		c = &config.Config{}
		configChanged = true
	}

	// Mark existing devices
	seen := make(map[string]bool)
	for _, device := range c.Devices {
		seen[device.SecretKey] = true
	}

	// Fill MQTT configuration if necessary
	if c.Mqtt.Host == "" && c.Mqtt.Port == "" && c.Mqtt.Username == "" && c.Mqtt.Password == "" {
		c.Mqtt.Host = os.Getenv("DAIKINBR_CONFIG_MQTT_HOST")
		c.Mqtt.Port = os.Getenv("DAIKINBR_CONFIG_MQTT_PORT")
		c.Mqtt.Username = os.Getenv("DAIKINBR_CONFIG_MQTT_USER")
		c.Mqtt.Password = os.Getenv("DAIKINBR_CONFIG_MQTT_PASSWORD")
	}

	// Fetch secretKey and devices from cloud
	daikin_email := os.Getenv("DAIKINBR_CONFIG_ACCOUNT_EMAIL")
	daikin_password := os.Getenv("DAIKINBR_CONFIG_ACCOUNT_PASSWORD")
	if daikin_email == "" || daikin_password == "" {
		log.Fatalf("Missing env vars: DAIKINBR_CONFIG_ACCOUNT_EMAIL | DAIKINBR_CONFIG_ACCOUNT_PASSWORD")
	}
	things := getThings(daikin_email, daikin_password)

	// Complete configuration
	newDevice := false
	for _, thing := range things {
		if !seen[thing.SecretKey] {
			d := config.Devices{
				Name:      thing.ThingName,
				SecretKey: thing.SecretKey,
				UniqueId:  thing.ThingID,
				FanModes:  []string{"auto", "low", "medium", "high"},
			}
			if thing.Heat {
				d.OperationModes = []string{"auto", "off", "cool", "heat", "dry", "fan_only"}
			} else {
				d.OperationModes = []string{"auto", "off", "cool", "dry", "fan_only"}
			}

			c.Devices = append(c.Devices, d)
			newDevice = true
			configChanged = true
		}
	}

	// write configuration
	if configChanged {
		writeConfig(c, "/config/config.yaml")
	}

	// Only starts server if there is no new device
	if !newDevice {
		writeConfig(c, "./config.yaml")
		cmd.Server(context.Background())
		os.Remove("./config.yaml")
	}
}

func writeConfig(c *config.Config, file string) error {

	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	err = os.WriteFile(file, out, 0)
	if err != nil {
		return err
	}

	return nil
}

func getThings(username, password string) (things []Thing) {

	ctx := context.Background()

	accountInfo, err := aws.GetAccountInfo(ctx, username, password)
	if err != nil {
		log.Printf("error getting account info: %v\n", err)
		return
	}

	// call the managething endpoint
	raw, err := iotalabs.ManageThing(ctx, accountInfo.Username, accountInfo.AccessToken, accountInfo.IDToken)
	if err != nil {
		// ensure we don't log PII data
		if !strings.HasPrefix(err.Error(), "invalidResponse") {
			log.Printf("error calling managething: %v\n", err)
		}
		return
	}

	// parse the things
	for _, t := range gjson.Get(raw, "json_response.things").Array() {
		things = append(things, Thing{
			ThingID:   t.Get("thing_id").String(),
			ThingName: t.Get("thing_metadata.thing_name").String(),
			SecretKey: strings.TrimSuffix(t.Get("thing_metadata.thing_secret_key").String(), "\n"),
			Heat:      strings.Contains(t.Get("thing_metadata.thing_feature_data").String(), "HEAT_PUMP"),
		})
	}
	return
}
