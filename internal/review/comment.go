package review

import (
	"fmt"
	"strings"
)

type Comment struct {
	ID     int64
	Author string
	Body   string
}

func (c Comment) MentionsBot(botName string) bool {
	mention := "@" + botName
	for _, word := range strings.Fields(c.Body) {
		if word == mention {
			return true
		}
	}
	return false
}

func (c Comment) ExtractCommand(botName string) (string, error) {
	mention := "@" + botName
	words := strings.Fields(c.Body)
	for i, word := range words {
		if word == mention {
			return strings.Join(words[i+1:], " "), nil
		}
	}
	return "", fmt.Errorf("comment does not mention bot %s", botName)
}
