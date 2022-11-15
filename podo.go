package main

import (
	"flag"
	"fmt"
	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/bogem/id3v2"
	"github.com/mmcdole/gofeed"
	"github.com/schollz/progressbar/v3"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type PodcastItem struct {
	origin      string
	title       string
	description string
	fileName    string
	date        *time.Time
	link        string
}

var (
	srcFileName = "podcast_sources.txt"
	memFileName = "podcasts_memory.txt"
	dumpPath    = ""
	dryRun      = false
	memory      = map[string]struct{}{}
)

func main() {
	setup()

	links := loadLinks(srcFileName)
	loadMemoryFile()

	podcastItems := getItems(links)

	downloaded := 0
	for index, item := range podcastItems {
		downloaded += downloadItem(item, index, len(podcastItems))
	}

	if downloaded == 0 {
		fmt.Println("No files downloaded.")
	} else {
		fmt.Println("Successfully downloaded " + strconv.Itoa(downloaded) + " files.")
	}
}

func setup() {
	flag.StringVar(&srcFileName, "src", "podcast_sources.txt", "Specify the file with podcast sources.")
	flag.StringVar(&memFileName, "mem", "podcasts_memory.txt", "Specify the memory file with already downloaded podcast links.")
	flag.StringVar(&dumpPath, "dump", os.Args[0]+"/", "Location (relative path) where to store downloaded files.")
	flag.BoolVar(&dryRun, "dry", false, "Dry run will add links to memory file without actually downloading any files.")
	flag.Parse()
}

func loadMemoryFile() {
	memRaw, err := os.ReadFile(memFileName)
	if err != nil {
		fmt.Println("File with previously downloaded links not found. Creating a new one.")
		os.Create(memFileName)
	}
	for _, line := range strings.Split(string(memRaw), "\n") {
		memory[strings.TrimSpace(line)] = struct{}{}
	}
}

func loadLinks(srcFName string) []string {
	srcs, err := os.ReadFile(srcFName)
	if err != nil {
		fmt.Println("Unable to load file with sources, unexpected error: " + err.Error())
		os.Exit(1)
	}
	links := strings.Split(strings.TrimSpace(string(srcs)), "\n")
	return links
}

func getItems(links []string) []PodcastItem {
	parser := gofeed.NewParser()
	items := make([]PodcastItem, 0)

	for _, link := range links {
		raw, err := parser.ParseURL(strings.TrimSpace(link))
		if err != nil {
			fmt.Println("Unable to parse url:" + link + ". Error:" + err.Error())
			continue
		}
		for _, item := range raw.Items {
			if item.Enclosures != nil {
				if _, ok := memory[item.Enclosures[0].URL]; ok {
					continue
				}
				p := PodcastItem{
					origin:      raw.Title,
					title:       item.Title,
					description: item.Description,
					fileName:    purify(item.Title),
					date:        item.PublishedParsed,
					link:        item.Enclosures[0].URL,
				}
				items = append(items, p)
			}
		}
	}
	return items
}

func purify(s string) string {
	s = strings.ReplaceAll(s, ">", "")
	s = strings.ReplaceAll(s, "<", "")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "\"", "")
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "\\", "")
	s = strings.ReplaceAll(s, "|", "")
	s = strings.ReplaceAll(s, "?", "")
	s = strings.ReplaceAll(s, "*", "")
	return s
}

func updateMemoryFile() {
	memS := ""
	for s := range memory {
		memS += strings.TrimSpace(s) + "\n"
	}
	err := os.WriteFile(memFileName, []byte(memS), 'w')
	if err != nil {
		fmt.Println("Unable to update memory file. ERROR: " + err.Error())
	}
}

// downloads given item and returns number of successfully downloaded files (1 or 0)
func downloadItem(item PodcastItem, index int, total int) int {
	if _, ok := memory[item.link]; ok {
		// link is already in memory => skip, don't download anything
		return 0
	}
	if dryRun {
		// dry-run => skip, don't download anything, link mark as downloaded
		memory[item.link] = struct{}{} // add new item to memory
		updateMemoryFile()             // update the memory file
		return 0
	}

	// get the data from the URL
	resp, err := http.Get(item.link)
	if err != nil {
		fmt.Println("Failed to download file from URL:" + item.link + "ERROR:" + err.Error())
		return 0
	}
	defer resp.Body.Close()

	// create a new file
	fileName := item.date.Format("2006-01-02") + " - " + item.origin + " - " + item.fileName + ".mp3"
	partFileName := filepath.Join(dumpPath, fileName+".part")
	finalFileName := filepath.Join(dumpPath, fileName)

	file, err := os.Create(partFileName)
	if err != nil {
		fmt.Println("Failed to create a new file: " + err.Error())
	}

	// create a progressbar
	pbar := progressbar.NewOptions(int(resp.ContentLength),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetDescription("downloading "+strconv.Itoa(index+1)+"/"+strconv.Itoa(total)),
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetWidth(20))

	// copy the data into the new file
	_, err = io.Copy(io.MultiWriter(file, pbar), resp.Body)
	if err != nil {
		// copying failed => report, close file, clear progress bar, return 0 (without updating memory or removing .part)
		fmt.Println("Failed to copy data to file:", err)
		pbar.Clear()
		file.Close()
		return 0
	}

	// file was successfully completed => close the file, clear the progress bar, remove .part, update memory and return 1
	pbar.Clear()
	file.Close()

	// rename the downloaded file
	err = os.Rename(partFileName, finalFileName)
	if err != nil {
		fmt.Println("Failed to rename downloaded file:", err)
	}

	// set tags from rss item
	setMetadata(finalFileName, item)

	// update the memory
	memory[item.link] = struct{}{}
	updateMemoryFile()

	return 1
}

func setMetadata(fileName string, item PodcastItem) {
	// Open file and parse tag in it.
	tag, err := id3v2.Open(fileName, id3v2.Options{Parse: true})
	if err != nil {
		log.Fatal("Error while opening mp3 file: ", err)
	}
	defer tag.Close()

	// Set simple text frames.
	tag.SetDefaultEncoding(id3v2.EncodingUTF8)
	tag.SetAlbum(item.origin)
	tag.SetTitle(item.title)

	// Set comment frame.
	converter := md.NewConverter("", true, nil)
	markdown, err := converter.ConvertString(item.description)
	if err != nil {
		log.Fatal("Error while read podcast description: ", err)
	}
	comment := id3v2.CommentFrame{
		Encoding:    id3v2.EncodingUTF8,
		Language:    "cze",
		Description: markdown,
	}
	tag.AddCommentFrame(comment)

	// Write tag to file.
	if err = tag.Save(); err != nil {
		log.Fatal("Error while saving a tag: ", err)
	}
}
