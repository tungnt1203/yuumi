package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

type WebhookPayload struct {
	Action  string `json:"action"`
	Comment struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
}

type Comment struct {
	Author string
	Body   string
}

func (c Comment) MentionsBot(botName string) bool {
	return strings.Contains(c.Body, "@"+botName)
}
func (c Comment) ExtractCommand(botName string) (string, error) {
	if !c.MentionsBot(botName) {
		return "", fmt.Errorf("comment does not mention bot %s", botName)
	}

	body := c.Body
	mention := "@" + botName
	index := strings.Index(body, mention)
	return strings.TrimSpace(body[index+len(mention):]), nil
}

func main() {
	fmt.Println("Yuumi review bot starting...")
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		var payload WebhookPayload
		err := json.NewDecoder(r.Body).Decode(&payload)
		if err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		comment := Comment{
			Author: payload.Comment.User.Login,
			Body:   payload.Comment.Body,
		}

		cmd, err := comment.ExtractCommand("yuumi-bot")
		if err != nil {
			fmt.Println("Ignored:", err)
			fmt.Fprintln(w, "ignored")
			return
		}
		fmt.Println("Command from", comment.Author, ":", cmd)
		fmt.Fprintln(w, "received: "+cmd)
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}
