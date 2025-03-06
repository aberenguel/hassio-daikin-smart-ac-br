package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Mqtt    *Mqtt     `yaml:"mqtt"`
	Devices []*Device `yaml:"devices"`
}

type Mqtt struct {
	Address   string `yaml:"address"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Device struct {
	ThingId        string   `yaml:"thing_id"`
	MqttId         string   `yaml:"mqtt_id"`
	Name           string   `yaml:"name"`
	APN            string   `yaml:"apn"`
	Address        string   `yaml:"address"`
	SecretKey      string   `yaml:"secret_key"`
	OperationModes []string `yaml:"operation_modes,omitempty"`
	FanModes       []string `yaml:"fan_modes,omitempty"`
}

func New() *Config {
	return &Config{
		Mqtt: &Mqtt{},
	}
}

func Read(filePath string) (*Config, error) {
	config := &Config{}

	file, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(file, &config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func (c *Config) Write(filePath string) error {

	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	err = os.WriteFile(filePath, out, 0666)
	if err != nil {
		return err
	}

	return nil
}

func (c *Config) LookupDeviceByThingID(thingID string) *Device {
	for _, d := range c.Devices {
		if d.ThingId == thingID {
			return d
		}
	}
	return nil
}

func (c *Config) LookupDeviceByAPN(apn string) *Device {
	for _, d := range c.Devices {
		if apn == d.APN || convertZeroConfAPN(apn) == d.APN {
			return d
		}
	}
	return nil
}
