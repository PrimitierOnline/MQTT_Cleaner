package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"strings"
	"time"

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
	verify := flag.Bool("verify", false, "Generate & clear retained messages under topic and verify deletion")
	pollute := flag.Bool("pollute", false, "Generate retained messages under topic to test cleanup")
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

	// --pollute オプション
	if *pollute {
		rand.Seed(time.Now().UnixNano())
		const n = 5 // より多くのメッセージを生成
		prefix := cfg.Topic + "/pollute"
		topics := make([]string, 0, n)

		// ランダムなトピックに対してretainedメッセージを生成
		for i := 0; i < n; i++ {
			// より深い階層のトピックを生成
			depth := rand.Intn(3) + 1 // 1-3の深さ
			topicParts := make([]string, depth)
			for j := 0; j < depth; j++ {
				topicParts[j] = fmt.Sprintf("level%d", rand.Int63())
			}
			topic := prefix + "/" + strings.Join(topicParts, "/")

			// ランダムなペイロードを生成
			payload := fmt.Sprintf("pollute_%d_%s", i, time.Now().Format("20060102150405"))

			if tok := client.Publish(topic, cfg.QoS, true, payload); tok.Wait() && tok.Error() != nil {
				log.Printf("Failed to publish retained to %s: %v", topic, tok.Error())
				continue
			}
			topics = append(topics, topic)
			fmt.Printf("Published retained message to: %s (payload: %s)\n", topic, payload)
		}

		// 生成したトピックの検証
		found := make(map[string]struct{})
		handler := func(_ mqtt.Client, msg mqtt.Message) {
			if msg.Retained() {
				found[msg.Topic()] = struct{}{}
			}
		}

		// 生成したトピックをサブスクライブして検証
		for _, topic := range topics {
			client.Subscribe(topic, cfg.QoS, handler)
		}
		time.Sleep(2 * time.Second)

		// 検証結果の表示
		fmt.Println("\nVerification results:")
		allPublished := true
		for _, topic := range topics {
			if _, exists := found[topic]; !exists {
				fmt.Printf("❌ Failed to publish retained on: %s\n", topic)
				allPublished = false
			} else {
				fmt.Printf("✅ Successfully published retained on: %s\n", topic)
			}
		}

		if allPublished {
			fmt.Println("\nAll retained messages successfully published!")
			fmt.Println("You can now use the cleanup function to remove these messages.")
		} else {
			fmt.Println("\nSome retained messages could not be published.")
		}
		return
	}

	// --test オプション
	if *test {
		testTopic := cfg.Topic + "/test"
		tok := client.Publish(testTopic, cfg.QoS, false, "connection test")
		tok.Wait()
		if err := tok.Error(); err != nil {
			fmt.Printf("Test publish error: %v\n", err)
		} else {
			fmt.Println("Test message published to:", testTopic)
		}
		return
	}

	// --verify オプション
	if *verify {
		rand.Seed(time.Now().UnixNano())
		const n = 3
		prefix := cfg.Topic + "/verify"
		topics := make([]string, 0, n)

		// ランダムなTOPICに対して検証用データをpublish
		for i := 0; i < n; i++ {
			topic := fmt.Sprintf("%s/%d", prefix, rand.Int63())
			payload := fmt.Sprintf("verify%d", i)
			if tok := client.Publish(topic, cfg.QoS, true, payload); tok.Wait() && tok.Error() != nil {
				log.Fatalf("Failed to publish retained to %s: %v", topic, tok.Error())
			}
			topics = append(topics, topic)
			fmt.Println("Published retained message to:", topic)
		}

		// 削除
		found := make(map[string]struct{})
		handler := func(_ mqtt.Client, msg mqtt.Message) {
			if msg.Retained() {
				found[msg.Topic()] = struct{}{}
			}
		}
		// prefix配下をsubscribeして retained topicを収集
		client.Subscribe(prefix, cfg.QoS, handler)
		client.Subscribe(prefix+"/#", cfg.QoS, handler)
		time.Sleep(2 * time.Second)
		client.Unsubscribe(prefix, prefix+"/#")

		// 検出したretainedメッセージをクリア
		for topic := range found {
			if tok := client.Publish(topic, cfg.QoS, true, ""); tok.Wait() && tok.Error() != nil {
				log.Printf("Error clearing %s: %v", topic, tok.Error())
			} else {
				fmt.Println("Cleared retained on:", topic)
			}
		}

		// 再度サブスクライブして retained が残っていないか確認
		cleared := make(map[string]bool)
		verifyHandler := func(_ mqtt.Client, msg mqtt.Message) {
			if msg.Retained() {
				cleared[msg.Topic()] = true
			}
		}
		for _, t := range topics {
			client.Subscribe(t, cfg.QoS, verifyHandler)
		}
		time.Sleep(1 * time.Second)
		client.Unsubscribe(topics...)

		// 検証結果表示
		allCleared := true
		for _, t := range topics {
			if cleared[t] {
				fmt.Printf("❌ Retained still present on %s\n", t)
				allCleared = false
			} else {
				fmt.Printf("✅ No retained on %s\n", t)
			}
		}
		if allCleared {
			fmt.Println("All retained messages successfully cleared!")
		}
		return
	}

	// 通常の retained メッセージ探索＆クリア
	found := make(map[string]struct{})
	handler := func(_ mqtt.Client, msg mqtt.Message) {
		if msg.Retained() {
			found[msg.Topic()] = struct{}{}
		}
	}

	baseFilter := cfg.Topic
	subFilter := cfg.Topic + "/#"
	client.Subscribe(baseFilter, cfg.QoS, handler)
	client.Subscribe(subFilter, cfg.QoS, handler)
	fmt.Println("Collecting retained topics (waiting 5s)...")
	time.Sleep(5 * time.Second) // 待機時間を延長
	client.Unsubscribe(baseFilter, subFilter)

	// 削除処理の実行
	cleared := make(map[string]bool)
	for topic := range found {
		// 最大3回まで再試行
		for retry := 0; retry < 3; retry++ {
			tok := client.Publish(topic, cfg.QoS, true, "")
			tok.Wait()
			if err := tok.Error(); err != nil {
				fmt.Printf("Error clearing %s (attempt %d): %v\n", topic, retry+1, err)
				time.Sleep(time.Second) // 再試行前に待機
				continue
			}
			cleared[topic] = true
			fmt.Println("Cleared retained message on topic:", topic)
			break
		}
	}

	// 削除の検証
	verifyHandler := func(_ mqtt.Client, msg mqtt.Message) {
		if msg.Retained() {
			cleared[msg.Topic()] = false
		}
	}

	// 検証用のサブスクリプション
	topics := make([]string, 0, len(found))
	for topic := range found {
		topics = append(topics, topic)
		client.Subscribe(topic, cfg.QoS, verifyHandler)
	}
	time.Sleep(2 * time.Second)
	client.Unsubscribe(topics...)

	// 結果の表示
	allCleared := true
	for topic, isCleared := range cleared {
		if !isCleared {
			fmt.Printf("❌ Failed to clear retained message on: %s\n", topic)
			allCleared = false
		}
	}

	if len(found) == 0 {
		fmt.Println("No retained messages found under:", cfg.Topic)
	} else if allCleared {
		fmt.Println("All retained messages successfully cleared!")
	} else {
		fmt.Println("Some retained messages could not be cleared.")
	}
}
