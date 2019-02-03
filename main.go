package main

import (
	"fmt"
	"html"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/batt/tinabott/brain"
	"github.com/batt/tinabott/slackbot"
	"github.com/nlopes/slack"
)

func main() {
	token := os.Getenv("SLACK_BOT_TOKEN")

	if token == "" {
		log.Fatalln("No slack token found!")
	}

	redis_url := os.Getenv("REDIS_URL")
	if redis_url == "" {
		log.Fatalln("No redis URL found!")
	}

	// Slack Bot filter
	var opts slackbot.Config
	bot := slackbot.New(token, opts)
	brain := brain.New(redis_url)

	bot.RespondTo("^per me (.*)$", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		fmt.Printf("Message from channel (%s) <%s>: %s\n\r", msg.Channel, user.Name, msg.Text)
		bot.Message(msg.Channel, "Ok, "+args[1]+" per "+user.Name)
	})

	bot.RespondTo("^set (.*)$", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		key := args[1]
		val := args[2]
		err := brain.Set(key, val)
		if err != nil {
			bot.Message(msg.Channel, "Ok")
		} else {
			bot.Message(msg.Channel, "Error: "+err.Error())
		}
	})

	bot.RespondTo("^get (.*)$", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		key := args[1]
		var val string
		err := brain.Get(key, &val)
		if err != nil {
			bot.Message(msg.Channel, key+": "+val)
		} else {
			bot.Message(msg.Channel, "Error: "+err.Error())
		}
	})

	fmt.Printf("Run Bot server\n\r")
	go func(b *slackbot.Bot) {
		if err := b.Start(); err != nil {
			log.Fatalln(err)
		}
	}(bot)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, %q", html.EscapeString(r.URL.Path))
	})

	httpPort := os.Getenv("PORT")
	if httpPort == "" {
		httpPort = "8080"
	}

	httpURL := os.Getenv("HTTPURL")
	if httpURL == "" {
		httpURL = "https://tinabott.herokuapp.com"
	}

	http.HandleFunc("/keepalive", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK")
		fmt.Println("keepalice pong")
	})

	wakeUpTime := os.Getenv("WAKEUP_TIME")
	if wakeUpTime == "" {
		wakeUpTime = "6:00"
	}

	sleepTime := os.Getenv("SLEEP_TIME")
	if sleepTime == "" {
		sleepTime = "21:00"
	}

	w := strings.Split(wakeUpTime, ":")
	s := strings.Split(sleepTime, ":")

	wh, _ := strconv.Atoi(w[0])
	wm, _ := strconv.Atoi(w[1])
	wakeUpOffset := (60*wh + wm) % (60 * 24)

	sh, _ := strconv.Atoi(s[0])
	sm, _ := strconv.Atoi(s[1])
	awakeMinutes := (60*(sh+24) + sm - wakeUpOffset) % (60 * 24)

	ticker := time.NewTicker(10 * time.Minute)
	go func() {
		for {
			select {
			case <-ticker.C:
				now := time.Now()

				elapsedMinutes := (60*(now.Hour()+24) + now.Minute() - wakeUpOffset) % (60 * 24)
				fmt.Printf("Awake for %d minutes\n", elapsedMinutes)
				if elapsedMinutes < awakeMinutes {
					fmt.Println("keepalive ping!")
					http.Get(httpURL + "/keepalive")
				} else {
					fmt.Println("skipping keepalive, going to sleep...")
				}
			}
		}
	}()

	fmt.Printf("Run HTTP server on port:%v\n\r", httpPort)
	http.ListenAndServe(":"+httpPort, nil)
}
