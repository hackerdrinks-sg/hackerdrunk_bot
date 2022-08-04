package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var invites map[string]InviteMap

const (
	InvitedByLink string = "invited_by_link"
	DirectInvite  string = "direct_invite"
)

// InviteMap contains the User Identifier and UserId of the person who made the
// invite
type InviteMap struct {
	InviterId int64
	Inviter   string
}

// A SocialGraphLine object contains a single invite social graph
type SocialGraphLine struct {
	Inviter    string    `json:"inviter"`
	InviterId  int64     `json:"inviter_id"`
	Invitee    string    `json:"invitee"`
	InviteeId  int64     `json:"invitee_id"`
	InviteType string    `json:"invite_type"`
	Timestamp  time.Time `json:"timestamp"`
}

// WriteInvite takes in a string line that contains the json output of the
// social graph and saves it to disk
func WriteInvite(line *SocialGraphLine, file *os.File) error {
	jsonBytes, err := json.Marshal(line)
	if err != nil {
		return err
	}

	_, err = file.Write(jsonBytes)
	if err != nil {
		return err
	}
	_, err = file.WriteString("\n")
	if err != nil {
		return err
	}
	return nil
}

func main() {

	// graphLogPath should contain the absolute/relative path to the logfile to be
	// used. eg. /path/to/invitegraph.txt
	graphLogPath := os.Getenv("GRAPH_LOG_PATH")
	if graphLogPath == "" {
		log.Fatalln("expecting to have GRAPH_LOG_PATH environment variable set but got nothing")
	}

	telegramBotAPIKey := os.Getenv("TELEGRAM_BOT_API_KEY")
	if telegramBotAPIKey == "" {
		log.Fatalln("expecting to have TELEGRAM_BOT_API_KEY environment variable set but got nothing")
	}

	baseRoom := os.Getenv("ROOM_ID")
	if baseRoom == "" {
		log.Fatalln("expecting to have ROOM_ID environment variable set but got nothing")
	}
	baseRoomId, err := strconv.Atoi(baseRoom)
	if err != nil {
		log.Fatalf("error converting ROOM_ID into int for keeping: %#v", err)
	}

	f, err := os.OpenFile(graphLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	invites = make(map[string]InviteMap)
	bot, err := tgbotapi.NewBotAPI(telegramBotAPIKey)
	if err != nil {
		log.Fatal(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {

		// handle if request is a ChatJoinRequest (after a /invite)
		if update.ChatJoinRequest != nil {
			chatJoinRequest := update.ChatJoinRequest

			link := chatJoinRequest.InviteLink
			username := GetUserIdentifier(chatJoinRequest.From)
			userid := chatJoinRequest.From.ID
			chatId := chatJoinRequest.Chat.ID
			log.Printf("user %s was invited to join chat via invite link: %#v", username, link)

			x := tgbotapi.ApproveChatJoinRequestConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: chatId}, UserID: userid}
			resp, err := bot.Request(x)
			if err != nil {
				log.Fatal(err)
			}
			if resp.Ok {
				// send msg to channel
				invitedby, ok := invites[link.InviteLink]
				if !ok {
					invitedby = InviteMap{
						Inviter:   "unknown",
						InviterId: 0,
					}
				}

				g := &SocialGraphLine{
					Inviter:    invitedby.Inviter,
					Invitee:    username,
					InviteeId:  chatJoinRequest.From.ID,
					InviteType: InvitedByLink,
					Timestamp:  time.Now(),
				}

				err := WriteInvite(g, f)
				if err != nil {
					log.Printf("error writing invite: %#v", err)
				}

				msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Say hello to %s who just joined us by invite from: %s", username, invitedby))

				bot.Send(msg)

			} else {
				log.Println("failed to join")

			}

			continue
		}

		if update.Message != nil {

			// Direct adding of user into channel flow
			chatId := update.Message.Chat.ID
			if update.Message.NewChatMembers != nil {
				messageFromID := update.Message.From.ID
				messageFromUserName := update.Message.From.UserName
				for _, v := range update.Message.NewChatMembers {
					if v.ID == messageFromID {
						continue
					}

					username := GetUserIdentifier(v)

					g := &SocialGraphLine{
						Inviter:    messageFromUserName,
						InviterId:  messageFromID,
						Invitee:    username,
						InviteeId:  v.ID,
						InviteType: DirectInvite,
						Timestamp:  time.Now(),
					}

					err := WriteInvite(g, f)
					if err != nil {
						log.Printf("error writing invite: %#v", err)
					}
					log.Printf("user %s added by %s", username, messageFromUserName)
					msg := tgbotapi.NewMessage(chatId, fmt.Sprintf("Hi welcome @%s!", username))
					bot.Send(msg)

				}

			}

			// When bot receives the /invite command
			if strings.Contains(update.Message.Text, "/invite") {
				if update.FromChat().IsPrivate() {
					x := tgbotapi.CreateChatInviteLinkConfig{
						ChatConfig:         tgbotapi.ChatConfig{ChatID: int64(baseRoomId)},
						Name:               "tmp",
						ExpireDate:         int(time.Now().Add(5 * time.Minute).Unix()),
						CreatesJoinRequest: true,
					}
					resp, err := bot.Request(x)
					if err != nil {
						log.Fatal(err)
					}

					inviteLink := &tgbotapi.ChatInviteLink{}
					invite := resp.Result

					err = json.Unmarshal(invite, inviteLink)
					if err != nil {
						log.Fatal(err)
					}

					username := GetUserIdentifier(*update.Message.From)
					log.Printf("user %s created an invite link: %#v", username, inviteLink.InviteLink)

					inviteMap := InviteMap{
						Inviter:   username,
						InviterId: update.Message.From.ID,
					}

					invites[inviteLink.InviteLink] = inviteMap

					// send msg invite out
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Generated invite link for hackerdrinks: %s\nThis link is valid for 5 minutes", inviteLink.InviteLink))
					msg.ReplyToMessageID = update.Message.MessageID
					bot.Send(msg)
				}

			}
		}
	}
}

// GetUserIdentifier takes in a Telegram User and returns the username if it
// exists, else return the user's first name
func GetUserIdentifier(u tgbotapi.User) string {
	if u.UserName == "" {
		return u.FirstName
	}
	return u.UserName

}
