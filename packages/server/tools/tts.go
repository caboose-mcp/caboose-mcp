package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/caboose-mcp/server/config"
)

// ShouldSpeak returns true if text is worth synthesizing to audio.
// Skips: short responses (<100 chars), messages with code blocks.
func ShouldSpeak(text string) bool {
	if len(text) < 100 {
		return false
	}
	if strings.Contains(text, "```") {
		return false
	}
	return true
}

// Synthesize calls the ElevenLabs v1 TTS API and returns MP3 bytes.
// Returns nil, nil if ElevenLabsAPIKey is not configured (silent no-op).
func Synthesize(ctx context.Context, cfg *config.Config, text string) ([]byte, error) {
	if cfg.ElevenLabsAPIKey == "" || cfg.ElevenLabsVoiceID == "" {
		return nil, nil
	}

	voiceID := cfg.ElevenLabsVoiceID

	body, err := json.Marshal(map[string]any{
		"text":     text,
		"model_id": "eleven_monolingual_v1",
		"voice_settings": map[string]float64{
			"stability":        0.5,
			"similarity_boost": 0.75,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("tts: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.elevenlabs.io/v1/text-to-speech/"+voiceID,
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tts: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", cfg.ElevenLabsAPIKey)
	req.Header.Set("Accept", "audio/mpeg")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tts: api error %d: %s", resp.StatusCode, string(b))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tts: read response: %w", err)
	}
	return audio, nil
}
