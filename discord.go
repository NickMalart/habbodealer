package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// sendDiscordWebhookForGame posts a nicely formatted embed about a completed
// game to the configured Discord webhook (read from DISCORD_WEBHOOK_URL).
func (a *App) sendDiscordWebhookForGame(entry GameHistoryEntry) {
	// Hardcoded webhook URL (provided by user)
	webhookURL := "https://discordapp.com/api/webhooks/1496681436592214016/QTGLb6qYMv0-61hVc3m9s7mBgvMc-E0LKpQTxd1bSow9N_GqOjQMyw9njq8KcsM8Jhi6"
	a.AddLogMsg("[DISCORD] using hardcoded webhook URL")

	// Validate webhook URL contains a numeric webhook ID (snowflake).
	if u, perr := url.Parse(webhookURL); perr == nil {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		for i, p := range parts {
			if p == "webhooks" && i+2 < len(parts) {
				webhookID := parts[i+1]
				token := parts[i+2]
				isDigits := true
				for _, r := range webhookID {
					if r < '0' || r > '9' {
						isDigits = false
						break
					}
				}
				maskedID := webhookID
				if len(maskedID) > 8 {
					maskedID = maskedID[:4] + "..." + maskedID[len(maskedID)-4:]
				}
				maskedToken := token
				if len(maskedToken) > 8 {
					maskedToken = maskedToken[:4] + "..." + maskedToken[len(maskedToken)-4:]
				} else {
					maskedToken = "****"
				}
				if !isDigits {
					a.AddLogMsg(fmt.Sprintf("[DISCORD] webhook id invalid: %q token=%q", maskedID, maskedToken))
					a.AddLogMsg("[DISCORD] webhook id must be numeric snowflake; check DISCORD_WEBHOOK_URL")
					return
				}
				a.AddLogMsg(fmt.Sprintf("[DISCORD] using webhook id=%s token=%s", maskedID, maskedToken))
				break
			}
		}
	} else {
		a.AddLogMsg(fmt.Sprintf("[DISCORD] failed to parse webhook URL: %v", perr))
	}

	formatItems := func(items []TradeItem) string {
		if len(items) == 0 {
			return "None"
		}
		parts := make([]string, 0, len(items))
		for _, it := range items {
			parts = append(parts, fmt.Sprintf("%s x%d", it.Name, it.Quantity))
		}
		s := strings.Join(parts, ", ")
		if len(s) > 900 {
			s = s[:900] + "…"
		}
		return s
	}

	formatNotes := func(notes []string) string {
		if len(notes) == 0 {
			return "None"
		}
		s := strings.Join(notes, "\n")
		if len(s) > 900 {
			s = s[:900] + "…"
		}
		return s
	}

	formatField := func(v string) string {
		if strings.TrimSpace(v) == "" {
			return "None"
		}
		if len(v) > 900 {
			return v[:900] + "…"
		}
		return v
	}

	embed := map[string]interface{}{
		"title":       fmt.Sprintf("%s - %s", entry.Game, entry.Status),
		"description": fmt.Sprintf("Player: %s", entry.PlayerName),
		"color":       3447003,
		"fields": []map[string]interface{}{
			{"name": "Winner", "value": formatField(entry.Winner), "inline": true},
			{"name": "Outcome", "value": formatField(entry.Status), "inline": true},
			{"name": "Choice", "value": formatField(entry.Choice), "inline": true},
			{"name": "Player Result", "value": formatField(entry.PlayerResult), "inline": true},
			{"name": "Dealer Result", "value": formatField(entry.DealerResult), "inline": true},
			{"name": "Payout Multiplier", "value": strconv.Itoa(entry.PayoutMultiplier), "inline": true},
			{"name": "Player Shout", "value": formatField(entry.ChoiceShout), "inline": false},
			{"name": "Bet Items", "value": formatItems(entry.BetItems), "inline": false},
			{"name": "Payout Items", "value": formatItems(entry.PayoutItems), "inline": false},
			{"name": "Notes", "value": formatNotes(entry.Notes), "inline": false},
			{"name": "Started At", "value": entry.StartedAt, "inline": true},
			{"name": "Completed At", "value": entry.CompletedAt, "inline": true},
		},
		"timestamp": entry.CompletedAt,
		"footer":    map[string]interface{}{"text": fmt.Sprintf("Game ID: %s", entry.ID)},
	}

	payload := map[string]interface{}{
		"username": "Roll Origins",
		"embeds":   []interface{}{embed},
		"allowed_mentions": map[string][]string{
			"parse": []string{},
		},
	}

	jb, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		a.AddErrorLog("[DISCORD] marshal error", err)
		return
	}

	// Persist the exact payload to payload.json (overwrite each game)
	payloadFile := "payload.json"
	if err := os.WriteFile(payloadFile, jb, 0600); err != nil {
		a.AddLogMsg(fmt.Sprintf("[DISCORD] failed to write payload file: %v", err))
	} else {
		a.AddLogMsg("[DISCORD] payload written to " + payloadFile)
	}

	// Read back the payload file to ensure we post the exact bytes written.
	fileBytes, fileErr := os.ReadFile(payloadFile)
	if fileErr == nil {
		jb = fileBytes
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(jb))
	if err != nil {
		a.AddErrorLog("[DISCORD] request error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Roll Origins/1.0")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		a.AddErrorLog("[DISCORD] POST error", err)
		return
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	respBody := strings.TrimSpace(string(bodyBytes))
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if respBody == "" {
			a.AddLogMsg("[DISCORD] webhook sent (no response body)")
		} else {
			a.AddLogMsg(fmt.Sprintf("[DISCORD] webhook sent; body=%q", respBody))
		}
	} else {
		a.AddLogMsg(fmt.Sprintf("[DISCORD] webhook responded: %d body=%q", resp.StatusCode, respBody))
	}
}

const raffleWebhookURL = "https://discordapp.com/api/webhooks/1499651607800057926/SLvv8HU_yG2vyW04MaXkZ2eVd_10qpddkJjBWDQ6GmB7LzUhJu7yZAgQInsg0eLOogJ9"

func getRaffleImagesDir() string {
	configDir, _ := os.UserConfigDir()
	path := filepath.Join(configDir, "Roll Origins", "raffle_images")
	_ = os.MkdirAll(path, 0700)
	return path
}

func decodeRaffleImageDataURL(dataURL string) (image.Image, error) {
	dataURL = strings.TrimSpace(dataURL)
	if dataURL == "" {
		return nil, fmt.Errorf("empty image data")
	}
	comma := strings.Index(dataURL, ",")
	if comma < 0 {
		return nil, fmt.Errorf("invalid data url")
	}
	head := dataURL[:comma]
	body := dataURL[comma+1:]
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(raw)
	if strings.Contains(strings.ToLower(head), "image/png") {
		return png.Decode(reader)
	}
	reader.Reset(raw)
	if strings.Contains(strings.ToLower(head), "image/jpeg") || strings.Contains(strings.ToLower(head), "image/jpg") {
		return jpeg.Decode(reader)
	}
	reader.Reset(raw)
	if strings.Contains(strings.ToLower(head), "image/gif") {
		return gif.Decode(reader)
	}
	reader.Reset(raw)
	img, _, err := image.Decode(reader)
	return img, err
}

func colorDistanceSq(aR, aG, aB, bR, bG, bB int) int {
	dR := aR - bR
	dG := aG - bG
	dB := aB - bB
	return dR*dR + dG*dG + dB*dB
}

// removeEdgeBackgroundToTransparent flood-fills from image borders and makes
// near-background pixels transparent, preserving the central foreground item.
func removeEdgeBackgroundToTransparent(src image.Image) *image.NRGBA {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA))
		}
	}
	if b.Dx() == 0 || b.Dy() == 0 {
		return out
	}

	corners := [][2]int{{0, 0}, {b.Dx() - 1, 0}, {0, b.Dy() - 1}, {b.Dx() - 1, b.Dy() - 1}}
	sumR, sumG, sumB := 0, 0, 0
	for _, c := range corners {
		px := out.NRGBAAt(c[0], c[1])
		sumR += int(px.R)
		sumG += int(px.G)
		sumB += int(px.B)
	}
	bgR, bgG, bgB := sumR/4, sumG/4, sumB/4
	thresholdSq := 38 * 38

	width, height := b.Dx(), b.Dy()
	visited := make([]bool, width*height)
	idx := func(x, y int) int { return y*width + x }
	queueX := make([]int, 0, width*2+height*2)
	queueY := make([]int, 0, width*2+height*2)
	push := func(x, y int) {
		if x < 0 || y < 0 || x >= width || y >= height {
			return
		}
		i := idx(x, y)
		if visited[i] {
			return
		}
		visited[i] = true
		queueX = append(queueX, x)
		queueY = append(queueY, y)
	}

	for x := 0; x < width; x++ {
		push(x, 0)
		push(x, height-1)
	}
	for y := 0; y < height; y++ {
		push(0, y)
		push(width-1, y)
	}

	for head := 0; head < len(queueX); head++ {
		x := queueX[head]
		y := queueY[head]
		px := out.NRGBAAt(x, y)
		d := colorDistanceSq(int(px.R), int(px.G), int(px.B), bgR, bgG, bgB)
		if d > thresholdSq {
			continue
		}
		px.A = 0
		out.SetNRGBA(x, y, px)
		push(x+1, y)
		push(x-1, y)
		push(x, y+1)
		push(x, y-1)
	}

	return out
}

func trimTransparentBounds(src *image.NRGBA) image.Rectangle {
	b := src.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if src.NRGBAAt(x, y).A == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < minX || maxY < minY {
		return b
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

func cropNRGBA(src *image.NRGBA, rect image.Rectangle) *image.NRGBA {
	rect = rect.Intersect(src.Bounds())
	if rect.Empty() {
		return image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	out := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			out.SetNRGBA(x, y, src.NRGBAAt(rect.Min.X+x, rect.Min.Y+y))
		}
	}
	return out
}

func scaleNearestNRGBA(src *image.NRGBA, newW, newH int) *image.NRGBA {
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	sb := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, newW, newH))
	for y := 0; y < newH; y++ {
		sy := sb.Min.Y + (y*sb.Dy())/newH
		if sy >= sb.Max.Y {
			sy = sb.Max.Y - 1
		}
		for x := 0; x < newW; x++ {
			sx := sb.Min.X + (x*sb.Dx())/newW
			if sx >= sb.Max.X {
				sx = sb.Max.X - 1
			}
			out.SetNRGBA(x, y, src.NRGBAAt(sx, sy))
		}
	}
	return out
}

func buildLargeRaffleShowcaseImage(src *image.NRGBA) *image.NRGBA {
	trimmed := cropNRGBA(src, trimTransparentBounds(src))
	canvasW, canvasH := 1400, 900
	margin := 70
	availW := canvasW - margin*2
	availH := canvasH - margin*2
	if availW < 1 {
		availW = canvasW
	}
	if availH < 1 {
		availH = canvasH
	}

	scaleX := float64(availW) / float64(trimmed.Bounds().Dx())
	scaleY := float64(availH) / float64(trimmed.Bounds().Dy())
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}
	if scale < 1 {
		scale = 1
	}
	newW := int(float64(trimmed.Bounds().Dx()) * scale)
	newH := int(float64(trimmed.Bounds().Dy()) * scale)
	scaled := scaleNearestNRGBA(trimmed, newW, newH)

	canvas := image.NewNRGBA(image.Rect(0, 0, canvasW, canvasH))
	offX := (canvasW - newW) / 2
	offY := (canvasH - newH) / 2
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			canvas.SetNRGBA(offX+x, offY+y, scaled.NRGBAAt(x, y))
		}
	}
	return canvas
}

func saveTransparentRafflePrizeImage(raffleID string, dataURL string) (string, error) {
	img, err := decodeRaffleImageDataURL(dataURL)
	if err != nil {
		return "", err
	}
	processed := buildLargeRaffleShowcaseImage(removeEdgeBackgroundToTransparent(img))
	outPath := filepath.Join(getRaffleImagesDir(), raffleID+".png")
	f, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, processed); err != nil {
		return "", err
	}
	return outPath, nil
}

func imageToNRGBA(src image.Image) *image.NRGBA {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA))
		}
	}
	return out
}

func saveRaffleWinnerProofImage(raffleID string, dataURL string) (string, error) {
	img, err := decodeRaffleImageDataURL(dataURL)
	if err != nil {
		return "", err
	}
	converted := imageToNRGBA(img)
	maxDim := converted.Bounds().Dx()
	if converted.Bounds().Dy() > maxDim {
		maxDim = converted.Bounds().Dy()
	}
	if maxDim > 1600 {
		scale := 1600.0 / float64(maxDim)
		converted = scaleNearestNRGBA(converted, int(float64(converted.Bounds().Dx())*scale), int(float64(converted.Bounds().Dy())*scale))
	}
	outPath := filepath.Join(getRaffleImagesDir(), raffleID+"_winner.png")
	f, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, converted); err != nil {
		return "", err
	}
	return outPath, nil
}

func parseDiscordWebhookURL(webhookURL string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(webhookURL))
	if err != nil {
		return "", "", err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "webhooks" && i+2 < len(parts) {
			return parts[i+1], parts[i+2], nil
		}
	}
	return "", "", fmt.Errorf("invalid webhook path: %s", u.Path)
}

func formatRaffleParticipantsForDiscord(parts []RaffleParticipant) string {
	if len(parts) == 0 {
		return "🎟️ No ticket purchases yet. Be the first to jump in!"
	}
	cp := make([]RaffleParticipant, len(parts))
	copy(cp, parts)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Tickets == cp[j].Tickets {
			return strings.ToLower(cp[i].Name) < strings.ToLower(cp[j].Name)
		}
		return cp[i].Tickets > cp[j].Tickets
	})

	lines := make([]string, 0, len(cp))
	for _, p := range cp {
		line := fmt.Sprintf("🎟️ %s - %d ticket(s)", strings.TrimSpace(p.Name), p.Tickets)
		lines = append(lines, line)
	}
	joined := strings.Join(lines, "\n")
	if len(joined) > 1000 {
		return joined[:1000] + "..."
	}
	return joined
}

func truncateDiscordField(v string, max int) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "N/A"
	}
	if len(v) > max {
		return v[:max] + "..."
	}
	return v
}

func normalizeGMTLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	upper := strings.ToUpper(label)
	if strings.HasPrefix(upper, "GMT") {
		return "GMT" + strings.TrimSpace(label[3:])
	}
	if strings.HasPrefix(label, "+") || strings.HasPrefix(label, "-") {
		return "GMT" + label
	}
	return "GMT " + label
}

func formatGMTOffsetFromTime(t time.Time) string {
	_, offset := t.Zone()
	if offset == 0 {
		return "GMT"
	}
	if offset < 0 {
		offset = -offset
		sign := "-"
		h := offset / 3600
		m := (offset % 3600) / 60
		if m == 0 {
			return fmt.Sprintf("GMT%s%d", sign, h)
		}
		return fmt.Sprintf("GMT%s%d:%02d", sign, h, m)
	}
	sign := "+"
	h := offset / 3600
	m := (offset % 3600) / 60
	if m == 0 {
		return fmt.Sprintf("GMT%s%d", sign, h)
	}
	return fmt.Sprintf("GMT%s%d:%02d", sign, h, m)
}

func formatRaffleDateTime(raw string, gmtOverride string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "N/A"
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	gmt := normalizeGMTLabel(gmtOverride)
	if gmt == "" {
		gmt = formatGMTOffsetFromTime(t)
	}
	return fmt.Sprintf("%s %s %s", t.Format("02/01/2006"), t.Format("15:04"), gmt)
}

func buildRaffleDiscordPayload(raffle Raffle, isCreate bool) map[string]interface{} {
	totalTickets := 0
	for _, p := range raffle.Participants {
		totalTickets += p.Tickets
	}

	headline := "🎉 NEW RAFFLE! 🎉"
	if !isCreate {
		headline = "🎟️ RAFFLE LIVE UPDATE 🎟️"
	}
	if strings.EqualFold(strings.TrimSpace(raffle.Status), "completed") && strings.TrimSpace(raffle.Winner) != "" {
		headline = "🏆 RAFFLE WINNER LOCKED IN! 🏆"
	}

	statusEmoji := "🟢"
	if strings.EqualFold(strings.TrimSpace(raffle.Status), "ended") {
		statusEmoji = "🟠"
	}
	if strings.EqualFold(strings.TrimSpace(raffle.Status), "completed") {
		statusEmoji = "✅"
	}

	winnerValue := "TBD"
	if strings.TrimSpace(raffle.Winner) != "" {
		winnerValue = "🏆 " + raffle.Winner
	}

	desc := fmt.Sprintf("%s %s\n🎁 Prize: %s x%d\n🎰 Raffle ID: %s", headline, raffle.Name, raffle.PrizeName, raffle.PrizeQty, raffle.ID)
	if strings.TrimSpace(raffle.EndAt) != "" {
		desc += fmt.Sprintf("\n⏰ Planned End: %s", formatRaffleDateTime(raffle.EndAt, raffle.EndAtGmt))
	}
	heroDescription := fmt.Sprintf("🎁 **%s x%d**\n🔥 %s", raffle.PrizeName, raffle.PrizeQty, raffle.Name)
	winnerProofDescription := "🤝 Winner payout screenshot"
	if strings.TrimSpace(raffle.Winner) != "" {
		winnerProofDescription = fmt.Sprintf("🤝 Proof that %s received the prize", raffle.Winner)
	}

	fields := []map[string]interface{}{
		{"name": "📌 Status", "value": truncateDiscordField(fmt.Sprintf("%s %s", statusEmoji, raffle.Status), 250), "inline": true},
		{"name": "👥 Participants", "value": fmt.Sprintf("%d", len(raffle.Participants)), "inline": true},
		{"name": "🎟️ Total Tickets", "value": fmt.Sprintf("%d", totalTickets), "inline": true},
		{"name": "🏆 Winner", "value": truncateDiscordField(winnerValue, 250), "inline": true},
		{"name": "🕒 Created", "value": truncateDiscordField(formatRaffleDateTime(raffle.CreatedAt, ""), 250), "inline": true},
		{"name": "📣 Ticket Board", "value": truncateDiscordField(formatRaffleParticipantsForDiscord(raffle.Participants), 1020), "inline": false},
	}

	if strings.TrimSpace(raffle.EndedAt) != "" {
		fields = append(fields, map[string]interface{}{"name": "🛑 Ended", "value": truncateDiscordField(formatRaffleDateTime(raffle.EndedAt, ""), 250), "inline": true})
	}

	detailsEmbed := map[string]interface{}{
		"title":       "🎲 Roll Origins Raffle Tracker 🎲",
		"description": desc,
		"color":       15844367,
		"fields":      fields,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"footer": map[string]interface{}{
			"text": "✨ Auto-updated on every ticket buy and winner draw ✨",
		},
	}
	embeds := []interface{}{}
	if strings.TrimSpace(raffle.PrizeImageURL) != "" {
		heroEmbed := map[string]interface{}{
			"title":       "🎁 WHAT'S UP FOR GRABS 🎁",
			"description": heroDescription,
			"color":       16766720,
			"image":       map[string]interface{}{"url": strings.TrimSpace(raffle.PrizeImageURL)},
		}
		embeds = append(embeds, heroEmbed)
	} else if isCreate && strings.TrimSpace(raffle.PrizeImagePath) != "" {
		heroEmbed := map[string]interface{}{
			"title":       "🎁 WHAT'S UP FOR GRABS 🎁",
			"description": heroDescription,
			"color":       16766720,
			"image":       map[string]interface{}{"url": "attachment://raffle_prize.png"},
		}
		embeds = append(embeds, heroEmbed)
	}
	if strings.TrimSpace(raffle.WinnerImageURL) != "" {
		proofEmbed := map[string]interface{}{
			"title":       "🏆 WINNER RECEIVED PRIZE 🏆",
			"description": winnerProofDescription,
			"color":       5763719,
			"image":       map[string]interface{}{"url": strings.TrimSpace(raffle.WinnerImageURL)},
		}
		embeds = append(embeds, proofEmbed)
	} else if strings.TrimSpace(raffle.WinnerImagePath) != "" {
		proofEmbed := map[string]interface{}{
			"title":       "🏆 WINNER RECEIVED PRIZE 🏆",
			"description": winnerProofDescription,
			"color":       5763719,
			"image":       map[string]interface{}{"url": "attachment://winner_proof.png"},
		}
		embeds = append(embeds, proofEmbed)
	}
	embeds = append(embeds, detailsEmbed)

	content := fmt.Sprintf("%s %s | 🎁 %s x%d | 🎟️ %d total tickets", headline, raffle.Name, raffle.PrizeName, raffle.PrizeQty, totalTickets)
	if strings.TrimSpace(raffle.Winner) != "" {
		content += " | 🏆 Winner: " + raffle.Winner
	}

	return map[string]interface{}{
		"username": "Roll Origins Raffles",
		"content":  truncateDiscordField(content, 1800),
		"embeds":   embeds,
		"allowed_mentions": map[string][]string{
			"parse": {},
		},
	}
}

func doDiscordWebhookRequest(method string, urlStr string, payload map[string]interface{}) (int, []byte, error) {
	jb, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(method, urlStr, bytes.NewReader(jb))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Roll Origins/1.0")

	client := &http.Client{Timeout: 7 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

func doDiscordWebhookMultipartRequest(method string, urlStr string, payload map[string]interface{}, files []struct {
	Path      string
	FieldName string
	FileName  string
}) (int, []byte, error) {
	jb, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("payload_json", string(jb)); err != nil {
		return 0, nil, err
	}
	for _, file := range files {
		fileBytes, err := os.ReadFile(file.Path)
		if err != nil {
			return 0, nil, err
		}
		part, err := w.CreateFormFile(file.FieldName, file.FileName)
		if err != nil {
			return 0, nil, err
		}
		if _, err := part.Write(fileBytes); err != nil {
			return 0, nil, err
		}
	}
	if err := w.Close(); err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequest(method, urlStr, &buf)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("User-Agent", "Roll Origins/1.0")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

func doDiscordWebhookMultipartPost(urlStr string, payload map[string]interface{}, filePath string, fieldName string, fileName string) (int, []byte, error) {
	return doDiscordWebhookMultipartRequest("POST", urlStr, payload, []struct {
		Path      string
		FieldName string
		FileName  string
	}{{Path: filePath, FieldName: fieldName, FileName: fileName}})
}

func (a *App) sendOrUpdateRaffleDiscordMessage(raffleID string, forceCreate bool) {
	raffleID = strings.TrimSpace(raffleID)
	if raffleID == "" {
		return
	}

	webhookID, webhookToken, err := parseDiscordWebhookURL(raffleWebhookURL)
	if err != nil {
		a.AddLogMsg("[DISCORD_RAFFLE] parse webhook url failed: " + err.Error())
		return
	}

	var raffle Raffle
	messageID := ""
	create := forceCreate

	rafflesMu.Lock()
	found := false
	for i := range raffles {
		if raffles[i].ID == raffleID {
			raffle = raffles[i]
			messageID = strings.TrimSpace(raffles[i].DiscordMessageID)
			if strings.TrimSpace(raffles[i].DiscordWebhookID) != webhookID {
				create = true
			}
			if messageID == "" {
				create = true
			}
			found = true
			break
		}
	}
	rafflesMu.Unlock()

	if !found {
		return
	}

	if create {
		payload := buildRaffleDiscordPayload(raffle, true)
		postURL := raffleWebhookURL + "?wait=true"
		var status int
		var body []byte
		var reqErr error
		if strings.TrimSpace(raffle.PrizeImagePath) != "" && strings.TrimSpace(raffle.PrizeImageURL) == "" {
			status, body, reqErr = doDiscordWebhookMultipartPost(postURL, payload, raffle.PrizeImagePath, "files[0]", "raffle_prize.png")
		} else {
			status, body, reqErr = doDiscordWebhookRequest("POST", postURL, payload)
		}
		if reqErr != nil {
			a.AddLogMsg("[DISCORD_RAFFLE] create message failed: " + reqErr.Error())
			return
		}
		if status < 200 || status >= 300 {
			a.AddLogMsg(fmt.Sprintf("[DISCORD_RAFFLE] create message responded %d body=%q", status, strings.TrimSpace(string(body))))
			return
		}
		var resp struct {
			ID          string `json:"id"`
			Attachments []struct {
				URL string `json:"url"`
			} `json:"attachments"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			a.AddLogMsg("[DISCORD_RAFFLE] failed to parse created message id: " + err.Error())
			return
		}
		if strings.TrimSpace(resp.ID) == "" {
			a.AddLogMsg("[DISCORD_RAFFLE] created webhook message has empty id")
			return
		}

		rafflesMu.Lock()
		for i := range raffles {
			if raffles[i].ID == raffleID {
				raffles[i].DiscordWebhookID = webhookID
				raffles[i].DiscordMessageID = resp.ID
				if len(resp.Attachments) > 0 && strings.TrimSpace(resp.Attachments[0].URL) != "" {
					raffles[i].PrizeImageURL = strings.TrimSpace(resp.Attachments[0].URL)
				}
				a.saveRafflesLocked()
				break
			}
		}
		rafflesMu.Unlock()
		a.AddLogMsg("[DISCORD_RAFFLE] created new raffle webhook message")
		return
	}

	payload := buildRaffleDiscordPayload(raffle, false)
	patchURL := fmt.Sprintf("https://discordapp.com/api/webhooks/%s/%s/messages/%s", webhookID, webhookToken, messageID)
	var status int
	var body []byte
	var reqErr error
	patchFiles := []struct {
		Path      string
		FieldName string
		FileName  string
	}{}
	if strings.TrimSpace(raffle.WinnerImagePath) != "" && strings.TrimSpace(raffle.WinnerImageURL) == "" {
		patchFiles = append(patchFiles, struct {
			Path      string
			FieldName string
			FileName  string
		}{Path: raffle.WinnerImagePath, FieldName: "files[0]", FileName: "winner_proof.png"})
	}
	if len(patchFiles) > 0 {
		status, body, reqErr = doDiscordWebhookMultipartRequest("PATCH", patchURL, payload, patchFiles)
	} else {
		status, body, reqErr = doDiscordWebhookRequest("PATCH", patchURL, payload)
	}
	if reqErr != nil {
		a.AddLogMsg("[DISCORD_RAFFLE] patch message failed: " + reqErr.Error())
		return
	}
	if status >= 200 && status < 300 {
		if len(patchFiles) > 0 {
			var resp struct {
				Attachments []struct {
					URL string `json:"url"`
				} `json:"attachments"`
			}
			if err := json.Unmarshal(body, &resp); err == nil {
				for _, att := range resp.Attachments {
					url := strings.TrimSpace(att.URL)
					if url == "" || !strings.Contains(strings.ToLower(url), "winner_proof") {
						continue
					}
					rafflesMu.Lock()
					for i := range raffles {
						if raffles[i].ID == raffleID {
							raffles[i].WinnerImageURL = url
							a.saveRafflesLocked()
							break
						}
					}
					rafflesMu.Unlock()
					break
				}
			}
		}
		a.AddLogMsg("[DISCORD_RAFFLE] patched raffle webhook message")
		return
	}

	a.AddLogMsg(fmt.Sprintf("[DISCORD_RAFFLE] patch responded %d body=%q", status, strings.TrimSpace(string(body))))
	if status == 404 {
		a.sendOrUpdateRaffleDiscordMessage(raffleID, true)
	}
}
