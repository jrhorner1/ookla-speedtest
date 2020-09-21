package main

import (
	log "github.com/sirupsen/logrus"
	"time"
	_ "context"
	"github.com/influxdata/influxdb-client-go"
	"strconv"
	"github.com/jrhorner1/ookla-speedtest/pkg/speedtest"
	"gopkg.in/yaml.v2"
	"os"
	"fmt"
	"flag"
)

type Config struct {
	Influxdb struct {
		Address  string `yaml:"address"`
		Port     int    `yaml:"port"`
		Database string `yaml:"database"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"influxdb"`
	Speedtest struct {
		Server struct {
			Id   int    `yaml:"id"`
			Name string `yaml:"name"`
		}
		Interval string `yaml:"interval"`
	} `yaml:"speedtest"`
	Logging struct {
		Level string `yaml:"level"`
	} `yaml:"logging"`
}

func NewConfig(configPath string) (*Config, error) {
	config := &Config{}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	d := yaml.NewDecoder(file)

	if err := d.Decode(&config); err != nil {
		return nil, err
	}

	return config, nil
}

func ValidateCOnfigPath(path string) error {
	s, err := os.Stat(path)
	if err != nil {
		return err
	}
	if s.IsDir() {
		return fmt.Errorf("'%s' is a directory, not a file",path)
	}
	return nil
}

func ParseFlags() (string, error) {
	var configPath string
	flag.StringVar(&configPath, "config", "./config.yaml", "path to config")
	flag.Parse()
	if err := ValidateCOnfigPath(configPath); err != nil {
		return "", err
	}
	return configPath, nil
}

func influxdbConnect(results *speedtest.Speedtest, config *Config){
	influxdb_protocol := "http"
	influxdb_server := config.Influxdb.Address
	influxdb_port := config.Influxdb.Port
	influxdb_url := influxdb_protocol + "://" + influxdb_server + ":" + strconv.Itoa(influxdb_port)
	influxdb_user := config.Influxdb.Username
	influxdb_pass := config.Influxdb.Password
	influxdb_token := influxdb_user + ":" + influxdb_pass
	influxdb_org := ""
	influxdb_database := config.Influxdb.Database

	log.Info("Connecting to influxdb server: " + influxdb_url)
	client := influxdb2.NewClient(influxdb_url, influxdb_token)


	writeAPI := client.WriteAPI(influxdb_org, influxdb_database)
    errorsCh := writeAPI.Errors()
    go func() {
        for err := range errorsCh {
        	log.Error("write error:", err.Error())
        }
	}()
	
	tags := map[string]string{ 
		"serverId": strconv.Itoa(results.Server.Id),
		"serverName": results.Server.Name,
		"serverLocation": results.Server.Location,
		"serverCountry": results.Server.Country,
		"serverHost": results.Server.Host,
		"serverPort": strconv.Itoa(results.Server.Port),
		"serverIp": results.Server.Ip,
		"isp": results.Isp,
		"internalIp": results.Interface.InternalIp,
		"interfaceName": results.Interface.Name,
		"interfaceMacAddr": results.Interface.MacAddr,
		"isVpn": strconv.FormatBool(results.Interface.IsVpn),
		"externalIp": results.Interface.ExternalIp,
		"result_id": results.Result.Id,
		"result_url": results.Result.Url}

	ping := influxdb2.NewPoint(
		"ping", 	// measurement
		tags, 
		map[string]interface{}{	// fields
			"jitter": results.Ping.Jitter,
			"latency": results.Ping.Latency},
		results.Timestamp)
		
	log.Debug("Writing ping measurements to influxdb")
	writeAPI.WritePoint(ping)

	download := influxdb2.NewPoint(
		"download", 	// measurement
		tags, 
		map[string]interface{}{
			"bandwidth": results.Download.Bandwidth * 8,  // Value is in bytes, converting to bits
			"bytes": results.Download.Bytes,
			"elapsed": results.Download.Elapsed},
		results.Timestamp)
	log.Debug("Writing download measurements to influxdb")
	writeAPI.WritePoint(download)

	upload := influxdb2.NewPoint(
		"upload", 	// measurement
		tags, 
		map[string]interface{}{
			"bandwidth": results.Upload.Bandwidth * 8,  // Value is in bytes, converting to bits
			"bytes": results.Upload.Bytes,
			"elapsed": results.Upload.Elapsed},
		results.Timestamp)
	log.Debug("Writing upload measurements to influxdb")
	writeAPI.WritePoint(upload)
		
	packet := influxdb2.NewPoint(
		"packet", 	// measurement
		tags, 
		map[string]interface{}{	// fields
			"loss": results.PacketLoss},
		results.Timestamp)
	log.Debug("Writing packet measurements to influxdb")
	writeAPI.WritePoint(packet)

	log.Info("Flushing writes to influxdb from the buffer")
	writeAPI.Flush()
	log.Info("Closing the influxdb client connection")
	client.Close()
}

func main() {
	configPath, err := ParseFlags()
	if err != nil {
		log.Fatal(err)
	}
	config, err := NewConfig(configPath)
	if err != nil {
		log.Fatal(err)
	}

	switch config.Logging.Level {
	case "panic":
		log.SetLevel(log.PanicLevel)
	case "fatal":
		log.SetLevel(log.FatalLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	case "warn":
		log.SetLevel(log.WarnLevel)
	case "info":
		log.SetLevel(log.InfoLevel)
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "trace":
		log.SetLevel(log.TraceLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}

	for {
		var results *speedtest.Speedtest
		if config.Speedtest.Server.Id != 0 {
			log.Debug("Running with server id")
			results = speedtest.RunWithServerId(config.Speedtest.Server.Id)
		} else if config.Speedtest.Server.Name != "" {
			log.Debug("Running with server hostname")
			results = speedtest.RunWithHost(config.Speedtest.Server.Name)
		} else {
			log.Debug("Running with default settings")
			results = speedtest.Run()
		}
		influxdbConnect(results, config)
		log.Info("Sleeping for " + config.Speedtest.Interval + "...")
		intervalDuration, err := time.ParseDuration(config.Speedtest.Interval)
		if err != nil {
			log.Error("Sleep interval parse error:", err.Error())
		}
		log.Debug("Sleep Duration: ", intervalDuration)
		time.Sleep(intervalDuration)
	}
}