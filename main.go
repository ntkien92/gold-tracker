package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	_ "github.com/mattn/go-sqlite3"
)

type GoldPrice struct {
	Type      string    `json:"type"`
	Buy       string    `json:"buy"`
	Sell      string    `json:"sell"`
	Converted string    `json:"converted"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Config struct {
	TelegramToken  string `json:"telegram_token"`
	TelegramChatID string `json:"telegram_chat_id"`
	SlackWebhook   string `json:"slack_webhook"`
	FormatTime     string `json:"format_time"`
}

func loadConfig() Config {
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Không đọc được config.json: %v", err)
	}
	var cfg Config
	json.Unmarshal(data, &cfg)
	return cfg
}

func fetchGoldPrices() ([]GoldPrice, error) {
	url := "https://hoakimnguyen.com/tra-cuu-gia-vang/"
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		return nil, err
	}

	var prices []GoldPrice
	doc.Find("table.table.table-bordered.table-hover tr").Each(func(i int, s *goquery.Selection) {
		if s.Find("th").Length() > 0 {
			return
		}
		cells := s.Find("td")
		if cells.Length() >= 3 {
			prices = append(prices, GoldPrice{
				Type:      strings.TrimSpace(cells.Eq(0).Text()),
				Buy:       strings.TrimSpace(cells.Eq(1).Text()),
				Sell:      strings.TrimSpace(cells.Eq(2).Text()),
				Converted: strings.TrimSpace(cells.Eq(3).Text()),
				UpdatedAt: time.Now(),
			})
		}
	})
	return prices, nil
}

func saveToSQLite(prices []GoldPrice) error {
	db, err := sql.Open("sqlite3", "gold.db")
	if err != nil {
		return err
	}
	defer db.Close()

	createTable := `
	CREATE TABLE IF NOT EXISTS gold_prices (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT,
		buy TEXT,
		sell TEXT,
		converted TEXT,
		updated_at DATETIME
	);`
	_, err = db.Exec(createTable)
	if err != nil {
		return err
	}

	for _, p := range prices {
		_, err = db.Exec(`INSERT INTO gold_prices (type, buy, sell, converted, updated_at) VALUES (?, ?, ?, ?, ?)`,
			p.Type, p.Buy, p.Sell, p.Converted, p.UpdatedAt)
		if err != nil {
			log.Println("Lỗi insert:", err)
		}
	}
	return nil
}

func sendTelegram(cfg Config, prices []GoldPrice) {
	if cfg.TelegramToken == "" || cfg.TelegramChatID == "" {
		return
	}

	message := fmt.Sprintf("Giá vàng hôm nay - %s 💰\n", time.Now().Format(cfg.FormatTime))
	for _, p := range prices {
		message += fmt.Sprintf("• %s: Mua %s - Bán %s\n", p.Type, p.Buy, p.Sell)
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.TelegramToken)
	http.PostForm(url, map[string][]string{
		"chat_id":    {cfg.TelegramChatID},
		"text":       {message},
		"parse_mode": {"Markdown"},
	})
}

func sendSlack(cfg Config, prices []GoldPrice) {
	if cfg.SlackWebhook == "" {
		return
	}
	message := fmt.Sprintf("Giá vàng hôm nay - %s 💰\n", time.Now().Format(cfg.FormatTime))
	for _, p := range prices {
		message += fmt.Sprintf("• %s: Mua %s - Bán %s\n", p.Type, p.Buy, p.Sell)
	}

	payload := fmt.Sprintf(`{"text": "%s"}`, strings.ReplaceAll(message, `"`, `\"`))
	http.Post(cfg.SlackWebhook, "application/json", strings.NewReader(payload))
}

func main() {
	cfg := loadConfig()

	for {
		now := time.Now()
		var nextRun time.Time

		morning := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())
		afternoon := time.Date(now.Year(), now.Month(), now.Day(), 15, 0, 0, 0, now.Location())

		if now.Before(morning) {
			nextRun = morning
		} else if now.Before(afternoon) {
			nextRun = afternoon
		} else {
			nextRun = morning.Add(24 * time.Hour)
		}

		wait := time.Until(nextRun)
		log.Printf("⏳ Chờ tới %v để chạy cron...", nextRun.Format("15:04 02/01/2006"))
		time.Sleep(wait)

		prices, err := fetchGoldPrices()
		if err != nil {
			log.Println("❌ Lỗi lấy dữ liệu:", err)
			continue
		}

		data, _ := json.MarshalIndent(prices, "", "  ")
		os.WriteFile("latest.json", data, 0644)
		saveToSQLite(prices)
		sendTelegram(cfg, prices)
		sendSlack(cfg, prices)

		log.Println("✅ Cập nhật giá vàng thành công:", time.Now())
	}
}
