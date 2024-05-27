// Scraper for SUZ news and Discord bot to report them
package main

import (
	"log"
	"os"
	"strconv"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gocolly/colly/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"
)

// Article holds the scraped data from the website
type Article struct {
	img       string
	title     string
	body      string
	link      string
	published time.Time
}

func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// Downloads the recent articles from the SUZ web
func scrapeWeb(domain string) (results []Article) {
	c := colly.NewCollector()

	results = []Article{}

	// Find and visit all links
	c.OnHTML(".news-list-block div .cell a", func(e *colly.HTMLElement) {
		link := "https://" + domain + e.Attr("href")
		img := "https://" + domain + e.ChildAttr(".img img", "src")
		title := e.ChildText(".content h3")
		body := e.ChildText(".content .body p")

		layout := "2. 1. 2006"
		published, err := time.Parse(layout, e.ChildText(".content .date"))

		if err != nil {
			log.Println("Failed to parse date from the articles page!", err)
			return
		}
		published = truncateToDay(published)

		results = append(results, Article{
			img, title, body, link, published,
		})
	})

	c.Visit("https://" + domain + "/cz/aktuality")

	// reverse the article order
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	return results
}

// Taken from the official GoLang caps library
func dropCaps() {
	// Read and display the capabilities of the running process
	// c := cap.GetProc()
	// log.Printf("this process has these caps:", c)

	if os.Geteuid() == 0 {
		nobody := 65534
		groups := []int{65534}
		err := cap.SetGroups(groups[0])
		if err != nil {
			log.Fatalln("Failed to drop root")
		}
		err = cap.SetUID(nobody)
		if err != nil {
			log.Fatalln("Failed to drop root")
		}
	}

	empty := cap.NewSet()
	if err := empty.SetProc(); err != nil {
		// Could be fatal
		log.Printf("Failed to drop privilege: %q: %v", empty, err)
	}
	now := cap.GetProc()
	if cf, _ := now.Cf(empty); cf != 0 {
		// Could be fatal
		log.Printf("Failed to fully drop privilege: have=%q, wanted=%q", now, empty)
	}
}

// env var keys
const envAuthToken = "SUZ_AUTH_TOKEN"
const envChannelID = "SUZ_CHANNEL_ID"
const envSleepMins = "SUZ_SLEEP_MINS"

func main() {
	dropCaps()

	// load env vars
	token := os.Getenv(envAuthToken)
	channelID := os.Getenv(envChannelID)
	sleepMins, err := strconv.ParseInt(os.Getenv(envSleepMins), 10, 32)
	if len(token) == 0 || len(channelID) == 0 || err != nil {
		log.Fatalln("Failed to read/parse all the env vars.")
	}

	os.Setenv(envAuthToken, "")
	os.Setenv(envChannelID, "")
	os.Setenv(envSleepMins, "")

	// main loop
	for {
		log.Println("Running...")
		process(token, channelID)

		log.Println("Sleeping for", sleepMins, "minutes")
		time.Sleep(time.Minute * time.Duration(sleepMins))
	}
}

// Fetches articles, opens web-socket to discord, fetches old messages and sends the new ones if possible
func process(token string, channelID string) {
	articles := scrapeWeb("suz.cvut.cz")
	log.Println("Loaded", len(articles), "articles.")

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalln("Failed to init the bot.")
	}

	err = session.Open()
	if err != nil {
		log.Fatalln("Failed to open socket.")
	}

	messages, err := session.ChannelMessages(channelID, 20, "", "", "")
	if err != nil {
		log.Fatalln("Failed to read old messages.")
	}
	log.Println("Read", len(messages), "messages from the channel.")

	sendFrom := lastMessageTimestamp(messages)

	for _, article := range articles {
		if article.published.After(sendFrom) {
			log.Println("Sending article", article.title)
			sendArticle(session, channelID, article)
		}
	}
	session.Close()
}

// Finds the most recent article sent by me
func lastMessageTimestamp(messages []*discordgo.Message) (result time.Time) {
	result = time.Unix(0, 0)

	for _, message := range messages {
		if !message.Author.Bot || len(message.Embeds) == 0 {
			continue
		}
		timestamp, err := time.Parse(time.RFC3339, message.Embeds[0].Timestamp)
		if err != nil {
			log.Fatalln("Failed to parse Discord timestamp!", err)
		}
		if result.Before(timestamp) {
			result = timestamp
		}
	}

	return result
}

// Creates a post in the channel
func sendArticle(session *discordgo.Session, channelID string, article Article) {
	image := discordgo.MessageEmbedThumbnail{
		URL:      article.img,
		ProxyURL: "",
		Width:    520,
		Height:   252,
	}
	embed := discordgo.MessageEmbed{
		URL:         article.link,
		Type:        discordgo.EmbedTypeArticle,
		Title:       article.title,
		Description: article.body,
		Timestamp:   article.published.Format(time.RFC3339),
		Color:       0xedea2b,
		Footer:      nil,
		Image:       nil,
		Thumbnail:   &image,
		Video:       nil,
		Provider:    nil,
		Author:      nil,
		Fields:      []*discordgo.MessageEmbedField{},
	}

	flags := discordgo.MessageFlags(0)
	message := discordgo.MessageSend{
		Content:         "",
		Embeds:          []*discordgo.MessageEmbed{&embed},
		TTS:             false,
		Components:      []discordgo.MessageComponent{},
		Files:           []*discordgo.File{},
		AllowedMentions: nil,
		Reference:       nil,
		StickerIDs:      []string{},
		Flags:           flags,
	}
	_, err := session.ChannelMessageSendComplex(channelID, &message)
	if err != nil {
		log.Println("Failed to send an article:", err)
	}
}
