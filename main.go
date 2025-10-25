package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	_ "github.com/mattn/go-sqlite3"
)

type GoldPrice struct {
	Type      string    `json:"type"`
	Buy       int64     `json:"buy"`
	Sell      int64     `json:"sell"`
	Converted string    `json:"converted"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Config struct {
	TelegramToken  string `json:"telegram_token"`
	TelegramChatID string `json:"telegram_chat_id"`
	SlackWebhook   string `json:"slack_webhook"`
	FormatTime     string `json:"format_time"`
	GptKey         string `json:"gpt_key"`
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
			buy, err := convertToInt64(strings.TrimSpace(cells.Eq(1).Text()))
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			sell, err := convertToInt64(strings.TrimSpace(cells.Eq(2).Text()))
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				return
			}
			prices = append(prices, GoldPrice{
				Type:      strings.TrimSpace(cells.Eq(0).Text()),
				Buy:       *buy,
				Sell:      *sell,
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

func sendTelegram(cfg Config, message string) {
	if cfg.TelegramToken == "" || cfg.TelegramChatID == "" {
		return
	}

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.TelegramToken)
	http.PostForm(url, map[string][]string{
		"chat_id":    {cfg.TelegramChatID},
		"text":       {message},
		"parse_mode": {"Markdown"},
	})
}

func sendSlack(cfg Config, message string) {
	if cfg.SlackWebhook == "" {
		return
	}

	payload := fmt.Sprintf(`{"text": "%s"}`, strings.ReplaceAll(message, `"`, `\"`))
	http.Post(cfg.SlackWebhook, "application/json", strings.NewReader(payload))
}

func formatMessage(cfg Config, prices []GoldPrice, lastPrices []GoldPrice) (*string, error) {
	message := fmt.Sprintf("Giá vàng hôm nay - %s 💰\n", time.Now().Format(cfg.FormatTime))
	var ringGold GoldPrice
	var lastRingGold GoldPrice

	for _, p := range prices {
		if p.Type == "Vàng nhẫn khâu 9999" {
			ringGold = p
		}
	}
	for _, p := range lastPrices {
		if p.Type == "Vàng nhẫn khâu 9999" {
			lastRingGold = p
		}
	}

	message += fmt.Sprintf("• %s: Mua %s/chỉ - Bán %s/chỉ\n", ringGold.Type, FormatVND(ringGold.Buy), FormatVND(ringGold.Sell))
	currentBuy := ringGold.Buy
	currentSell := ringGold.Sell
	lastBuy := lastRingGold.Buy
	lastSell := lastRingGold.Sell

	switch true {
	case currentBuy-lastBuy > 0:
		message += fmt.Sprintf("> Hôm nay giá mua tăng %s/chỉ so với trước đó\n", FormatVND(currentBuy-lastBuy))
	case currentBuy-lastBuy < 0:
		message += fmt.Sprintf("> Hôm nay giá mua giảm %s/chỉ so với trước đó\n", FormatVND(currentBuy-lastBuy))
	default:
		message += fmt.Sprint("> Hôm nay giá mua không đổi so với trước đó\n")
	}

	switch true {
	case currentSell-lastSell > 0:
		message += fmt.Sprintf("> Hôm nay giá bán tăng %s/chỉ so với trước đó\n", FormatVND(currentSell-lastSell))
	case currentSell-lastSell < 0:
		message += fmt.Sprintf("> Hôm nay giá bán giảm %s/chỉ so với trước đó\n", FormatVND(currentSell-lastSell))
	default:
		message += fmt.Sprint("> Hôm nay giá bán không đổi so với trước đó\n")
	}

	return &message, nil
}

func convertToInt64(valueStr string) (*int64, error) {
	valueStr = strings.ReplaceAll(valueStr, ",", "")

	value, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		return nil, err
	}

	return &value, nil
}

func loadLastPrices() []GoldPrice {
	data, err := os.ReadFile("latest.json")
	if err != nil {
		log.Fatalf("Không đọc được latest.json: %v", err)
	}
	var prices []GoldPrice
	json.Unmarshal(data, &prices)
	return prices
}

func FormatVND(n int64) string {
	n = n * 1000
	if n < 0 {
		n *= -1
	}
	s := fmt.Sprintf("%d", n)
	var result strings.Builder

	count := 0
	for i := len(s) - 1; i >= 0; i-- {
		result.WriteByte(s[i])
		count++
		if count%3 == 0 && i != 0 {
			result.WriteByte('.')
		}
	}

	runes := []rune(result.String())
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	return string(runes) + " ₫"
}

func main() {
	cfg := loadConfig()

	for {
		now := time.Now()
		var nextRun time.Time

		morning := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
		afternoon := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())

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

		lastPrices := loadLastPrices()

		data, _ := json.MarshalIndent(prices, "", "  ")
		os.WriteFile("latest.json", data, 0644)
		saveToSQLite(prices)
		message, err := formatMessage(cfg, prices, lastPrices)
		if err != nil {
			log.Println("❌ Lỗi lấy message:", err)
			continue
		}
		sendTelegram(cfg, *message)
		sendSlack(cfg, *message)

		log.Println("✅ Cập nhật giá vàng thành công:", time.Now())
	}
}
