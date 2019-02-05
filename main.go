package main

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis"

	log "github.com/Sirupsen/logrus"
	"github.com/batt/tinabott/brain"
	"github.com/batt/tinabott/slackbot"
	"github.com/nlopes/slack"
)

type Order struct {
	Timestamp time.Time
	Dishes    map[string][]string //map dishes with users
	Users     map[string][]string //map each user to his/her dishes
}

func NewOrder() *Order {
	return &Order{
		Timestamp: time.Now(),
		Dishes:    make(map[string][]string),
		Users:     make(map[string][]string),
	}
}

func getOrder(brain *brain.Brain) *Order {
	var order Order
	err := brain.Get("order", &order)
	if err != nil {
		return NewOrder()
	}

	if time.Since(order.Timestamp).Hours() > 13 {
		log.Infoln("Deleting old order")
		return NewOrder()
	}
	return &order
}

func fuzzyMatch(dish, menuline string) bool {
	dish = strings.ToLower(dish)

	key := regexp.MustCompile(strings.Replace(dish, " ", ".*", -1))

	return key.MatchString(strings.ToLower(menuline))
}

func findDishes(menu, dish string) []string {
	dish = strings.TrimSpace(strings.ToLower(dish))
	menus := strings.Split(strings.TrimSpace(menu), "\n")
	var matches []string
	for _, m := range menus {
		if strings.ToLower(m) == dish {
			return []string{m}
		}

		if fuzzyMatch(dish, m) {
			matches = append(matches, m)
		}
	}
	return matches
}

func clearUserOrder(order *Order, user string) string {
	dishes := order.Users[user]
	delete(order.Users, user)
	for _, d := range dishes {

		// Find and remove the user
		for i, v := range order.Dishes[d] {
			if v == user {
				order.Dishes[d] = append(order.Dishes[d][:i], order.Dishes[d][i+1:]...)
				break
			}
		}
		if len(order.Dishes[d]) == 0 {
			delete(order.Dishes, d)
		}
	}
	return strings.Join(dishes, ",")
}

func main() {
	token := os.Getenv("SLACK_BOT_TOKEN")

	if token == "" {
		log.Fatalln("No slack token found!")
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		log.Fatalln("No redis URL found!")
	}
	log.Printf("Redis URL: %s\n", redisURL)

	// Slack Bot filter
	var opts slackbot.Config
	bot := slackbot.New(token, opts)
	brain := brain.New(redisURL)

	bot.RespondTo("^per me (.*)$", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		dish := args[1]
		order := getOrder(brain)

		if strings.ToLower(dish) == "niente" {
			old := clearUserOrder(order, user.Name)
			bot.Message(msg.Channel, "Ok, cancello ordine "+old)
			brain.Set("order", order)
			return
		}
		var menu string
		err := brain.Get("menu", &menu)
		if err != nil {
			bot.Message(msg.Channel, "Nessun menu impostato!")
			return
		}

		dishes := findDishes(menu, dish)
		if len(dishes) == 0 {
			bot.Message(msg.Channel, "Non ho trovato nulla nel menu che corrisponda a '"+dish+"'")
		} else if len(dishes) > 1 {
			matches := strings.Join(dishes, "\n")
			bot.Message(msg.Channel, "Ho trovato i seguenti piatti:\n"+matches+"\n----\nSii più preciso cribbio!")
		} else {
			d := dishes[0]
			u := user.Name
			clearUserOrder(order, user.Name)
			order.Dishes[d] = append(order.Dishes[d], u)
			order.Users[u] = append(order.Users[u], d)
			brain.Set("order", order)
			bot.Message(msg.Channel, "Ok, "+d+" per "+u)
		}
	})

	bot.RespondTo("^ordine$", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		order := getOrder(brain)

		r := ""
		for d := range order.Dishes {
			l := fmt.Sprintf("%d %s ", len(order.Dishes[d]), d)
			l += "[ " + strings.Join(order.Dishes[d], ",") + " ]\n"
			r = r + l
		}

		bot.Message(msg.Channel, "Ecco l'ordine:\n"+r)
	})

	bot.RespondTo("^email$", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		order := getOrder(brain)
		subj := "Ordine Develer del giorno " + order.Timestamp.Format("02/01/2006")
		body := ""
		for d := range order.Dishes {
			body += fmt.Sprintf("%d %s\n", len(order.Dishes[d]), d)
		}
		out := subj + "\n" + body + "\n\n" +
			"<mailto:info@tuttobene-bar.it,sara@tuttobene-bar.it" +
			"?subject=" + url.PathEscape(subj) +
			"&body=" + url.PathEscape(body) +
			"|Link `mailto` clickabile>"
		bot.Message(msg.Channel, out)
	})

	bot.RespondTo("^menu([\\s\\S]*)?", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		var menu string
		if len(args) > 1 {
			menu = strings.TrimSpace(args[1])
		} else {
			menu = ""
		}

		if menu == "" {
			err := brain.Get("menu", &menu)
			if err == redis.Nil {
				bot.Message(msg.Channel, "Non c'è nessun menu impostato!")
			} else {
				bot.Message(msg.Channel, "Il menu è:\n"+menu)
			}
		} else {
			brain.Set("menu", menu)
			bot.Message(msg.Channel, "Ok, il menu è:\n"+menu)
		}
	})

	bot.RespondTo("^set (.*)$", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		ar := strings.Split(args[1], " ")
		key := ar[0]
		val := ar[1]
		err := brain.Set(key, val)
		if err != nil {
			bot.Message(msg.Channel, "Error: "+err.Error())
		} else {
			bot.Message(msg.Channel, "Ok")
		}
	})

	bot.RespondTo("^get (.*)$", func(b *slackbot.Bot, msg *slack.Msg, user *slack.User, args ...string) {
		key := args[1]
		var val string
		err := brain.Get(key, &val)
		if err != nil {
			bot.Message(msg.Channel, "Error: "+err.Error())
		} else {
			bot.Message(msg.Channel, key+": "+val)
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
