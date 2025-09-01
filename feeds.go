package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

func (a *Atomstr) dbGetAllFeeds() *[]feedStruct {
	sqlStatement := `SELECT pub, sec, url FROM feeds`
	rows, err := a.db.Query(sqlStatement)
	if err != nil {
		log.Fatal("[ERROR] Returning feeds from DB failed")
	}

	feedItems := []feedStruct{}

	for rows.Next() {
		feedItem := feedStruct{}
		if err := rows.Scan(&feedItem.Pub, &feedItem.Sec, &feedItem.Url); err != nil {
			log.Fatal("[ERROR] Scanning for feeds failed")
		}
		feedItem.Npub, _ = nip19.EncodePublicKey(feedItem.Pub)
		feedItems = append(feedItems, feedItem)
	}

	return &feedItems
}

// func processFeedUrl(ch chan string, wg *sync.WaitGroup, feedItem *feedStruct) {
func (a *Atomstr) processFeedUrl(ch chan feedStruct, wg *sync.WaitGroup) {
	for feedItem := range ch {
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // fetch feeds with 10s timeout
			defer cancel()
			fp := gofeed.NewParser()
			feed, err := fp.ParseURLWithContext(feedItem.Url, ctx)
			if err != nil {
				log.Println("[ERROR] Can't update feed", feedItem.Url)
			} else {
				log.Println("[DEBUG] Updating feed ", feedItem.Url)
				//fmt.Println(feed)
				feedItem.Title = feed.Title
				feedItem.Description = feed.Description
				feedItem.Link = feed.Link
				if feed.Image != nil {
					feedItem.Image = feed.Image.URL
				} else {
					feedItem.Image = defaultFeedImage
				}
				//feedItem.Image = feed.Image

				for i := range feed.Items {
					a.processFeedPost(feedItem, feed.Items[i])
				}
				log.Println("[DEBUG] Finished updating feed ", feedItem.Url)
			}
		}()
	}
	wg.Done()
}

func (a *Atomstr) processFeedPost(feedItem feedStruct, feedPost *gofeed.Item) {
	p := bluemonday.StrictPolicy() // initialize html sanitizer
	p.AllowImages()
	p.AllowStandardURLs()
	p.AllowAttrs("href").OnElements("a")

	//fmt.Println(feedPost.PublishedParsed)

	// Check if we should publish this post (age, duplicates, etc.)
	shouldPublish, reason := a.shouldPublishPost(feedItem, feedPost)
	if !shouldPublish {
		log.Println("[DEBUG] Skipping post from", feedItem.Url+":", reason)
		return
	}

	var feedText string
	var re = regexp.MustCompile(`nitter|telegram`)
	if re.MatchString(feedPost.Link) { // fix duplicated title in nitter/telegram
		feedText = p.Sanitize(feedPost.Description)
	} else {
		feedText = feedPost.Title + "\n\n" + p.Sanitize(feedPost.Description)
	}
	//fmt.Println(feedText)

	var regImg = regexp.MustCompile(`\<img.src=\"(http.*\.(jpg|png|gif)).*\/\>`) // allow inline images
	feedText = regImg.ReplaceAllString(feedText, "$1\n")

	var regLink = regexp.MustCompile(`\<a.href=\"(https.*?)\"\ .*\<\/a\>`) // allow inline links
	feedText = regLink.ReplaceAllString(feedText, "$1\n")

	feedText = html.UnescapeString(feedText) // decode html strings

	if feedPost.Enclosures != nil { // allow enclosure images/links
		for _, enclosure := range feedPost.Enclosures {
			feedText = feedText + "\n\n" + enclosure.URL
		}
	}

	if feedPost.Link != "" {
		feedText = feedText + "\n\n" + feedPost.Link
	}

	var tags nostr.Tags

	if feedPost.Categories != nil { // use post categories as tags
		for _, category := range feedPost.Categories {
			tags = append(tags, nostr.Tag{"t", category})
		}
	}

	tags = append(tags, nostr.Tag{"proxy", feedItem.Url + `#` + url.QueryEscape(feedPost.Link), "rss"})

	ev := nostr.Event{
		PubKey:    feedItem.Pub,
		CreatedAt: nostr.Timestamp(feedPost.PublishedParsed.Unix()),
		Kind:      nostr.KindTextNote,
		Tags:      tags,
		Content:   feedText,
	}

	ev.Sign(feedItem.Sec)

	if !noPub {
		nostrPostItem(ev)
		// Record that we published this post
		a.dbRecordPublishedPost(feedPost.Link, feedItem.Url, ev.ID)
	} else {
		log.Println("[DEBUG] not publishing post", ev)
		// Still record it even if not actually publishing (for testing)
		a.dbRecordPublishedPost(feedPost.Link, feedItem.Url, ev.ID)
	}

}

func (a *Atomstr) dbWriteFeed(feedItem *feedStruct) bool {
	_, err := a.db.Exec(`insert into feeds (pub, sec, url) values(?, ?, ?)`, feedItem.Pub, feedItem.Sec, feedItem.Url)
	if err != nil {
		log.Println("[ERROR] Can't add feed!")
		log.Fatal(err)
	}
	nip19Pub, _ := nip19.EncodePublicKey(feedItem.Pub)
	log.Println("[INFO] Added feed " + feedItem.Url + " with public key " + nip19Pub)
	return true
}

func (a *Atomstr) dbGetFeed(feedUrl string) *feedStruct {
	sqlStatement := `SELECT pub, sec, url FROM feeds WHERE url=$1;`
	row := a.db.QueryRow(sqlStatement, feedUrl)

	feedItem := feedStruct{}
	err := row.Scan(&feedItem.Pub, &feedItem.Sec, &feedItem.Url)

	if err != nil {
		log.Println("[INFO] Feed not found in DB")
	}
	return &feedItem
}

func (a *Atomstr) dbCheckPublishedPost(postUrl string) bool {
	sqlStatement := `SELECT COUNT(*) FROM published_posts WHERE url=?;`
	row := a.db.QueryRow(sqlStatement, postUrl)

	var count int
	err := row.Scan(&count)
	if err != nil {
		log.Println("[ERROR] Failed to check published post:", err)
		return false
	}
	return count > 0
}

func (a *Atomstr) dbRecordPublishedPost(postUrl, feedUrl, nostrEventId string) bool {
	sqlStatement := `INSERT INTO published_posts (url, feed_url, published_at, nostr_event_id) VALUES (?, ?, ?, ?);`
	_, err := a.db.Exec(sqlStatement, postUrl, feedUrl, time.Now().Unix(), nostrEventId)
	if err != nil {
		log.Println("[ERROR] Failed to record published post:", err)
		return false
	}
	log.Println("[DEBUG] Recorded published post:", postUrl, "with event ID:", nostrEventId)
	return true
}

func (a *Atomstr) dbGetPublishedPostByEventId(nostrEventId string) (string, bool) {
	sqlStatement := `SELECT url FROM published_posts WHERE nostr_event_id=?;`
	row := a.db.QueryRow(sqlStatement, nostrEventId)

	var url string
	err := row.Scan(&url)
	if err != nil {
		return "", false
	}
	return url, true
}

func (a *Atomstr) dbPrunePublishedPosts(olderThan time.Duration) (int64, error) {
	cutoffTime := time.Now().Add(-olderThan).Unix()
	sqlStatement := `DELETE FROM published_posts WHERE published_at < ?;`
	result, err := a.db.Exec(sqlStatement, cutoffTime)
	if err != nil {
		log.Println("[ERROR] Failed to prune published posts:", err)
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Println("[ERROR] Failed to get rows affected:", err)
		return 0, err
	}

	log.Printf("[INFO] Pruned %d published posts older than %v", rowsAffected, olderThan)
	return rowsAffected, nil
}

func (a *Atomstr) shouldPublishPost(feedItem feedStruct, feedPost *gofeed.Item) (bool, string) {
	// Check if post has a valid timestamp
	if feedPost.PublishedParsed == nil {
		return false, "Can't read PublishedParsed date"
	}

	// Check if post is too old
	if !checkMaxAge(feedPost.PublishedParsed, maxPostAge) {
		timeSince := time.Since(*feedPost.PublishedParsed)
		return false, fmt.Sprintf("Post is too old: %v (max age: %v)", timeSince, maxPostAge)
	}

	// Check if already published
	if a.dbCheckPublishedPost(feedPost.Link) {
		return false, "Post already published"
	}

	return true, "Ready to publish"
}

func checkValidFeedSource(feedUrl string) (*feedStruct, error) {
	log.Println("[DEBUG] Trying to find feed at", feedUrl)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fp := gofeed.NewParser()
	feed, err := fp.ParseURLWithContext(feedUrl, ctx)
	feedItem := feedStruct{}

	if err != nil {
		log.Println("[ERROR] Not a valid feed source")
		return &feedItem, err
	}
	// FIXME! That needs proper error handling.
	feedItem.Url = feedUrl
	feedItem.Title = feed.Title
	feedItem.Description = feed.Description
	feedItem.Link = feed.Link
	if feed.Image != nil {
		feedItem.Image = feed.Image.URL
	} else {
		feedItem.Image = defaultFeedImage
	}
	feedItem.Posts = feed.Items

	return &feedItem, err
}

func (a *Atomstr) addSource(feedUrl string) (*feedStruct, error) {
	//var feedElem2 *feedStruct
	feedItem, err := checkValidFeedSource(feedUrl)
	//if feedItem.Title == "" {
	if err != nil {
		log.Println("[ERROR] No valid feed found on", feedUrl)
		return feedItem, err
	}

	// check for existing feed
	feedTest := a.dbGetFeed(feedUrl)
	if feedTest.Url != "" {
		log.Println("[WARN] Feed already exists")
		return feedItem, err
	}

	feedItemKeys := generateKeysForUrl(feedUrl)
	feedItem.Pub = feedItemKeys.Pub
	feedItem.Sec = feedItemKeys.Sec
	//fmt.Println(feedItem)

	a.dbWriteFeed(feedItem)
	if !noPub {
		nostrUpdateFeedMetadata(feedItem)
	}

	log.Println("[INFO] Parsing post history of new feed")
	for i := range feedItem.Posts {
		a.processFeedPost(*feedItem, feedItem.Posts[i])
	}
	log.Println("[INFO] Finished parsing post history of new feed")

	return feedItem, err
}
func (a *Atomstr) deleteSource(feedUrl string) bool {
	// check for existing feed
	feedTest := a.dbGetFeed(feedUrl)
	if feedTest.Url != "" {
		sqlStatement := `DELETE FROM feeds WHERE url=$1;`
		_, err := a.db.Exec(sqlStatement, feedUrl)
		if err != nil {
			log.Println("[WARN] Can't remove feed")
			log.Fatal(err)
		}
		log.Println("[INFO] feed removed")
		return true
	} else {
		log.Println("[WARN] feed not found")
		return false
	}
}

func (a *Atomstr) listFeeds() {
	feeds := a.dbGetAllFeeds()

	for _, feedItem := range *feeds {
		nip19Pub, _ := nip19.EncodePublicKey(feedItem.Pub)
		fmt.Print(nip19Pub + " ")
		fmt.Println(feedItem.Url)
	}

}
