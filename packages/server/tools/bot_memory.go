package tools

// bot_memory — per-user conversation history for chat bots.
//
// Stores the last N message turns for each user so the bot has context
// across sessions. Files live at ~/.claude/bot-memory/<key>.json where
// key is "<platform>:<userID>" (e.g. "slack:U012AB3CD").

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

const maxHistoryTokenBudget = 8000 // ~32k chars at 4 chars/token

// memoryTurn is a single message turn stored in history.
type memoryTurn struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// botMemory holds the conversation history for one user.
type botMemory struct {
	Turns []memoryTurn `json:"turns"`
}

func botMemoryPath(claudeDir, key string) string {
	return filepath.Join(claudeDir, "bot-memory", key+".json")
}

// loadBotMemory reads the stored history for a user key.
// Returns an empty history if the file doesn't exist or can't be parsed.
func loadBotMemory(claudeDir, key string) botMemory {
	data, err := os.ReadFile(botMemoryPath(claudeDir, key))
	if err != nil {
		return botMemory{}
	}
	var m botMemory
	if err := json.Unmarshal(data, &m); err != nil {
		log.Printf("bot_memory: failed to parse history for %s: %v", key, err)
		return botMemory{}
	}
	return m
}

// trimHistory removes the oldest turns until the total token count is within budget.
// Estimates 1 token = 4 characters (ceiling division) with a minimum of 1 token per turn
// to prevent unbounded growth from many short messages.
func trimHistory(turns []memoryTurn) []memoryTurn {
	total := 0
	for i := len(turns) - 1; i >= 0; i-- {
		tokens := (len(turns[i].Content) + 3) / 4 // ceiling division
		if tokens < 1 {
			tokens = 1 // at least 1 token per turn
		}
		total += tokens
		if total > maxHistoryTokenBudget {
			return turns[i+1:]
		}
	}
	return turns
}

// saveBotMemory writes the history to disk, trimming to stay within token budget.
func saveBotMemory(claudeDir, key string, m botMemory) {
	// Trim to stay within token budget
	m.Turns = trimHistory(m.Turns)

	path := botMemoryPath(claudeDir, key)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		log.Printf("bot_memory: failed to create dir: %v", err)
		return
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		log.Printf("bot_memory: failed to marshal history for %s: %v", key, err)
		return
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		log.Printf("bot_memory: failed to write history for %s: %v", key, err)
	}
}
