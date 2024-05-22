// Scraper for SUZ news and Discord bot to report them
package main

import (
	"log"
	"os"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gocolly/colly/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"

	//#include <unistd.h>
	//#include <errno.h>
	"C"
)

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
			return
		}
		published = truncateToDay(published)

		results = append(results, Article{
			img, title, body, link, published,
		})
	})

	c.Visit("https://" + domain + "/cz/aktuality")

	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	return results
}

func dropPriv() {
	if syscall.Getuid() != 0 {
		return
	}

	log.Println("Running as root, downgrading to user nobody")
	user, err := user.Lookup("nobody")
	if err != nil {
		log.Fatalln("User not found or other error:", err)
	}
	uid, _ := strconv.ParseInt(user.Uid, 10, 32)
	gid, _ := strconv.ParseInt(user.Gid, 10, 32)
	cerr, errno := C.setgid(C.__gid_t(gid))
	if cerr != 0 {
		log.Fatalln("Unable to set GID due to error:", errno)
	}
	cerr, errno = C.setuid(C.__uid_t(uid))
	if cerr != 0 {
		log.Fatalln("Unable to set UID due to error:", errno)
	}
}

// Taken from the official GoLang caps library
func dropCaps() {
	// Read and display the capabilities of the running process
	// c := cap.GetProc()
	// log.Printf("this process has these caps:", c)

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

const ENV_AUTH_TOKEN = "SUZ_AUTH_TOKEN"
const ENV_CHANNEL_ID = "SUZ_CHANNEL_ID"
const ENV_SLEEP_MINS = "SUZ_SLEEP_MINS"

func main() {
	dropPriv()
	dropCaps()

	token := os.Getenv(ENV_AUTH_TOKEN)
	channelId := os.Getenv(ENV_CHANNEL_ID)
	sleepMins, err := strconv.ParseInt(os.Getenv(ENV_SLEEP_MINS), 10, 32)
	if len(token) == 0 || len(channelId) == 0 || err != nil {
		log.Fatalln("Failed to read/parse all the env vars.")
	}

	os.Setenv(ENV_AUTH_TOKEN, "")
	os.Setenv(ENV_CHANNEL_ID, "")
	os.Setenv(ENV_SLEEP_MINS, "")

	for {
		log.Println("Running...")
		process(token, channelId)
		log.Println("Sleeping for", sleepMins, "minutes")
		time.Sleep(time.Minute * time.Duration(sleepMins))
	}
}

func process(token string, channelId string) {
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

	messages, err := session.ChannelMessages(channelId, 20, "", "", "")
	if err != nil {
		log.Fatalln("Failed to read old messages.")
	}
	log.Println("Read", len(messages), "from the channel.")

	sendFrom := lastMessageTimestamp(messages)

	for _, article := range articles {
		if article.published.After(sendFrom) {
			log.Println("Sending article", article.title)
			sendArticle(session, channelId, article)
		}
	}
	session.Close()
}

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

func sendArticle(session *discordgo.Session, channelId string, article Article) {
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
	_, err := session.ChannelMessageSendComplex(channelId, &message)
	if err != nil {
		log.Println("Failed to send an article:", err)
	}
}
