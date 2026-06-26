package main
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type DiscordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type DiscordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Color       int                 `json:"color"`
	Fields      []DiscordEmbedField `json:"fields"`
	Timestamp   string              `json:"timestamp"`
	Footer      struct {
		Text string `json:"text"`
	} `json:"footer"`
}

type DiscordWebhookPayload struct {
	Username  string         `json:"username"`
	AvatarURL string         `json:"avatar_url"`
	Embeds    []DiscordEmbed `json:"embeds"`
}

func main() {
	webhookURL := "https://discord.com/api/webhooks/1518982954716627094/2_SZHySpMk9b6mrEqRm1N5ndLDF6XVAWRNMLIPJry-cz5Y4ihN7OyaxXZypqsXTy0lp-"
	embed := DiscordEmbed{
		Title: "TEST TITLE",
		Color: 0xf1c40f,
	}
	payload := DiscordWebhookPayload{
		Username:  "Casino Performance Bot",
		AvatarURL: "https://i.imgur.com/W7S6S8k.png",
		Embeds:    []DiscordEmbed{embed},
	}
	payloadBytes, _ := json.Marshal(payload)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(payloadBytes))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Status: %s\nBody: %s\n", resp.Status, string(body))
}
