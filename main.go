// Scraper for SUZ news and Discord bot to report them
package main

import (
	"log"
	"time"
	// "time"

	// "github.com/bwmarrin/discordgo"
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
		img := e.ChildAttr(".img img", "src")
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

func main() {

	articles := scrapeWeb("suz.cvut.cz")
	log.Println(articles)

	// _, err := discordgo.New("Bot " + "authentication token")
	// if err != nil {
	//     log.Fatalln("Failed to init the bot.")
	// }
}
