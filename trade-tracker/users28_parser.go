package main

import (
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
)

var (
	figureRegex          = regexp.MustCompile(`^(hr|hd|ch|lg|sh|ea|ha|fa|ca|wa)-\d+-\d+`)
	mottoSkipRegex       = regexp.MustCompile(`^[A-Z]{3,4}\d`)
	alphaLowerUpperRegex = regexp.MustCompile(`^[a-z]+[A-Z]$`)
)

type rawField struct {
	raw []byte
	str string
}

func cleanASCII(data []byte) string {
	var sb strings.Builder
	sb.Grow(len(data))
	for _, b := range data {
		if b >= 32 && b <= 126 {
			sb.WriteByte(b)
		}
	}
	return strings.TrimSpace(sb.String())
}

func splitPacketFields(packet []byte) []rawField {
	parts := []rawField{}
	rawParts := bytesSplit(packet, 2)
	for _, part := range rawParts {
		parts = append(parts, rawField{
			raw: part,
			str: cleanASCII(part),
		})
	}
	return parts
}

func bytesSplit(s []byte, sep byte) [][]byte {
	var res [][]byte
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			res = append(res, s[start:i])
			start = i + 1
		}
	}
	res = append(res, s[start:])
	return res
}

func vl64ChunkLength(text string) int {
	if len(text) == 0 {
		return 0
	}
	length := int((text[0] >> 3) & 7)
	if length <= 0 {
		length = 1
	}
	return length
}

func decodeVL64(chunk string) int {
	if len(chunk) == 0 {
		return 0
	}
	first := int(chunk[0])
	totalBytes := (first >> 3) & 7
	if totalBytes <= 0 {
		totalBytes = 1
	}
	if totalBytes > len(chunk) {
		totalBytes = len(chunk)
	}
	negative := (first & 4) != 0
	value := first & 3
	shift := 2
	for i := 1; i < totalBytes; i++ {
		value |= int(chunk[i]&0x3F) << shift
		shift += 6
	}
	if negative {
		value = -value
	}
	return value
}

func prefixCountFromLiveBlock(text string) int {
	if len(text) == 0 {
		return 3
	}
	if vl64ChunkLength(text) == 2 {
		return 5
	}
	return 3
}

func readFixedVL64Prefix(text string, count int) ([]string, string) {
	var parts []string
	remaining := text
	for i := 0; i < count; i++ {
		if len(remaining) == 0 {
			break
		}
		length := vl64ChunkLength(remaining)
		if length > len(remaining) {
			length = len(remaining)
		}
		current := remaining[:length]
		parts = append(parts, current)
		remaining = remaining[length:]
	}
	return parts, remaining
}

func isAllAlpha(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')) {
			return false
		}
	}
	return true
}

func isAlphaByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func extractEntityAndUsername(nameBlock string, nameBlockRaw []byte) (entityID, chatIDRaw, tradeIDRaw, username, tokenHex string, chatID, tradeID int) {
	working := nameBlock
	if strings.HasPrefix(working, "@\\") {
		working = working[2:]
	}

	numInts := prefixCountFromLiveBlock(working)
	parsedInts, remaining := readFixedVL64Prefix(working, numInts)

	if len(remaining) == 0 && len(parsedInts) > 0 {
		remaining = parsedInts[len(parsedInts)-1]
		parsedInts = parsedInts[:len(parsedInts)-1]
	}

	if len(remaining) <= 2 && len(parsedInts) > 0 {
		for len(parsedInts) > 0 && isAllAlpha(parsedInts[len(parsedInts)-1]) {
			candidate := parsedInts[len(parsedInts)-1]
			if alphaLowerUpperRegex.MatchString(candidate) {
				break
			}
			remaining = candidate + remaining
			parsedInts = parsedInts[:len(parsedInts)-1]
		}
	}

	if len(parsedInts) > 0 && len(remaining) > 0 {
		lastChunk := parsedInts[len(parsedInts)-1]
		if len(lastChunk) <= 2 && isAllAlpha(lastChunk) && isAlphaByte(remaining[0]) {
			remaining = lastChunk + remaining
			parsedInts = parsedInts[:len(parsedInts)-1]
		}
	}

	if len(parsedInts) >= 2 {
		chatIDRaw = parsedInts[len(parsedInts)-2]
	}
	if len(parsedInts) >= 1 {
		tradeIDRaw = parsedInts[len(parsedInts)-1]
	}
	entityID = strings.Join(parsedInts, "")

	if len(nameBlockRaw) > 0 {
		end := 4
		if len(nameBlockRaw) < 4 {
			end = len(nameBlockRaw)
		}
		tokenHex = hex.EncodeToString(nameBlockRaw[:end])
	}

	chatID = decodeVL64(chatIDRaw)
	tradeID = decodeVL64(tradeIDRaw)
	username = remaining
	return
}

// ParseUsers28 parses raw packet bytes for USERS[28] or SPACENODEUSERS[154]
// natively in Go without invoking external Python processes.
func ParseUsers28(packet []byte) ([]ParsedUsers28User, error) {
	fields := splitPacketFields(packet)
	var users []ParsedUsers28User

	for i, field := range fields {
		if !figureRegex.MatchString(field.str) {
			continue
		}

		nameBlockStr := ""
		var nameBlockRaw []byte
		if i > 0 {
			nameBlockStr = fields[i-1].str
			nameBlockRaw = fields[i-1].raw
		}

		entityID, chatIDRaw, tradeIDRaw, username, tokenHex, chatID, tradeID := extractEntityAndUsername(nameBlockStr, nameBlockRaw)

		sex := ""
		if i+1 < len(fields) {
			sex = fields[i+1].str
		}

		motto := ""
		if i+2 < len(fields) {
			candidate := fields[i+2].str
			if len(candidate) > 5 && !mottoSkipRegex.MatchString(candidate) && candidate != "Istd" && candidate != "std" {
				motto = candidate
			}
		}

		users = append(users, ParsedUsers28User{
			Username:     username,
			TradeID:      tradeID,
			TradeIDRaw:   tradeIDRaw,
			ChatID:       chatID,
			ChatIDRaw:    chatIDRaw,
			EntityID:     entityID,
			Figure:       field.str,
			Sex:          sex,
			Motto:        motto,
			TokenHex:     tokenHex,
			RawNameBlock: nameBlockStr,
		})
	}

	sort.Slice(users, func(i, j int) bool {
		if users[i].ChatID != users[j].ChatID {
			return users[i].ChatID < users[j].ChatID
		}
		if users[i].TradeID != users[j].TradeID {
			return users[i].TradeID < users[j].TradeID
		}
		return users[i].Username < users[j].Username
	})

	return users, nil
}
