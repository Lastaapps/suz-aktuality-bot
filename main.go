// Scraper for SUZ news and Discord bot to report them
package main

import (
	"log"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gocolly/colly/v2"
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
	return results
}

const ENV_AUTH_TOKEN = "SUZ_AUTH_TOKEN"
const ENV_CHANNEL_ID = "SUZ_CHANNEL_ID"

func main() {

	articles := scrapeWeb("suz.cvut.cz")
	log.Println("Loaded", len(articles), "articles.")

	token := os.Getenv(ENV_AUTH_TOKEN)
	channelId := os.Getenv(ENV_CHANNEL_ID)
	if len(token) == 0 || len(channelId) == 0 {
		log.Fatalln("Failed to read all the env vars.")
	}

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalln("Failed to init the bot.")
	}

	err = session.Open()
	if err != nil {
		log.Fatalln("Failed to open socket.")
	}

	messages, err := session.ChannelMessages(channelId, 1, "", "", "")
	if err != nil {
		log.Fatalln("Failed to read old messages.")
	}
	log.Println(messages)

  sendArticle(session, channelId, articles[0])
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
		Color:       255,
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
