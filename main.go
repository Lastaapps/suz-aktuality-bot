// Scraper for SUZ news and Discord bot to report them
package main

import (
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/gocolly/colly/v2"
	"kernel.org/pub/linux/libs/security/libcap/cap"
)

// Article holds the scraped data from the website
type Article struct {
	img   string
	title string
	body  string
	url   string
	label string
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
		img := "https://" + domain + e.ChildAttr(".img-wrapper img", "src")
		title := e.ChildText("h2")
		body := e.ChildText(".body-wrapper p")

		label := e.ChildText(".labels-container")

		results = append(results, Article{
			img, title, body, link, label,
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
const SUZ_DOMAIN = "suz.cvut.cz"

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
	articles := scrapeWeb(SUZ_DOMAIN)
	log.Println("Loaded", len(articles), "articles.")

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalln("Failed to init the bot.")
	}

	err = session.Open()
	if err != nil {
		log.Fatalln("Failed to open socket.")
	}

	messages, err := session.ChannelMessages(channelID, 32, "", "", "")
	if err != nil {
		log.Fatalln("Failed to read old messages.")
	}
	log.Println("Read", len(messages), "messages from the channel.")

	lastUrls := lastPublishedUrls(messages)

	anySent := false
	for _, article := range articles {
		if !slices.Contains(lastUrls, article.url) {
			log.Println("Sending article", article.title)
			url, err := archiveArticle(article.url)
			if err != nil {
				log.Println("Failed to create an archive link for", article.title)
				continue
			}
			sendArticle(session, channelID, article, url)
			anySent = true
		}
	}

	session.Close()

	if anySent {
		log.Println("Archiving the root page")
		_, err = archiveWebPage("https://www.suz.cvut.cz/cz/aktuality")
		if err != nil {
			log.Println("Failed to create an archive link for the root Aktuality page")
		}
		log.Println("Done")
	}
}

func lastPublishedUrls(messages []*discordgo.Message) []string {
	result := []string{}

	for _, message := range messages {
		if !message.Author.Bot || len(message.Embeds) == 0 {
			log.Println("Found a suspicious message from", message.Author.Username)
			continue
		}
		theEmbed := message.Embeds[0]
		url := theEmbed.URL
		if strings.HasPrefix(url, "https://web.archive.org") {
			url = strings.TrimPrefix(url, "https")
			urlStartIndex := strings.Index(url, "http")
			url = url[urlStartIndex:]
		}
		result = append(result, url)
	}

	return result
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

func archiveArticle(articleUrl string) (string, error) {
	c := colly.NewCollector()
	domain := SUZ_DOMAIN

	// Find and visit all links
	c.OnHTML(".block-suzcvut-content .body a", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		// for the local content
		if !strings.HasPrefix(link, "http") {
			link = "https://" + domain + link
		}
		_, err := archiveWebPage(link)
		if err != nil {
			log.Println("Failed to backup the file")
		}
	})

	c.Visit(articleUrl)

	url, err := archiveWebPage(articleUrl)
	if err != nil {
		log.Println("Failed to backup the article")
	}

	return url, nil
}

// Creates a post in the channel
func sendArticle(session *discordgo.Session, channelID string, article Article, archiveUrl string) {
	bodyWithArchive := article.body + "\n\n[**Archiv**](" + archiveUrl + ")"

	image := discordgo.MessageEmbedThumbnail{
		URL:      article.img,
		ProxyURL: "",
		Width:    520,
		Height:   252,
	}
	embed := discordgo.MessageEmbed{
		URL: article.url, // article.link,
		// URL:         url,
		Type:  discordgo.EmbedTypeArticle,
		Title: article.title,
		// Description: article.body,
		Description: bodyWithArchive,
		// Timestamp:   article.published.Format(time.RFC3339),
		Color:     getColorForString(article.label),
		Footer:    nil,
		Image:     nil,
		Thumbnail: &image,
		Video:     nil,
		Provider:  nil,
		Author:    nil,
		Fields:    []*discordgo.MessageEmbedField{
			// {Name: "Kategorie", Value: article.label},
			// {Name: "Archiv", Value: archiveUrl},
		},
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
	dcMessage, err := session.ChannelMessageSendComplex(channelID, &message)
	if err != nil {
		log.Println("Failed to send an article:", err)
		return
	}
	// CrossPosts the message of related channels
	_, err = session.ChannelMessageCrosspost(dcMessage.ChannelID, dcMessage.ID)
	if err != nil {
		log.Println("Failed to publish an article:", err)
		return
	}
}

func getColorForString(str string) int {
	// https://www.figma.com/colors/rose/
	// yes, this is slow and stupid, but I don't care
	if str == "Stravování" {
		return 0xff1d8d // rose
	}
	if str == "Ubytování" {
		return 0x90d5ff // light blue
	}
	if str == "Obecné" {
		return 0xf2b949 // mimosa
	}
	if str == "Akce" {
		return 0x89f336 // lime green
	}
	return 0xedea2b
}

func archiveWebPage(url string) (string, error) {
	log.Println("Archiving URL:", url)

	if !strings.HasPrefix(url, "http") {
		log.Fatalln("Urls don't have the http(s) prefix, it's gonna be \"hard\" to work with them later.")
	}
	// log.Println("curl", "-I", "\"https://web.archive.org/save/"+url+"\"")
	_, err := http.Get("https://web.archive.org/save/" + url)
	if err != nil {
		return "", nil
	}
	archiveURL := "https://web.archive.org/web/" + url
	log.Println("Archive successful:", archiveURL)
	return archiveURL, nil
}
