package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Config struct {
	Broker   string `json:"broker"`
	Username string `json:"username"`
	Password string `json:"password"`
	ClientID string `json:"clientID"`
	Topic    string `json:"topic"`
	QoS      byte   `json:"qos"`
}

func loadConfig(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	// フラグ定義
	cfgPath := flag.String("config", "config.json", "Path to JSON config file")
	test := flag.Bool("test", false, "Publish a test message to topic/test instead of clearing retained")
	flag.Parse()

	// 設定ファイル読み込み
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// MQTT クライアントオプション生成
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID(cfg.ClientID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password)

	client := mqtt.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		log.Fatalf("Connect error: %v", tok.Error())
	}
	defer client.Disconnect(250)

	if *test {
		testTopic := "topic/test"
		tok := client.Publish(testTopic, cfg.QoS, false, "connection test")
		tok.Wait()
		if err := tok.Error(); err != nil {
			fmt.Printf("Test publish error: %v\n", err)
		} else {
			fmt.Println("Test message published to:", testTopic)
		}
	} else {
		// retainedクリア用：空ペイロード＋retain=true
		tok := client.Publish(cfg.Topic, cfg.QoS, true, "")
		tok.Wait()
		if err := tok.Error(); err != nil {
			fmt.Printf("Publish error: %v\n", err)
		} else {
			fmt.Println("Cleared retained message on topic:", cfg.Topic)
		}
	}
}
