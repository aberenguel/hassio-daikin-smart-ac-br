package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/crossworth/daikin"
	"github.com/crossworth/daikin/aws"
	"github.com/crossworth/daikin/iotalabs"
	"github.com/tidwall/gjson"

	"github.com/aberenguel/hassio-daikin-smart-ac-br/app/config"

	daikin2 "github.com/billbatista/ha-daikin-smart-ac-br/daikin"
	"github.com/billbatista/ha-daikin-smart-ac-br/ha"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

const configFilePath = "/config/config.yaml"
const daikinApiPort = 15914

type Thing struct {
	ID        string `json:"ID"`
	Name      string `json:"Name"`
	APN       string `json:"APN"`
	SecretKey string `json:"secretKey"`
	Heat      bool   `json:"heat"`
}

func main() {

	shouldReloadThings := false

	// Read configuration
	c, err := config.Read(configFilePath)
	if err != nil {
		slog.Info("No config file exists. A new one will be created at " + configFilePath)
		c = config.New()
		shouldReloadThings = true
	}

	// Mark existing devices
	seen := make(map[string]bool)
	for _, device := range c.Devices {
		seen[device.ThingId] = true
	}

	// Fill MQTT configuration if necessary
	if c.Mqtt.Address == "" && c.Mqtt.Username == "" && c.Mqtt.Password == "" {
		c.Mqtt.Address = os.Getenv("DAIKINBR_CONFIG_MQTT_ADDRESS")
		c.Mqtt.Username = os.Getenv("DAIKINBR_CONFIG_MQTT_USER")
		c.Mqtt.Password = os.Getenv("DAIKINBR_CONFIG_MQTT_PASSWORD")
	}

	if !shouldReloadThings {
		shouldReloadThings, _ = strconv.ParseBool(os.Getenv("DAIKINBR_CONFIG_RELOAD_THINGS"))
	}

	if shouldReloadThings {
		reloadThings(c)
	}

	// check addresses
	shouldReloadAddresses := false
	for _, d := range c.Devices {
		if d.Address == "" {
			shouldReloadAddresses = true
		}
	}

	if !shouldReloadAddresses {
		shouldReloadAddresses, _ = strconv.ParseBool(os.Getenv("DAIKINBR_CONFIG_RELOAD_ADDRESSES"))
	}

	if shouldReloadAddresses {
		reloadAddresses(c)
	}

	// write configuration
	if shouldReloadThings || shouldReloadAddresses {
		err = c.Write(configFilePath)
		if err != nil {
			slog.Error("Error writing config file:"+configFilePath, slog.Any("error", err))
			os.Exit(1)
		}
	}

	// Starts the server
	err = startServer(c)
	if err != nil {
		slog.Error("Error starting server", slog.Any("error", err))
		os.Exit(1)
	}
}

// getThings retrieves the list of Things from the AWS IoT Core service using the provided username and password.
// It fetches the account information, calls the ManageThing endpoint, and parses the returned data to extract the list of Things.
//
// Initially copied from https://github.com/crossworth/daikin/blob/22ece1dda915aba812c93bfcaaee9d9cd34f343a/cmd/extract-secret-key/handler.go
//
// Parameters:
// - username: The username for authenticating with the Daikin Smart AC - Brasil app.
// - password: The password for authenticating with the Daikin Smart AC - Brasil app.
//
// Return Value:
// - things: A slice of Thing structs representing the retrieved Things.
// - err: An error if any occurred during the retrieval process. If the error is due to invalid credentials, it will be logged.
func getThings(username, password string) (things []Thing, err error) {

	ctx := context.Background()

	accountInfo, err := aws.GetAccountInfo(ctx, username, password)
	if err != nil {
		slog.Error("error getting account info", slog.Any("error", err), slog.String("username", username))
		return
	}

	// call the managething endpoint
	raw, err := iotalabs.ManageThing(ctx, accountInfo.Username, accountInfo.AccessToken, accountInfo.IDToken)
	if err != nil {
		// ensure we don't log PII data
		if !strings.HasPrefix(err.Error(), "invalidResponse") {
			slog.Error("error calling ManageThing", slog.Any("error", err))
		}
		return
	}

	// parse the things
	for _, t := range gjson.Get(raw, "json_response.things").Array() {
		things = append(things, Thing{
			ID:        t.Get("thing_id").String(),
			Name:      t.Get("thing_metadata.thing_name").String(),
			APN:       t.Get("thing_metadata.thing_apn").String(),
			SecretKey: strings.TrimSuffix(t.Get("thing_metadata.thing_secret_key").String(), "\n"),
			Heat:      strings.Contains(t.Get("thing_metadata.thing_feature_data").String(), "HEAT_PUMP"),
		})
	}
	return
}

func reloadThings(c *config.Config) {

	// Fetch secretKey and devices from cloud
	daikin_email := os.Getenv("DAIKINBR_CONFIG_ACCOUNT_EMAIL")
	daikin_password := os.Getenv("DAIKINBR_CONFIG_ACCOUNT_PASSWORD")
	if daikin_email == "" || daikin_password == "" {
		log.Fatalf("Missing env vars: DAIKINBR_CONFIG_ACCOUNT_EMAIL | DAIKINBR_CONFIG_ACCOUNT_PASSWORD")
	}

	slog.Info("getting things from AWS server")

	things, err := getThings(daikin_email, daikin_password)
	if err != nil {
		log.Fatalf("Error getting things: %v", err)
	}

	// Complete configuration
	for _, thing := range things {

		d := c.LookupDeviceByThingID(thing.ID)

		if d != nil {

			slog.Info("updating device", slog.String("thingID", thing.ID), slog.String("APN", thing.APN))

			// update device
			d.Name = thing.Name
			d.APN = thing.APN
			d.SecretKey = thing.SecretKey

		} else {

			slog.Info("creating device", slog.String("thingID", thing.ID), slog.String("APN", thing.APN))

			// create new device
			d = &config.Device{
				ThingId:        thing.ID,
				MqttId:         strings.ToLower(thing.APN),
				APN:            thing.APN,
				Name:           thing.Name,
				SecretKey:      thing.SecretKey,
				FanModes:       []string{"auto", "low", "medium", "high"},
				OperationModes: []string{"auto", "off", "cool", "dry", "fan_only"},
			}

			if thing.Heat {
				d.OperationModes = append(d.OperationModes, "heat")
			}

			c.Devices = append(c.Devices, d)
		}
	}
}

func reloadAddresses(c *config.Config) {

	timeout := 10 * time.Second

	slog.Info("discovering devices in local network.", slog.Any("time", timeout))

	discoveredDevices, err := daikin.DiscoveryDevices(context.Background(), timeout)
	if err != nil {
		slog.Error("error discovering devices", slog.Any("error", err))
	} else {

		if len(discoveredDevices) == 0 {
			slog.Info("no devices were discovered in local network")
		}
		for _, discoveredDevice := range discoveredDevices {
			slog.Info("discovered device:", slog.Any("device", discoveredDevice))

			d := c.LookupDeviceByAPN(discoveredDevice.APN)
			if d != nil {
				d.Address = fmt.Sprintf("http://%s:%d", discoveredDevice.Hostname, daikinApiPort)
				slog.Info("updated device address in config", slog.Any("address", d.Address))
			} else {
				slog.Warn("device not found in config", slog.Any("device", discoveredDevice))
			}
		}
	}
}

// startServer initializes and starts the server responsible for managing the Daikin AC devices.
// It connects to MQTT broker, initializes each device, and handles device state updates and commands.
//
// Initially copied from https://github.com/billbatista/ha-daikin-smart-ac-br/blob/b5331a62e3f42690f8bf55e0f66b7d5fb8810292/cmd/server.go

// Parameters:
// c - A pointer to the configuration struct containing the necessary information for initializing the devices.
//
// Return Value:
// An error if there is an issue connecting to the MQTT broker or initializing the devices.
// nil if the server starts successfully.
func startServer(c *config.Config) error {

	mqttClient := pahomqtt.NewClient(
		pahomqtt.NewClientOptions().
			AddBroker(c.Mqtt.Address).
			SetUsername(c.Mqtt.Username).
			SetPassword(c.Mqtt.Password),
	)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		slog.Error("failed to connect to MQTT", slog.Any("error", token.Error()))
		return token.Error()
	}
	slog.Info("connected to MQTT")

	defer func() {
		slog.Info("signal caught - exiting")
		mqttClient.Disconnect(1000)
		slog.Info("shutdown complete")
	}()

	for _, d := range c.Devices {

		deviceAddress := d.Address
		if deviceAddress == "" {
			deviceAddress = fmt.Sprintf("http://%s.local.:%d", d.APN, daikinApiPort)
			slog.Warn("device address not defined. Please configure it in '/addons_config/<this_addon>/config.yaml'. Using: "+deviceAddress, slog.String("ThingID", d.ThingId), slog.String("APN", d.APN))
		}

		// fix host only define address
		if !strings.HasPrefix(deviceAddress, "http") {
			deviceAddress = "http://" + deviceAddress
		}
		if !strings.Contains(deviceAddress, ":") {
			deviceAddress = fmt.Sprintf("%s:%d", deviceAddress, daikinApiPort)
		}

		url, err := url.Parse(deviceAddress)
		if err != nil {
			slog.Error("invalid device address", slog.Any("error", err), slog.Any("device", d))
			continue
		}

		secretKey, err := base64.StdEncoding.DecodeString(d.SecretKey)
		if err != nil {
			slog.Error("invalid secret key", slog.Any("error", err), slog.Any("device", d))
			continue
		}

		slog.Info("initializing device", slog.String("ThingId", d.ThingId), slog.String("MqttID", d.MqttId), slog.Any("url", url), slog.String("name", d.Name), slog.String("APN", d.APN))

		client := daikin2.NewClient(url, secretKey)
		ac := ha.NewClimate(client, mqttClient, d.Name, d.MqttId, d.OperationModes, d.FanModes)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			ac.PublishDiscovery()
			wg.Done()
		}()
		wg.Wait()

		ctx := context.Background()

		_, err = client.State(ctx)
		if err != nil {
			slog.Error("could not get ac state", slog.Any("error", err), slog.Any("device", d))
			ac.PublishUnavailable(ctx)
		}

		if err == nil {
			go func() {
				ac.PublishAvailable()
				ac.StateUpdate(ctx)
				ac.CommandSubscriptions()
			}()
		}
		defer func() {
			ac.PublishUnavailable(ctx)
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	signal.Notify(sig, syscall.SIGTERM)

	<-sig

	return nil

}
